package workflowview

import (
	"core/internal/testharness/workflowfixture"
	"errors"
	"testing"
	"time"

	"core/server/tools"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/clientui"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAttentionProjectsPendingApprovalAndInterruptedCurrentNode(t *testing.T) {
	approvalFixture := newCurrentNodeViewFixture(t, true)
	approvalStarted := approvalFixture.startTask(t, "Approval task")
	completed, err := workflowfixture.CompleteCurrentNode(t, approvalFixture.ctx, approvalFixture.metadata, approvalFixture.store, workflowstore.CurrentNodeCompletionRequest{
		Source:       approvalStarted.currentNode,
		TransitionID: "done",
		Commentary:   "Ready to merge.",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	if completed.PendingApproval == nil {
		t.Fatal("CompleteCurrentNode did not create a pending Approval")
	}

	interruptedFixture := newCurrentNodeViewFixture(t, false)
	interruptedStarted := interruptedFixture.startTask(t, "Interrupted task")
	interruptedSessionID := interruptedFixture.bindCurrentNodeSession(t, interruptedStarted)
	if err := interruptedFixture.store.InterruptCurrentNode(
		interruptedFixture.ctx,
		interruptedStarted.currentNode,
		workflow.CurrentNodeInterruptionReason("server_restart"),
		workflow.CurrentNodeInterruptionDetail{Code: "restart", Fields: map[string]string{"error": "process stopped"}},
	); err != nil {
		t.Fatalf("InterruptCurrentNode: %v", err)
	}

	approvalAttention := approvalFixture.attention(t)
	approvals, err := approvalAttention.ListTask(approvalFixture.ctx, &taskpb.TaskAttentionListRequest{TaskId: string(approvalStarted.task.ID)})
	if err != nil {
		t.Fatalf("Attention.ListTask approval: %v", err)
	}
	if len(approvals.Items) != 1 {
		t.Fatalf("approval attention = %+v, want one approval", approvals.Items)
	}
	approval := approvals.Items[0]
	if approval.Kind != taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_APPROVAL ||
		approval.GetApproval() == nil ||
		approval.GetApproval().ApprovalId != completed.PendingApproval.ID.String() ||
		approval.GetApproval().ApprovalSnapshot == nil ||
		approval.GetApproval().ApprovalSnapshot.GetCommentary() != "Ready to merge." {
		t.Fatalf("approval attention item = %+v, want pending Approval identity", approval)
	}
	requireAttentionMessageOmitted(t, approval)

	interruptedAttention := interruptedFixture.attention(t)
	interruptions, err := interruptedAttention.List(interruptedFixture.ctx, &taskpb.AttentionListRequest{PageSize: 20})
	if err != nil {
		t.Fatalf("Attention.List interrupted: %v", err)
	}
	if len(interruptions.Items) != 1 {
		t.Fatalf("interrupted attention = %+v, want one interrupted Current Node", interruptions.Items)
	}
	interrupted := interruptions.Items[0]
	detail := interrupted.GetInterruptedCurrentNode()
	if interrupted.Kind != taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_INTERRUPTED_CURRENT_NODE ||
		interrupted.TaskId != string(interruptedStarted.task.ID) ||
		detail == nil || detail.CurrentNode == nil ||
		detail.CurrentNode.NodeId != string(interruptedFixture.agentNodeID) ||
		detail.CurrentNode.GetSessionId() != interruptedSessionID.String() ||
		detail.GetSessionId() != interruptedSessionID.String() ||
		detail.Details == nil || detail.Details.Code != "restart" {
		t.Fatalf("interrupted attention item = %+v, want Current Node identity", interrupted)
	}
	requireAttentionMessageOmitted(t, interrupted)
	taskInterruptions, err := interruptedAttention.ListTask(interruptedFixture.ctx, &taskpb.TaskAttentionListRequest{TaskId: string(interruptedStarted.task.ID)})
	if err != nil {
		t.Fatalf("Attention.ListTask interrupted: %v", err)
	}
	if len(taskInterruptions.Items) != 1 || taskInterruptions.Items[0].Id != interrupted.Id {
		t.Fatalf("task interrupted attention = %+v, want exact Current Node attention", taskInterruptions.Items)
	}
}

func requireAttentionMessageOmitted(t *testing.T, item *taskpb.AttentionItem) {
	t.Helper()
	var message *string
	switch detail := item.Detail.(type) {
	case *taskpb.AttentionItem_Question:
		message = detail.Question.Message
	case *taskpb.AttentionItem_Approval:
		message = detail.Approval.Message
	case *taskpb.AttentionItem_InterruptedCurrentNode:
		message = detail.InterruptedCurrentNode.Message
	default:
		t.Fatalf("attention detail is required: %T", detail)
	}
	if message != nil {
		t.Fatal("attention item supplied fallback copy")
	}
}

func TestAttentionPaginatesDurableCurrentStateAndScopesTaskQuery(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, true)
	approvalTask := fixture.startTask(t, "Approval")
	completed, err := workflowfixture.CompleteCurrentNode(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.CurrentNodeCompletionRequest{
		Source:       approvalTask.currentNode,
		TransitionID: "done",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	if completed.PendingApproval == nil {
		t.Fatal("pending Approval is missing")
	}
	firstInterrupted := fixture.startTask(t, "Interrupted first")
	secondInterrupted := fixture.startTask(t, "Interrupted second")
	for _, task := range []startedCurrentNodeViewTask{firstInterrupted, secondInterrupted} {
		if err := fixture.store.InterruptCurrentNode(
			fixture.ctx,
			task.currentNode,
			workflow.CurrentNodeInterruptionReason("server_restart"),
			workflow.CurrentNodeInterruptionDetail{Code: "restart"},
		); err != nil {
			t.Fatalf("InterruptCurrentNode %s: %v", task.task.ID, err)
		}
	}
	fixture.setApprovalCreatedAt(t, completed.PendingApproval.ID.String(), 3_000)
	fixture.setCurrentNodeInterruptedAt(t, firstInterrupted.currentNode, 2_000)
	fixture.setCurrentNodeInterruptedAt(t, secondInterrupted.currentNode, 1_000)
	attention := fixture.attention(t)

	first, err := attention.List(fixture.ctx, &taskpb.AttentionListRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("Attention.List first: %v", err)
	}
	if len(first.Items) != 2 ||
		first.Items[0].Kind != taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_APPROVAL ||
		first.Items[1].TaskId != string(firstInterrupted.task.ID) ||
		first.NextPageToken == nil {
		t.Fatalf("first attention page = %+v token %v", first.Items, first.NextPageToken)
	}
	second, err := attention.List(fixture.ctx, &taskpb.AttentionListRequest{
		PageSize:  2,
		PageToken: first.NextPageToken,
	})
	if err != nil {
		t.Fatalf("Attention.List second: %v", err)
	}
	if len(second.Items) != 1 ||
		second.Items[0].TaskId != string(secondInterrupted.task.ID) ||
		second.NextPageToken != nil {
		t.Fatalf("second attention page = %+v token %v", second.Items, second.NextPageToken)
	}
	scoped, err := attention.ListTask(fixture.ctx, &taskpb.TaskAttentionListRequest{
		TaskId: string(secondInterrupted.task.ID),
	})
	if err != nil {
		t.Fatalf("Attention.ListTask: %v", err)
	}
	if len(scoped.Items) != 1 || scoped.Items[0].TaskId != string(secondInterrupted.task.ID) {
		t.Fatalf("task-scoped attention = %+v", scoped.Items)
	}
	if _, err := attention.List(fixture.ctx, &taskpb.AttentionListRequest{
		PageSize:  2,
		PageToken: proto.String("invalid"),
	}); !errors.Is(err, ErrInvalidPageToken) {
		t.Fatalf("invalid attention page token error = %v, want invalid page token", err)
	}
}

func TestAttentionAndDetailProjectLiveQuestionFromExactScope(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "Question")
	unrelated := fixture.startTask(t, "Unrelated")
	question := fixture.startCurrentNodeQuestion(t, started)
	projector := NewTaskProjector()
	questionProjection, err := NewTaskStatusProjection(
		fixture.store,
		projector,
		&currentNodeViewStatusObservationSource{
			authority: question.authority,
			blocked:   fixture.quiescence.blocked,
		},
	)
	if err != nil {
		t.Fatalf("NewTaskStatusProjection question: %v", err)
	}
	taskList, err := NewTaskList(
		fixture.metadata,
		mustDefinitionProjection(t, fixture.store),
		questionProjection,
	)
	if err != nil {
		t.Fatalf("NewTaskList: %v", err)
	}
	projectID := fixture.binding.ProjectID
	limit := int32(20)
	listed, err := taskList.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId:   &projectID,
		StatusKinds: []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION},
		LabelFilter: noLabelFilter(),
		Limit:       &limit,
	})
	if err != nil {
		t.Fatalf("TaskList.List waiting question: %v", err)
	}
	if len(listed.Tasks) != 1 ||
		listed.Tasks[0].TaskId != string(started.task.ID) ||
		listed.Tasks[0].Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION ||
		len(listed.Tasks[0].Status.AttentionTypes) != 1 ||
		listed.Tasks[0].Status.AttentionTypes[0] != taskpb.TaskAttentionKind_TASK_ATTENTION_KIND_QUESTION {
		t.Fatalf("task list waiting-question projection = %+v", listed)
	}
	prompts := currentNodeViewPrompts{bySession: map[string][]PendingPromptSnapshot{
		question.sessionID.String(): {{
			ToolCallID:             clientui.ToolCallID(question.request.ToolCallID),
			SessionID:              question.sessionID,
			StepID:                 mustWorkflowViewStepID(t, question.request.StepID),
			CreatedAt:              time.UnixMilli(4_000).UTC(),
			Question:               question.request.Question,
			Suggestions:            question.request.Suggestions,
			RecommendedOptionIndex: intPointer(question.request.RecommendedOptionIndex),
		}, {
			ToolCallID:        clientui.ToolCallID("unrelated-approval"),
			CreatedAt:         time.UnixMilli(4_001).UTC(),
			Question:          "Approve unrelated action?",
			Approval:          true,
			ApprovalDecisions: []clientui.ApprovalDecision{clientui.ApprovalDecisionAllowOnce},
		}},
	}}
	attention, err := NewAttention(
		fixture.metadata,
		mustDefinitionProjection(t, fixture.store),
		question.authority,
		prompts,
	)
	if err != nil {
		t.Fatalf("NewAttention: %v", err)
	}
	taskAttention, err := attention.ListTask(fixture.ctx, &taskpb.TaskAttentionListRequest{
		TaskId: string(started.task.ID),
	})
	if err != nil {
		t.Fatalf("Attention.ListTask: %v", err)
	}
	if len(taskAttention.Items) != 1 {
		t.Fatalf("question attention = %+v", taskAttention.Items)
	}
	item := taskAttention.Items[0]
	prompt := item.GetQuestion()
	if item.Kind != taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_QUESTION ||
		prompt == nil || prompt.GetMessage() != question.request.Question ||
		prompt.GetSessionName() != "Current Node session" || prompt.Question == nil ||
		prompt.Question.ToolCallId != question.request.ToolCallID ||
		prompt.Question.SessionId != question.sessionID.String() ||
		prompt.Question.StepId != mustWorkflowViewStepID(t, question.request.StepID).String() ||
		prompt.CurrentNode == nil || prompt.CurrentNode.SessionId != nil ||
		prompt.CurrentNode.NodeId != string(fixture.agentNodeID) {
		t.Fatalf("question attention = %+v", taskAttention.Items)
	}
	unrelatedAttention, err := attention.ListTask(fixture.ctx, &taskpb.TaskAttentionListRequest{
		TaskId: string(unrelated.task.ID),
	})
	if err != nil {
		t.Fatalf("Attention.ListTask unrelated: %v", err)
	}
	if len(unrelatedAttention.Items) != 0 {
		t.Fatalf("unrelated task attention = %+v, want none", unrelatedAttention.Items)
	}
	dependencies, err := NewTaskDependencies(fixture.metadata, questionProjection, fixture.dependencyCounter)
	if err != nil {
		t.Fatalf("NewTaskDependencies: %v", err)
	}
	detail, err := NewTaskDetail(fixture.metadata, questionProjection, dependencies)
	if err != nil {
		t.Fatalf("NewTaskDetail: %v", err)
	}
	projected, err := detail.GetTask(fixture.ctx, string(started.task.ID))
	if err != nil {
		t.Fatalf("TaskDetail.GetTask: %v", err)
	}
	if projected.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION ||
		projected.AttentionCount != 1 ||
		len(projected.LiveSessions) != 1 ||
		projected.LiveSessions[0].SessionId != question.sessionID.String() ||
		projected.LiveSessions[0].SessionName == nil ||
		*projected.LiveSessions[0].SessionName != "Current Node session" ||
		projected.LiveSessions[0].NodeDisplayName != "Agent" {
		t.Fatalf("question task detail = %+v", projected)
	}
	question.resolve(t, fixture.ctx)
}

