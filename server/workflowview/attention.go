package workflowview

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/shared/clientui"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Attention struct {
	queries     *sqlitegen.Queries
	definitions *DefinitionProjection
	authority   *sessionruntime.Authority
	prompts     PendingPromptSource
}

func NewAttention(metadataStore *metadata.Store, definitions *DefinitionProjection, authority *sessionruntime.Authority, prompts PendingPromptSource) (*Attention, error) {
	if metadataStore == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	if definitions == nil {
		return nil, errors.New("definition projection is required")
	}
	if authority == nil {
		return nil, errors.New("session runtime authority is required")
	}
	if prompts == nil {
		return nil, errors.New("pending prompt source is required")
	}
	return &Attention{
		queries:     metadataStore.Queries(),
		definitions: definitions,
		authority:   authority,
		prompts:     prompts,
	}, nil
}

func (a *Attention) List(ctx context.Context, req *taskpb.AttentionListRequest) (*taskpb.AttentionListSuccess, error) {
	if a == nil {
		return nil, errors.New("attention read model is required")
	}
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	pageSize := int(req.PageSize)
	if pageSize == 0 {
		pageSize = 50
	}
	cursor, err := parseAttentionPageToken(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	live, err := a.liveQuestionCandidates(ctx, nil, nil)
	if err != nil {
		return nil, err
	}
	durable, err := a.durableCandidates(ctx, cursor, nil, pageSize+len(live)+1)
	if err != nil {
		return nil, err
	}
	items := mergeAttentionCandidates(cursor, durable, live)
	hasNext := len(items) > pageSize
	if hasNext {
		items = items[:pageSize]
	}
	var nextPageToken *string
	if hasNext && len(items) != 0 {
		last := items[len(items)-1]
		token := attentionPageTokenFor(last.OccurredAt.AsTime().UnixMilli(), last.Id)
		nextPageToken = &token
	}
	return &taskpb.AttentionListSuccess{
		Items:         items,
		NextPageToken: nextPageToken,
		GeneratedAt:   timestamppb.Now(),
	}, nil
}

func (a *Attention) ListTask(ctx context.Context, req *taskpb.TaskAttentionListRequest) (*taskpb.TaskAttentionListSuccess, error) {
	if a == nil {
		return nil, errors.New("attention read model is required")
	}
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	taskID := strings.TrimSpace(req.TaskId)
	task, err := a.queries.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	durable, err := a.durableCandidates(ctx, attentionPageCursor{}, &taskID, 0)
	if err != nil {
		return nil, err
	}
	live, err := a.liveQuestionCandidates(ctx, &taskID, &task)
	if err != nil {
		return nil, err
	}
	items := mergeAttentionCandidates(attentionPageCursor{}, durable, live)
	return &taskpb.TaskAttentionListSuccess{
		Items:       items,
		GeneratedAt: timestamppb.Now(),
	}, nil
}

type attentionPageCursor struct {
	occurredAtUnixMs int64
	itemID           string
	hasValue         bool
}

type attentionCandidate struct {
	item *taskpb.AttentionItem
}

func (a *Attention) durableCandidates(ctx context.Context, cursor attentionPageCursor, taskID *string, limit int) ([]attentionCandidate, error) {
	rows, err := a.durableCandidateRows(ctx, cursor, taskID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]attentionCandidate, 0, len(rows))
	for _, row := range rows {
		item, err := a.durableCandidate(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, attentionCandidate{item: item})
	}
	return out, nil
}

func (a *Attention) durableCandidateRows(ctx context.Context, cursor attentionPageCursor, taskID *string, limit int) ([]sqlitegen.ListWorkflowDurableAttentionCandidatesRow, error) {
	task := sql.NullString{}
	if taskID != nil {
		task = sql.NullString{String: *taskID, Valid: true}
	}
	return a.queries.ListWorkflowDurableAttentionCandidates(ctx, sqlitegen.ListWorkflowDurableAttentionCandidatesParams{
		SelectedTaskID:         task,
		PageLimit:              int64(limit),
		CursorActive:           boolInt64(cursor.hasValue),
		CursorOccurredAtUnixMs: cursor.occurredAtUnixMs,
		CursorItemID:           cursor.itemID,
	})
}

