package protoapi_test

import (
	"testing"
	"time"

	"core/shared/protoapi"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestQuestionNotificationDisplayCountMustBePositive(t *testing.T) {
	for _, count := range []int32{-1, 0, 1, 2} {
		if err := protoapi.Validate(&attentionpb.QuestionState{DisplayCount: count}); (err == nil) != (count > 0) {
			t.Errorf("Question display count %d acceptance = %v", count, err)
		}
	}
}

func TestWorkflowQuestionNotificationRequiresQuestionFocus(t *testing.T) {
	for _, questionFocus := range []bool{true, false} {
		focus := &taskpb.AttentionNotificationTaskFocus{
			Focus: &taskpb.AttentionNotificationTaskFocus_Approval{Approval: &taskpb.ApprovalFocus{ApprovalId: "approval-1"}},
		}
		if questionFocus {
			focus = &taskpb.AttentionNotificationTaskFocus{
				Focus: &taskpb.AttentionNotificationTaskFocus_Question{Question: &taskpb.QuestionFocus{AskIds: []string{"ask-1", "ask-2"}}},
			}
		}
		message := &taskpb.AttentionNotification{
			Id:         &taskpb.AttentionNotificationID{Kind: taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_QUESTION, Uuid: "batch-1"},
			OccurredAt: timestamppb.Now(), Revision: 1,
			State: &taskpb.AttentionNotification_Question{Question: &attentionpb.QuestionState{DisplayCount: 2}},
			Target: &taskpb.AttentionNotificationTarget{
				Target: &taskpb.AttentionNotificationTarget_WorkflowTask{WorkflowTask: &taskpb.WorkflowTaskAttentionTarget{
					TaskId: "task-1", WorkflowId: "22222222-2222-4222-8222-222222222222", Focus: focus,
				}},
			},
		}
		if err := protoapi.Validate(message); (err == nil) != questionFocus {
			t.Errorf("Question notification with Question focus %t acceptance = %v", questionFocus, err)
		}
	}
}

func TestAttentionNotificationKeyMustBeNonblank(t *testing.T) {
	for _, value := range []string{" \t ", "approval-key"} {
		message := &taskpb.AttentionNotificationID{
			Kind: taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_WORKFLOW_APPROVAL, Uuid: value,
		}
		if err := protoapi.Validate(message); (err == nil) != (value == "approval-key") {
			t.Errorf("notification key %q acceptance = %v", value, err)
		}
	}
}

func TestAttentionNotificationIdentityMustMatchState(t *testing.T) {
	sessionTarget := &taskpb.AttentionNotificationTarget{
		Target: &taskpb.AttentionNotificationTarget_SessionPrompt{SessionPrompt: &taskpb.SessionPromptAttentionTarget{
			SessionId: "11111111-1111-4111-8111-111111111111", ProjectId: "project-1",
		}},
	}
	workflowTarget := func(focus *taskpb.AttentionNotificationTaskFocus) *taskpb.AttentionNotificationTarget {
		return &taskpb.AttentionNotificationTarget{
			Target: &taskpb.AttentionNotificationTarget_WorkflowTask{WorkflowTask: &taskpb.WorkflowTaskAttentionTarget{
				TaskId: "task-1", WorkflowId: "22222222-2222-4222-8222-222222222222",
				CurrentNodeId: proto.String("node-1"), Focus: focus,
			}},
		}
	}
	variants := []struct {
		kind    taskpb.AttentionNotificationKind
		message *taskpb.AttentionNotification
	}{
		{
			kind: taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_QUESTION,
			message: &taskpb.AttentionNotification{
				State:  &taskpb.AttentionNotification_Question{Question: &attentionpb.QuestionState{DisplayCount: 1}},
				Target: sessionTarget,
			},
		},
		{
			kind: taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_APPROVAL,
			message: &taskpb.AttentionNotification{
				State:  &taskpb.AttentionNotification_Approval{Approval: &attentionpb.ApprovalState{Message: proto.String("Approve access")}},
				Target: sessionTarget,
			},
		},
		{
			kind: taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_WORKFLOW_APPROVAL,
			message: &taskpb.AttentionNotification{
				State: &taskpb.AttentionNotification_WorkflowApproval{WorkflowApproval: &taskpb.WorkflowApprovalState{ApprovalId: "approval-1"}},
				Target: workflowTarget(&taskpb.AttentionNotificationTaskFocus{
					Focus: &taskpb.AttentionNotificationTaskFocus_Approval{Approval: &taskpb.ApprovalFocus{ApprovalId: "approval-1"}},
				}),
			},
		},
		{
			kind: taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_INTERRUPTED_CURRENT_NODE,
			message: &taskpb.AttentionNotification{
				State: &taskpb.AttentionNotification_InterruptedCurrentNode{InterruptedCurrentNode: &taskpb.InterruptedCurrentNodeState{}},
				Target: workflowTarget(&taskpb.AttentionNotificationTaskFocus{
					Focus: &taskpb.AttentionNotificationTaskFocus_InterruptedCurrentNode{InterruptedCurrentNode: &taskpb.InterruptedCurrentNodeFocus{}},
				}),
			},
		},
	}
	for _, variant := range variants {
		for _, identity := range variants {
			message := proto.Clone(variant.message).(*taskpb.AttentionNotification)
			message.Id = &taskpb.AttentionNotificationID{Kind: identity.kind, Uuid: "notification-1"}
			message.OccurredAt = timestamppb.Now()
			message.Revision = 1
			if err := protoapi.Validate(message); (err == nil) != (variant.kind == identity.kind) {
				t.Errorf("state %v with identity %v acceptance = %v", variant.kind, identity.kind, err)
			}
		}
	}
}

func TestAttentionNotificationRejectsGoZeroOccurrenceTime(t *testing.T) {
	for _, occurredAt := range []time.Time{{}, time.Unix(0, 0), time.Unix(-1, 0)} {
		id := &taskpb.AttentionNotificationID{
			Kind: taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_WORKFLOW_APPROVAL,
			Uuid: "approval-key",
		}
		messages := []proto.Message{
			&taskpb.ResolvedAttentionNotification{Id: id, OccurredAt: timestamppb.New(occurredAt)},
			&taskpb.AttentionNotification{
				Id: id, OccurredAt: timestamppb.New(occurredAt), Revision: 1,
				State: &taskpb.AttentionNotification_WorkflowApproval{WorkflowApproval: &taskpb.WorkflowApprovalState{ApprovalId: "approval-key"}},
				Target: &taskpb.AttentionNotificationTarget{
					Target: &taskpb.AttentionNotificationTarget_WorkflowTask{WorkflowTask: &taskpb.WorkflowTaskAttentionTarget{
						TaskId: "task-1", WorkflowId: "22222222-2222-4222-8222-222222222222",
						Focus: &taskpb.AttentionNotificationTaskFocus{
							Focus: &taskpb.AttentionNotificationTaskFocus_Approval{Approval: &taskpb.ApprovalFocus{ApprovalId: "approval-key"}},
						},
					}},
				},
			},
		}
		for _, message := range messages {
			if err := protoapi.Validate(message); (err == nil) != !occurredAt.IsZero() {
				t.Errorf("%T occurrence %v acceptance = %v", message, occurredAt, err)
			}
		}
	}
}

func TestAttentionWorkflowApprovalIdentityMustBeNonblank(t *testing.T) {
	for _, value := range []string{" \t ", "approval-key"} {
		message := &taskpb.WorkflowApprovalState{ApprovalId: value}
		if err := protoapi.Validate(message); (err == nil) != (value == "approval-key") {
			t.Errorf("approval identity %q acceptance = %v", value, err)
		}
	}
}

func TestAttentionSessionPromptProjectMustBeNonblank(t *testing.T) {
	for _, value := range []string{" \t ", "project-key"} {
		message := &taskpb.SessionPromptAttentionTarget{
			ProjectId: value, SessionId: "11111111-1111-4111-8111-111111111111",
		}
		if err := protoapi.Validate(message); (err == nil) != (value == "project-key") {
			t.Errorf("Session prompt Project %q acceptance = %v", value, err)
		}
	}
}

func TestQuestionAttentionOccurrenceMustBeAfterEpoch(t *testing.T) {
	for _, occurredAt := range []time.Time{time.UnixMilli(0), time.UnixMilli(-1), time.UnixMilli(1)} {
		message := &taskpb.AttentionItem{
			Id:        "attention-key",
			ProjectId: "project-1", WorkflowId: "22222222-2222-4222-8222-222222222222",
			TaskId: "task-1", TaskShortId: "T-1", TaskTitle: "Task", OccurredAt: timestamppb.New(occurredAt),
			Detail: &taskpb.AttentionItem_Question{Question: &taskpb.QuestionAttention{
				CurrentNode: &taskpb.AttentionCurrentNode{NodeId: "node-1"},
				Question: &promptpb.ObservationQuestion{Question: &promptpb.ObservationQuestion_Ask{Ask: &promptpb.Question{
					SessionId: "11111111-1111-4111-8111-111111111111", StepId: "22222222-2222-4222-8222-222222222222",
					ToolCallId: "ask-1", Question: "Proceed?", CreatedAt: timestamppb.New(time.UnixMilli(1)),
				}}},
			}},
		}
		if err := protoapi.Validate(message); (err == nil) != (occurredAt.UnixMilli() > 0) {
			t.Errorf("Question occurrence %v acceptance = %v", occurredAt, err)
		}
		message.Detail = &taskpb.AttentionItem_InterruptedCurrentNode{InterruptedCurrentNode: &taskpb.InterruptedAttention{
			CurrentNode: &taskpb.AttentionCurrentNode{NodeId: "node-1"},
		}}
		if err := protoapi.Validate(message); err != nil {
			t.Errorf("durable interruption occurrence %v rejected: %v", occurredAt, err)
		}
		message.Detail = &taskpb.AttentionItem_Approval{Approval: &taskpb.ApprovalAttention{
			ApprovalId: "approval-1",
			ApprovalSnapshot: &taskpb.ApprovalSnapshot{
				SourceNodeDisplayName: "Implement",
			},
		}}
		if err := protoapi.Validate(message); err != nil {
			t.Errorf("durable Approval occurrence %v rejected: %v", occurredAt, err)
		}
		message.Detail = nil
		if err := protoapi.Validate(message); err == nil {
			t.Error("attention without detail accepted")
		}
		message.Detail = &taskpb.AttentionItem_InterruptedCurrentNode{InterruptedCurrentNode: &taskpb.InterruptedAttention{
			CurrentNode: &taskpb.AttentionCurrentNode{NodeId: "node-1"},
		}}
		message.OccurredAt = nil
		if err := protoapi.Validate(message); err == nil {
			t.Error("attention without occurrence accepted")
		}
	}
}