func TestAttentionProjectsLiveSessionApprovalFromExactScope(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "Live approval")
	request := workflowViewApprovalRequest()
	request.Question = ""
	prompt := fixture.startCurrentNodePrompt(t, started, request)
	prompts := currentNodeViewPrompts{bySession: map[string][]PendingPromptSnapshot{
		prompt.sessionID.String(): {{
			ToolCallID:        clientui.ToolCallID(request.ToolCallID),
			SessionID:         prompt.sessionID,
			StepID:            mustWorkflowViewStepID(t, request.StepID),
			CreatedAt:         time.UnixMilli(4_000).UTC(),
			Question:          request.Question,
			Approval:          true,
			ApprovalDecisions: []clientui.ApprovalDecision{clientui.ApprovalDecisionAllowOnce, clientui.ApprovalDecisionDeny},
			AccessTargets: []clientui.FileAccessTarget{{
				RequestedPath: "../outside.txt",
				ResolvedPath:  "/outside.txt",
			}},
		}},
	}}
	attention, err := NewAttention(
		fixture.metadata,
		mustDefinitionProjection(t, fixture.store),
		prompt.authority,
		prompts,
	)
	if err != nil {
		t.Fatalf("NewAttention: %v", err)
	}
	response, err := attention.ListTask(fixture.ctx, &taskpb.TaskAttentionListRequest{
		TaskId: string(started.task.ID),
	})
	if err != nil {
		t.Fatalf("Attention.ListTask: %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("live approval attention = %+v, want one item", response.Items)
	}
	item := response.Items[0]
	if err := protoapi.Validate(response); err != nil {
		t.Fatalf("validate structured approval attention: %v", err)
	}
	requireAttentionMessageOmitted(t, item)
	question := item.GetQuestion()
	if item.Kind != taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_QUESTION ||
		question == nil || question.Question == nil ||
		question.Question.ToolCallId != request.ToolCallID ||
		question.Question.SessionId != prompt.sessionID.String() ||
		question.Question.StepId != mustWorkflowViewStepID(t, request.StepID).String() ||
		question.Question.Kind != taskpb.AttentionQuestionKind_ATTENTION_QUESTION_KIND_APPROVAL ||
		question.Question.GetApproval() == nil ||
		len(question.Question.GetApproval().AccessTargets) != 1 ||
		question.Question.GetApproval().AccessTargets[0].RequestedPath != "../outside.txt" ||
		question.Question.GetApproval().AccessTargets[0].ResolvedPath != "/outside.txt" ||
		question.CurrentNode == nil || question.CurrentNode.SessionId != nil ||
		question.CurrentNode.NodeId != string(fixture.agentNodeID) {
		t.Fatalf("live approval attention item = %+v", item)
	}
	prompt.resolve(t, fixture.ctx, tools.AskQuestionApproval{
		Decision: tools.AskQuestionApprovalDecisionAllowOnce,
	})
}