func (a *Attention) durableCandidate(ctx context.Context, row sqlitegen.ListWorkflowDurableAttentionCandidatesRow) (*taskpb.AttentionItem, error) {
	switch row.Kind {
	case "approval":
		if !row.ApprovalID.Valid {
			return nil, fmt.Errorf("approval attention candidate %q has no approval id", row.ID)
		}
		parsedApprovalID, err := workflow.ParseApprovalID(strings.TrimSpace(row.ApprovalID.String))
		if err != nil {
			return nil, err
		}
		approvalID := parsedApprovalID.String()
		approval, err := a.pendingApproval(ctx, workflow.TaskID(row.TaskID), approvalID)
		if err != nil {
			return nil, err
		}
		snapshot := approvalAttentionSnapshot(approval)
		return &taskpb.AttentionItem{
			Id:          row.ID,
			Kind:        taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_APPROVAL,
			ProjectId:   row.ProjectID,
			WorkflowId:  row.WorkflowID.String(),
			TaskId:      row.TaskID,
			TaskShortId: row.ShortID,
			TaskTitle:   row.Title,
			Detail: &taskpb.AttentionItem_Approval{Approval: &taskpb.ApprovalAttention{
				ApprovalId: approvalID, ApprovalSnapshot: snapshot,
			}},
			OccurredAt: timestamppb.New(time.UnixMilli(row.OccurredAtUnixMs)),
		}, nil
	case "interrupted":
		reference, err := currentNodeReferenceFromAttentionCandidate(row)
		if err != nil {
			return nil, err
		}
		currentNode, err := currentNodeFromAttentionCandidate(row, reference)
		if err != nil {
			return nil, err
		}
		var details *taskpb.InterruptedCurrentNodeDetails
		if row.InterruptionDetailJson.Valid {
			value := strings.TrimSpace(row.InterruptionDetailJson.String)
			if value == "" {
				return nil, fmt.Errorf("interrupted attention candidate %q has blank interruption detail", row.ID)
			}
			details, err = interruptionDetails(value)
			if err != nil {
				return nil, err
			}
		}
		return &taskpb.AttentionItem{
			Id:          row.ID,
			Kind:        taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_INTERRUPTED_CURRENT_NODE,
			ProjectId:   row.ProjectID,
			WorkflowId:  row.WorkflowID.String(),
			TaskId:      row.TaskID,
			TaskShortId: row.ShortID,
			TaskTitle:   row.Title,
			Detail: &taskpb.AttentionItem_InterruptedCurrentNode{InterruptedCurrentNode: &taskpb.InterruptedAttention{
				CurrentNode: currentNode, SessionId: currentNode.SessionId, Details: details,
			}},
			OccurredAt: timestamppb.New(time.UnixMilli(row.OccurredAtUnixMs)),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported durable workflow attention candidate kind %q", row.Kind)
	}
}

func (a *Attention) pendingApproval(ctx context.Context, taskID workflow.TaskID, approvalID string) (workflow.PendingApproval, error) {
	approvals, err := a.definitions.store.ListPendingApprovals(ctx, taskID)
	if err != nil {
		return workflow.PendingApproval{}, err
	}
	for _, approval := range approvals {
		if approval.ID.String() == approvalID {
			return approval, nil
		}
	}
	return workflow.PendingApproval{}, fmt.Errorf("approval attention candidate %q is no longer pending for task %q", approvalID, taskID)
}

func approvalAttentionSnapshot(approval workflow.PendingApproval) *taskpb.ApprovalSnapshot {
	targets := make([]*taskpb.ApprovalTarget, 0, len(approval.Branches))
	for _, branch := range approval.Branches {
		targets = append(targets, &taskpb.ApprovalTarget{DisplayName: branch.Target.DisplayName})
	}
	return &taskpb.ApprovalSnapshot{
		SourceNodeDisplayName: approval.Transition.SourceDisplayName,
		Targets:               targets,
		Commentary:            textutil.OptionalExactString(approval.Commentary),
		OutputValues:          attentionOutputValues(approval.OutputValues),
		WorkflowRevisionSeen:  approval.WorkflowVersion,
	}
}

func attentionOutputValues(values map[string]string) []*taskpb.OutputValue {
	out := make([]*taskpb.OutputValue, 0, len(values))
	for key, value := range values {
		out = append(out, &taskpb.OutputValue{Name: key, Value: value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func interruptionDetails(raw string) (*taskpb.InterruptedCurrentNodeDetails, error) {
	detail, err := workflow.DecodeCurrentNodeInterruptionDetail(raw)
	if err != nil {
		return nil, err
	}
	result := &taskpb.InterruptedCurrentNodeDetails{
		Code:   detail.Code,
		Detail: &taskpb.InterruptedCurrentNodeDetails_Generic{Generic: &taskpb.GenericInterruptionDetails{}},
	}
	for key, value := range detail.Fields {
		result.Fields = append(result.Fields, &taskpb.DetailField{Name: key, Value: value})
	}
	sort.Slice(result.Fields, func(i, j int) bool { return result.Fields[i].Name < result.Fields[j].Name })
	if unavailable := detail.ConfiguredExecutionTargetUnavailable; unavailable != nil {
		result.Detail = &taskpb.InterruptedCurrentNodeDetails_ConfiguredExecutionTargetUnavailable{
			ConfiguredExecutionTargetUnavailable: serverapi.WorkflowConfiguredTargetUnavailableDetails(
				serverapi.WorkflowExecutionTargetMode(unavailable.Mode), unavailable.RequestedRef,
				serverapi.WorkflowExecutionTargetUnavailableCause(unavailable.Cause)),
		}
	}
	if detail.OriginalExecutionTargetUnavailable != nil {
		result.Detail = &taskpb.InterruptedCurrentNodeDetails_OriginalExecutionTargetUnavailable{
			OriginalExecutionTargetUnavailable: detail.OriginalExecutionTargetUnavailable,
		}
	}
	return result, nil
}

func currentNodeFromAttentionCandidate(row sqlitegen.ListWorkflowDurableAttentionCandidatesRow, reference workflow.CurrentNodeReference) (*taskpb.AttentionCurrentNode, error) {
	currentNode := workflowCurrentNodeReference(reference)
	if row.SessionID.Valid {
		value := strings.TrimSpace(row.SessionID.String)
		if value == "" {
			return nil, fmt.Errorf("interrupted attention candidate %q has blank session id", row.ID)
		}
		currentNode.SessionId = &value
	}
	return currentNode, nil
}

func currentNodeReferenceFromAttentionCandidate(row sqlitegen.ListWorkflowDurableAttentionCandidatesRow) (workflow.CurrentNodeReference, error) {
	if !row.NodeID.Valid || strings.TrimSpace(row.NodeID.String) == "" {
		return workflow.CurrentNodeReference{}, fmt.Errorf("interrupted attention candidate %q has no current node", row.ID)
	}
	var branchKey *workflow.TransitionBranchKey
	if row.TransitionBranchKey.Valid {
		value := workflow.TransitionBranchKey(strings.TrimSpace(row.TransitionBranchKey.String))
		if value == "" {
			return workflow.CurrentNodeReference{}, fmt.Errorf("interrupted attention candidate %q has invalid branch key", row.ID)
		}
		branchKey = &value
	}
	reference, err := workflow.NewCurrentNodeReference(workflow.TaskID(row.TaskID), workflow.NodeID(row.NodeID.String), branchKey)
	if err != nil {
		return workflow.CurrentNodeReference{}, fmt.Errorf("interrupted attention candidate %q has invalid current node: %w", row.ID, err)
	}
	return reference, nil
}

func (a *Attention) liveQuestionCandidates(ctx context.Context, taskFilter *string, selectedTask *sqlitegen.TaskRecord) ([]attentionCandidate, error) {
	var snapshots map[workflow.TaskID]sessionruntime.TaskExecutionSnapshot
	var err error
	if selectedTask == nil {
		snapshots, err = a.authority.CurrentWorkflowTaskExecutionSnapshots()
	} else {
		snapshots, err = a.authority.CurrentScopedTaskExecutionSnapshots(
			selectedTask.ProjectID, runtimeids.WorkflowID(selectedTask.WorkflowID), []workflow.TaskID{workflow.TaskID(selectedTask.ID)},
		)
	}
	if err != nil {
		return nil, err
	}
	out := []attentionCandidate{}
	for taskID, snapshot := range snapshots {
		if taskFilter != nil && string(taskID) != *taskFilter {
			continue
		}
		var task *sqlitegen.TaskRecord
		for _, execution := range snapshot.Executions {
			if !execution.HasPendingPromptKind(sessionruntime.PendingPromptKindQuestion) &&
				!execution.HasPendingPromptKind(sessionruntime.PendingPromptKindSessionApproval) {
				continue
			}
			if execution.Agent == nil {
				return nil, fmt.Errorf("task %q has a question on a non-agent execution", taskID)
			}
			if task == nil {
				record, err := a.queries.GetTask(ctx, string(taskID))
				if err != nil {
					return nil, err
				}
				task = &record
			}
			prompts, err := a.prompts.ListPendingPrompts(execution.Agent.SessionID.String())
			if err != nil {
				return nil, err
			}
			promptsByID := make(map[clientui.ToolCallID]PendingPromptSnapshot, len(prompts))
			for _, prompt := range prompts {
				if err := prompt.ToolCallID.Validate(); err != nil {
					return nil, fmt.Errorf("task %q session %q pending prompt identity: %w", taskID, execution.Agent.SessionID, err)
				}
				if _, duplicate := promptsByID[prompt.ToolCallID]; duplicate {
					return nil, fmt.Errorf("task %q session %q has duplicate pending prompt %q", taskID, execution.Agent.SessionID, prompt.ToolCallID)
				}
				promptsByID[prompt.ToolCallID] = prompt
			}
			for _, promptReference := range execution.PendingPrompts {
				if promptReference.Kind != sessionruntime.PendingPromptKindQuestion &&
					promptReference.Kind != sessionruntime.PendingPromptKindSessionApproval {
					continue
				}
				prompt, present := promptsByID[promptReference.ToolCallID]
				if !present {
					continue
				}
				question, present, err := pendingQuestionFromPrompt(prompt)
				if err != nil {
					return nil, err
				}
				if !present {
					continue
				}
				if prompt.Approval != (promptReference.Kind == sessionruntime.PendingPromptKindSessionApproval) {
					return nil, fmt.Errorf("task %q session %q prompt for tool call %q changed prompt kind", taskID, execution.Agent.SessionID, promptReference.ToolCallID)
				}
				occurredAt := prompt.CreatedAt.UnixMilli()
				if occurredAt <= 0 {
					return nil, fmt.Errorf("task %q session %q prompt for tool call %q has no occurrence time", taskID, execution.Agent.SessionID, promptReference.ToolCallID)
				}
				currentNode := workflowCurrentNodeReference(execution.Ref.CurrentNode)
				out = append(out, attentionCandidate{item: &taskpb.AttentionItem{
					Id:          liveQuestionAttentionID(execution.Agent.SessionID, prompt.StepID, prompt.ToolCallID),
					Kind:        taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_QUESTION,
					ProjectId:   task.ProjectID,
					WorkflowId:  task.WorkflowID.String(),
					TaskId:      task.ID,
					TaskShortId: task.ShortID,
					TaskTitle:   task.Title,
					Detail: &taskpb.AttentionItem_Question{Question: &taskpb.QuestionAttention{
						Message: textutil.OptionalExactString(question.message), CurrentNode: currentNode, Question: question.prompt,
					}},
					OccurredAt: timestamppb.New(time.UnixMilli(occurredAt)),
				}})
			}
		}
	}
	if err := a.attachLiveQuestionSessionNames(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func liveQuestionAttentionID(
	sessionID runtimeids.SessionID,
	stepID runtimeids.StepID,
	toolCallID clientui.ToolCallID,
) string {
	return "question:" + sessionID.String() + ":" + stepID.String() + ":" + string(toolCallID)
}

func (a *Attention) attachLiveQuestionSessionNames(ctx context.Context, candidates []attentionCandidate) error {
	sessionIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.item.GetQuestion().GetQuestion() == nil {
			return fmt.Errorf("live question attention %q has no prompt", candidate.item.Id)
		}
		sessionIDs = append(sessionIDs, candidate.item.GetQuestion().Question.SessionId)
	}
	names, err := resolveSessionNames(ctx, a.queries.ListSessionNamesByIDs, sessionIDs)
	if err != nil {
		return err
	}
	for index := range candidates {
		question := candidates[index].item.GetQuestion()
		question.SessionName = names[question.Question.SessionId]
	}
	return nil
}

func mergeAttentionCandidates(cursor attentionPageCursor, groups ...[]attentionCandidate) []*taskpb.AttentionItem {
	items := []*taskpb.AttentionItem{}
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, candidate := range group {
			item := candidate.item
			if cursor.hasValue && !attentionItemBefore(item, cursor) {
				continue
			}
			if _, exists := seen[item.Id]; exists {
				continue
			}
			seen[item.Id] = struct{}{}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].OccurredAt.AsTime().Equal(items[j].OccurredAt.AsTime()) {
			return items[i].OccurredAt.AsTime().After(items[j].OccurredAt.AsTime())
		}
		return items[i].Id > items[j].Id
	})
	return items
}

func attentionItemBefore(item *taskpb.AttentionItem, cursor attentionPageCursor) bool {
	occurredAt := item.OccurredAt.AsTime().UnixMilli()
	return occurredAt < cursor.occurredAtUnixMs ||
		(occurredAt == cursor.occurredAtUnixMs && item.Id < cursor.itemID)
}

func parseAttentionPageToken(token string) (attentionPageCursor, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return attentionPageCursor{}, nil
	}
	timestampPart, encodedID, ok := strings.Cut(trimmed, "|")
	if !ok {
		return attentionPageCursor{}, ErrInvalidPageToken
	}
	occurredAt, err := strconv.ParseInt(timestampPart, 10, 64)
	if err != nil || occurredAt < 0 {
		return attentionPageCursor{}, ErrInvalidPageToken
	}
	decodedID, err := base64.RawURLEncoding.DecodeString(encodedID)
	if err != nil || strings.TrimSpace(string(decodedID)) == "" {
		return attentionPageCursor{}, ErrInvalidPageToken
	}
	return attentionPageCursor{occurredAtUnixMs: occurredAt, itemID: string(decodedID), hasValue: true}, nil
}

func attentionPageTokenFor(occurredAtUnixMs int64, id string) string {
	return strconv.FormatInt(occurredAtUnixMs, 10) + "|" + base64.RawURLEncoding.EncodeToString([]byte(id))
}
