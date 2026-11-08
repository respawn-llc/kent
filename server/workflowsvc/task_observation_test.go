package workflowsvc

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"core/server/workflow"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNormalizeTaskObservationErrorClassifiesClosedEventStream(t *testing.T) {
	err := normalizeTaskObservationError(io.EOF)
	if !errors.Is(err, serverapi.ErrStreamFailed) {
		t.Fatalf("normalized error = %v, want stream failure", err)
	}
	if errors.Is(err, io.EOF) {
		t.Fatalf("normalized error = %v, must not remain raw EOF", err)
	}
}

type observationTaskDetailStub struct {
	detail *taskpb.TaskDetail
	nodes  []workflow.CurrentNode
}

func (s observationTaskDetailStub) GetTask(context.Context, string) (*taskpb.TaskDetail, error) {
	return s.detail, nil
}

func (s observationTaskDetailStub) GetTaskByProjectShortID(context.Context, string, string) (*taskpb.TaskDetail, error) {
	return s.detail, nil
}

func (s observationTaskDetailStub) GetTaskByShortID(context.Context, string) (*taskpb.TaskDetail, error) {
	return s.detail, nil
}

func (s observationTaskDetailStub) ListCurrentNodes(context.Context, string) ([]workflow.CurrentNode, error) {
	return s.nodes, nil
}

type observationDefinitionStub struct {
	definition *pb.WorkflowDefinition
}

func (s observationDefinitionStub) GetDefinition(context.Context, runtimeids.WorkflowID) (*pb.WorkflowDefinition, map[string]workflow.NodeKind, error) {
	if s.definition != nil {
		return s.definition, nil, nil
	}
	return &pb.WorkflowDefinition{}, nil, nil
}

type observationAttentionStub struct {
	items []*taskpb.AttentionItem
}

func (observationAttentionStub) List(context.Context, *taskpb.AttentionListRequest) (*taskpb.AttentionListSuccess, error) {
	return &taskpb.AttentionListSuccess{}, nil
}

func (s observationAttentionStub) ListTask(context.Context, *taskpb.TaskAttentionListRequest) (*taskpb.TaskAttentionListSuccess, error) {
	return &taskpb.TaskAttentionListSuccess{Items: s.items}, nil
}

func TestObserveWorkflowTaskWaitReturnsInterruptedOutcome(t *testing.T) {
	_, _, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	sessionID := createPersistedWorkflowServiceSession(t, metadataStore, binding)
	record, err := metadataStore.ResolvePersistedSession(t.Context(), sessionID.String())
	if err != nil {
		t.Fatal(err)
	}
	taskID := "task-1"
	projectID := "project-1"
	workflowID := runtimeids.NewWorkflowID()
	currentNode, err := workflow.NewCurrentNodeReference(workflow.TaskID(taskID), "node-1", nil)
	if err != nil {
		t.Fatalf("current node reference: %v", err)
	}
	service := &Service{metadata: metadataStore, readModels: ReadModels{
		Definitions: observationDefinitionStub{},
		TaskDetail: observationTaskDetailStub{
			detail: &taskpb.TaskDetail{Summary: &taskpb.TaskSummary{
				Id: taskID, ProjectId: projectID, WorkflowId: workflowID.String(), ShortId: "T-1",
			}, Status: &taskpb.TaskStatus{Kind: taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED, NativeState: taskpb.TaskNativeState_TASK_NATIVE_STATE_INTERRUPTED}},
			nodes: []workflow.CurrentNode{{
				Reference: currentNode,
				SessionID: &sessionID,
				Scheduling: &workflow.CurrentNodeScheduling{
					State: workflow.CurrentNodeSchedulingInterrupted,
					Interruption: &workflow.CurrentNodeInterruption{
						Reason: workflow.CurrentNodeInterruptionReasonUserInterrupt,
						Detail: workflow.NewCurrentNodeInterruptionDetail("user_interrupt", nil),
					},
				},
			}},
		},
		Attention: observationAttentionStub{},
	}}
	response, ready, err := service.observeWorkflowTask(context.Background(), &taskpb.ObserveRequest{
		TaskId: taskID, ProjectId: projectID, Mode: taskpb.ObservationMode_OBSERVATION_MODE_WAIT,
	})
	if err != nil {
		t.Fatalf("observe workflow task: %v", err)
	}
	if !ready || len(response.Outcomes) != 1 {
		t.Fatalf("response = %+v, ready=%v; want one ready interruption", response, ready)
	}
	if response.Outcomes[0].GetInterrupted() == nil {
		t.Fatalf("outcome = %v, want interruption", response.Outcomes[0])
	}
	failure := response.Outcomes[0].GetInterrupted()
	if failure.GetSessionId() != sessionID.String() || record.ShortID == nil ||
		failure.ShortId == nil || *failure.ShortId != record.ShortID.Int64() {
		t.Fatalf("observed Session identity = %v, want %s/%v", failure, sessionID, record.ShortID)
	}
}

