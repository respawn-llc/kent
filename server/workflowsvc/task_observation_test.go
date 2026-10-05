package workflowsvc

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"core/server/registry"
	"core/server/tools"
	"core/server/workflow"
	"core/shared/clientui"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"core/shared/serverapi"
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

type observationPendingPromptSourceStub struct {
	items []registry.PendingPromptSnapshot
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

type observationDefinitionStub struct{}

func (observationDefinitionStub) GetDefinition(context.Context, runtimeids.WorkflowID) (*pb.WorkflowDefinition, map[string]workflow.NodeKind, error) {
	return &pb.WorkflowDefinition{}, nil, nil
}

type observationAttentionStub struct{}

func (observationAttentionStub) List(context.Context, *taskpb.AttentionListRequest) (*taskpb.AttentionListSuccess, error) {
	return &taskpb.AttentionListSuccess{}, nil
}

func (observationAttentionStub) ListTask(context.Context, *taskpb.TaskAttentionListRequest) (*taskpb.TaskAttentionListSuccess, error) {
	return &taskpb.TaskAttentionListSuccess{}, nil
}

func (s observationPendingPromptSourceStub) ListPendingPrompts(string) []registry.PendingPromptSnapshot {
	return append([]registry.PendingPromptSnapshot(nil), s.items...)
}

func TestObserveWorkflowTaskWaitReturnsInterruptedOutcome(t *testing.T) {
	taskID := "task-1"
	projectID := "project-1"
	workflowID := runtimeids.NewWorkflowID()
	currentNode, err := workflow.NewCurrentNodeReference(workflow.TaskID(taskID), "node-1", nil)
	if err != nil {
		t.Fatalf("current node reference: %v", err)
	}
	service := &Service{readModels: ReadModels{
		Definitions: observationDefinitionStub{},
		TaskDetail: observationTaskDetailStub{
			detail: &taskpb.TaskDetail{Summary: &taskpb.TaskSummary{
				Id: taskID, ProjectId: projectID, WorkflowId: workflowID.String(), ShortId: "T-1",
			}, Status: &taskpb.TaskStatus{Kind: taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED, NativeState: taskpb.TaskNativeState_TASK_NATIVE_STATE_INTERRUPTED}},
			nodes: []workflow.CurrentNode{{
				Reference: currentNode,
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

func TestTaskQuestionResolvesLiveAccessThroughAuthoritativePendingPromptSource(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	stepID, err := runtimeids.ParseStepID("55555555-5555-4555-8555-555555555555")
	if err != nil {
		t.Fatalf("ParseStepID: %v", err)
	}
	questionID := "access-1"
	message := "Allow access?"
	createdAt := time.UnixMilli(42).UTC()
	service := &Service{readModels: ReadModels{
		PendingPrompts: observationPendingPromptSourceStub{items: []registry.PendingPromptSnapshot{{
			Request: tools.AskQuestionRequest{
				ToolCallID: questionID, StepID: stepID.String(), Question: message, Approval: true,
				ApprovalOptions: []tools.AskQuestionApprovalOption{{Decision: tools.AskQuestionApprovalDecisionAllowOnce}},
			},
			CreatedAt: createdAt,
		}}},
	}}
	item := &taskpb.AttentionItem{
		Kind: taskpb.AttentionItemKind_ATTENTION_ITEM_KIND_QUESTION,
		Detail: &taskpb.AttentionItem_Question{Question: &taskpb.QuestionAttention{
			Message: &message,
			Question: &taskpb.AttentionQuestionPrompt{
				SessionId: sessionID.String(), StepId: stepID.String(), ToolCallId: questionID,
				Kind: taskpb.AttentionQuestionKind_ATTENTION_QUESTION_KIND_APPROVAL,
				Prompt: &taskpb.AttentionQuestionPrompt_Approval{Approval: &taskpb.ApprovalQuestion{
					ApprovalDecisions: []promptpb.ApprovalDecision{promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_ONCE},
				}},
			},
		}},
		OccurredAt: timestamppb.New(createdAt),
	}
	outcome, ok, err := service.taskQuestion(context.Background(), item, nil, map[string][]clientui.PendingApproval{})
	if err != nil || !ok {
		t.Fatalf("task question = %+v, ok=%v, err=%v", outcome, ok, err)
	}
	question := outcome.GetQuestion().GetQuestion().GetApproval()
	if question == nil || question.Options[0].Decision != promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_ONCE {
		t.Fatalf("question = %+v", outcome)
	}
}