func mustWorkflowViewStepID(t *testing.T, raw string) runtimeids.StepID {
	t.Helper()
	id, err := runtimeids.ParseStepID(raw)
	if err != nil {
		t.Fatalf("ParseStepID(%q): %v", raw, err)
	}
	return id
}

func TestMergeAttentionCandidatesPreservesQuestionStepIdentity(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	toolCallID := clientui.ToolCallID("shared-prompt")
	ids := []string{
		liveQuestionAttentionID(sessionID, mustWorkflowViewStepID(t, "11111111-1111-4111-8111-111111111111"), toolCallID),
		liveQuestionAttentionID(sessionID, mustWorkflowViewStepID(t, "22222222-2222-4222-8222-222222222222"), toolCallID),
	}
	items := mergeAttentionCandidates(
		attentionPageCursor{},
		[]attentionCandidate{{item: &taskpb.AttentionItem{Id: ids[0], OccurredAt: timestamppb.New(time.UnixMilli(1))}}},
		[]attentionCandidate{{item: &taskpb.AttentionItem{Id: ids[1], OccurredAt: timestamppb.New(time.UnixMilli(1))}}},
	)
	page := mergeAttentionCandidates(
		attentionPageCursor{occurredAtUnixMs: 1, itemID: items[0].Id, hasValue: true},
		[]attentionCandidate{{item: items[0]}, {item: items[1]}},
	)
	if ids[0] == ids[1] || len(items) != 2 || len(page) != 1 || page[0].Id != items[1].Id {
		t.Fatalf("full-key merge = ids %v items %+v page %+v", ids, items, page)
	}
}

func TestAttentionOmitsLivePromptThatRetiredBeforePromptProjection(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "Retired prompt")
	request := workflowViewApprovalRequest()
	prompt := fixture.startCurrentNodePrompt(t, started, request)
	attention, err := NewAttention(
		fixture.metadata,
		mustDefinitionProjection(t, fixture.store),
		prompt.authority,
		currentNodeViewPrompts{},
	)
	if err != nil {
		t.Fatalf("NewAttention: %v", err)
	}
	response, err := attention.ListTask(fixture.ctx, &taskpb.TaskAttentionListRequest{
		TaskId: string(started.task.ID),
	})
	if err != nil {
		t.Fatalf("Attention.ListTask: %v", err)
	}
	if len(response.Items) != 0 {
		t.Fatalf("retired prompt attention = %+v, want no items for session %s", response.Items, prompt.sessionID)
	}
	prompt.resolve(t, fixture.ctx, tools.AskQuestionApproval{
		Decision: tools.AskQuestionApprovalDecisionAllowOnce,
	})
}