func TestTaskCurrentNodeFailureUsesDefinitionIdentityAndDiagnostic(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	currentNode, err := workflow.NewCurrentNodeReference("task-1", "node-script", nil)
	if err != nil {
		t.Fatalf("current node reference: %v", err)
	}
	node := workflow.CurrentNode{
		Reference: currentNode,
		SessionID: &sessionID,
		Scheduling: &workflow.CurrentNodeScheduling{
			State: workflow.CurrentNodeSchedulingInterrupted,
			Interruption: &workflow.CurrentNodeInterruption{
				Reason: workflow.CurrentNodeInterruptionReason("script_failed"),
				Detail: workflow.NewCurrentNodeInterruptionDetail("script_failed", errors.New("script stopped")),
			},
		},
	}
	scriptPath := "scripts/check.sh"
	outcome, err := taskCurrentNodeFailure(
		node,
		map[string]*pb.WorkflowNode{"node-script": {Id: "node-script", Key: "check", ScriptPath: &scriptPath}},
		map[string]string{"node-script": "check"},
	)
	if err != nil {
		t.Fatalf("task current node failure: %v", err)
	}
	detail := outcome.GetExecutionError()
	if detail == nil || detail.SessionId != nil ||
		detail.ScriptPath == nil || *detail.ScriptPath != scriptPath ||
		detail.NodeKey == nil || *detail.NodeKey != "check" ||
		detail.Failure == nil || detail.Failure.Diagnostic == nil || *detail.Failure.Diagnostic != "script stopped" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestObserveWorkflowTaskRetainsCompletedQuestionProjection(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	stepID, err := runtimeids.ParseStepID("55555555-5555-4555-8555-555555555555")
	if err != nil {
		t.Fatalf("ParseStepID: %v", err)
	}
	shortID := int64(42)
	createdAt := time.UnixMilli(42).UTC()
	prompts := []*promptpb.ObservationQuestion{
		{Question: &promptpb.ObservationQuestion_Approval{Approval: &promptpb.Approval{
			SessionId: sessionID.String(), ShortId: &shortID, StepId: stepID.String(), ToolCallId: "access-1",
			CreatedAt: timestamppb.New(createdAt),
			AccessTargets: []*promptpb.FileAccessTarget{
				{RequestedPath: "/alias/a", ResolvedPath: "/real/file"},
				{RequestedPath: "/alias/b", ResolvedPath: "/real/file"},
			},
			Options: []*promptpb.ApprovalOption{
				{Decision: promptpb.ApprovalDecision_APPROVAL_DECISION_DENY},
				{Decision: promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_ONCE},
			},
			Batch: &promptpb.QuestionBatch{ToolCallIds: []string{"access-1", "ask-1"}, UnmaterializedCount: 1},
		}}},
		{Question: &promptpb.ObservationQuestion_Ask{Ask: &promptpb.Question{
			SessionId: sessionID.String(), ShortId: &shortID, StepId: stepID.String(), ToolCallId: "ask-1",
			Question: "Continue?", Suggestions: []string{"Continue", "Stop"},
			CreatedAt: timestamppb.New(createdAt.Add(time.Second)),
			Batch:     &promptpb.QuestionBatch{ToolCallIds: []string{"access-1", "ask-1"}, UnmaterializedCount: 1},
		}}},
	}
	items := make([]*taskpb.AttentionItem, 0, len(prompts))
	expected := make([]*promptpb.ObservationQuestion, 0, len(prompts))
	for i, prompt := range prompts {
		expected = append(expected, proto.Clone(prompt).(*promptpb.ObservationQuestion))
		items = append(items, &taskpb.AttentionItem{
			Kind: taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_QUESTION,
			Detail: &taskpb.AttentionItem_Question{Question: &taskpb.QuestionAttention{
				CurrentNode: &taskpb.AttentionCurrentNode{NodeId: "node-1"},
				Question:    prompt,
			}},
			OccurredAt: timestamppb.New(createdAt.Add(time.Duration(len(prompts)-i) * time.Second)),
		})
	}
	service := &Service{readModels: ReadModels{
		Definitions: observationDefinitionStub{definition: &pb.WorkflowDefinition{
			Nodes: []*pb.WorkflowNode{{Id: "node-1", Key: "implement"}},
		}},
		TaskDetail: observationTaskDetailStub{detail: &taskpb.TaskDetail{
			Summary: &taskpb.TaskSummary{
				Id: "task-1", ProjectId: "project-1", WorkflowId: runtimeids.NewWorkflowID().String(), ShortId: "T-1",
			},
			Status: &taskpb.TaskStatus{Kind: taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION},
		}},
		Attention: observationAttentionStub{items: items},
	}}
	// Only the completed attention result is available; no live prompt source is needed.
	service.events = newWorkflowProjectEventBroker()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	response, err := service.ObserveWorkflowTask(ctx, &taskpb.ObserveRequest{
		TaskId: "task-1", ProjectId: "project-1", Mode: taskpb.ObservationMode_OBSERVATION_MODE_WATCH,
	})
	if err != nil || len(response.Outcomes) != len(prompts) {
		t.Fatalf("observe = %+v, err=%v", response, err)
	}
	for i, outcome := range response.Outcomes {
		question := outcome.GetQuestion()
		if question == nil || !proto.Equal(question.Question, expected[i]) ||
			question.GetSessionId() != sessionID.String() || question.ShortId == nil || *question.ShortId != shortID ||
			question.NodeKey == nil || *question.NodeKey != "implement" {
			t.Fatalf("observed question %d = %+v, want canonical prompt %+v", i, question, expected[i])
		}
	}
}
