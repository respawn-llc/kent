package workflowsvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"core/server/workflow"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func (s *Service) ObserveWorkflowTask(ctx context.Context, req *taskpb.ObserveRequest) (*taskpb.ObserveSuccess, error) {
	sub, err := s.events.subscribe(&req.ProjectId, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sub.Close() }()

	for {
		response, ready, err := s.observeWorkflowTask(ctx, req)
		if err != nil || ready {
			return response, err
		}
		for {
			event, err := sub.Next(ctx)
			if err != nil {
				return nil, normalizeTaskObservationError(err)
			}
			if event.Resource == pb.ProjectEventResource_WORKFLOW_PROJECT_EVENT_RESOURCE_TASK &&
				(event.PrimaryEntityId == req.TaskId || slices.Contains(event.RelatedIds, req.TaskId)) {
				break
			}
		}
	}
}

func normalizeTaskObservationError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return serverapi.ErrWorkflowTaskNotFound
	}
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: task observation event stream closed: %v", serverapi.ErrStreamFailed, err)
	}
	return serverapi.NormalizeStreamError(err)
}

func (s *Service) observeWorkflowTask(ctx context.Context, req *taskpb.ObserveRequest) (*taskpb.ObserveSuccess, bool, error) {
	detail, err := s.readModels.TaskDetail.GetTask(ctx, req.TaskId)
	if err != nil {
		return nil, false, normalizeTaskObservationError(err)
	}
	if detail.Summary.ProjectId != req.ProjectId {
		return nil, false, errors.New("workflow task does not belong to project")
	}
	response := &taskpb.ObserveSuccess{TaskId: detail.Summary.Id, TaskShortId: detail.Summary.ShortId}
	if detail.Status.Kind == taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE {
		response.Outcomes = []*taskpb.ObserveOutcome{{Outcome: &taskpb.ObserveOutcome_Done{Done: &taskpb.ObserveDone{}}}}
		return response, true, nil
	}

	workflowID, err := runtimeids.WorkflowIDFromProto(detail.Summary.WorkflowId)
	if err != nil {
		return nil, false, err
	}
	definition, _, err := s.readModels.Definitions.GetDefinition(ctx, workflowID)
	if err != nil {
		return nil, false, err
	}
	nodeKeys := make(map[string]string, len(definition.Nodes))
	nodes := make(map[string]*pb.WorkflowNode, len(definition.Nodes))
	for _, node := range definition.Nodes {
		nodeKeys[node.Id] = node.Key
		nodes[node.Id] = node
	}
	currentNodes, err := s.readModels.TaskDetail.ListCurrentNodes(ctx, req.TaskId)
	if err != nil {
		return nil, false, normalizeTaskObservationError(err)
	}
	for _, currentNode := range currentNodes {
		if currentNode.Scheduling == nil || currentNode.Scheduling.Interruption == nil {
			continue
		}
		outcome, err := taskCurrentNodeFailure(currentNode, nodes, nodeKeys)
		if err != nil {
			return nil, false, err
		}
		failure := outcome.GetExecutionError()
		if failure == nil {
			failure = outcome.GetInterrupted()
		}
		if failure != nil && failure.SessionId != nil {
			identity, err := s.metadata.SessionIdentity(ctx, *currentNode.SessionID)
			if err != nil {
				return nil, false, err
			}
			failure.ShortId = protoapi.SessionShortIDToProto(identity.ShortID)
		}
		if req.Mode == taskpb.ObservationMode_OBSERVATION_MODE_WATCH ||
			outcome.GetExecutionError() != nil ||
			outcome.GetInterrupted() != nil {
			response.Outcomes = append(response.Outcomes, outcome)
		}
	}

	attention, err := s.readModels.Attention.ListTask(ctx, &taskpb.TaskAttentionListRequest{TaskId: req.TaskId})
	if err != nil {
		return nil, false, normalizeTaskObservationError(err)
	}
	if req.Mode == taskpb.ObservationMode_OBSERVATION_MODE_WATCH {
		for _, item := range attention.Items {
			if item.Kind != taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_QUESTION {
				continue
			}
			outcome, err := taskQuestion(item, nodeKeys)
			if err != nil {
				return nil, false, err
			}
			response.Outcomes = append(response.Outcomes, outcome)
		}
	}
	return response, len(response.Outcomes) > 0, nil
}

func taskQuestion(
	item *taskpb.AttentionItem,
	keys map[string]string,
) (*taskpb.ObserveOutcome, error) {
	detail := item.GetQuestion()
	question := detail.GetQuestion()
	var sessionID string
	var shortID *int64
	switch selected := question.GetQuestion().(type) {
	case *promptpb.ObservationQuestion_Ask:
		sessionID, shortID = selected.Ask.SessionId, selected.Ask.ShortId
	case *promptpb.ObservationQuestion_Approval:
		sessionID, shortID = selected.Approval.SessionId, selected.Approval.ShortId
	default:
		return nil, errors.New("task attention has no Question or Approval")
	}
	return &taskpb.ObserveOutcome{
		Outcome: &taskpb.ObserveOutcome_Question{Question: &taskpb.ObserveQuestion{
			ShortId:   shortID,
			SessionId: &sessionID,
			NodeKey:   nodeKey(detail.CurrentNode, keys),
			Question:  question,
		}},
	}, nil
}

func taskCurrentNodeFailure(
	currentNode workflow.CurrentNode,
	nodes map[string]*pb.WorkflowNode,
	keys map[string]string,
) (*taskpb.ObserveOutcome, error) {
	interruption := currentNode.Scheduling.Interruption
	if interruption == nil {
		return nil, errors.New("current node interruption is required")
	}
	reason := strings.TrimSpace(string(interruption.Reason))
	if reason == "" {
		return nil, errors.New("task interruption reason is required")
	}
	failure := &promptpb.LiveWatchFailure{Reason: strings.TrimSpace(interruption.Detail.Code)}
	if failure.Reason == "" {
		failure.Reason = reason
	}
	failure.Diagnostic = interruption.Detail.Diagnostic()
	var sessionID *string
	if currentNode.SessionID != nil {
		value := currentNode.SessionID.String()
		sessionID = &value
	}
	var scriptPath *string
	if node, ok := nodes[string(currentNode.Reference.NodeID)]; ok && node.ScriptPath != nil {
		value := *node.ScriptPath
		if strings.TrimSpace(value) != "" {
			scriptPath = &value
			sessionID = nil
		}
	}
	detail := &taskpb.ObserveFailure{
		SessionId:  sessionID,
		ScriptPath: scriptPath,
		NodeKey:    nodeKey(&taskpb.AttentionCurrentNode{NodeId: string(currentNode.Reference.NodeID)}, keys),
		Failure:    failure,
	}
	if interruption.Reason == workflow.CurrentNodeInterruptionReasonUserInterrupt ||
		interruption.Reason == workflow.CurrentNodeInterruptionReasonRuntimeCanceled {
		return &taskpb.ObserveOutcome{Outcome: &taskpb.ObserveOutcome_Interrupted{Interrupted: detail}}, nil
	}
	return &taskpb.ObserveOutcome{Outcome: &taskpb.ObserveOutcome_ExecutionError{ExecutionError: detail}}, nil
}

func nodeKey(node *taskpb.AttentionCurrentNode, keys map[string]string) *string {
	if node == nil {
		return nil
	}
	key := strings.TrimSpace(keys[node.NodeId])
	if key == "" {
		return nil
	}
	return &key
}
