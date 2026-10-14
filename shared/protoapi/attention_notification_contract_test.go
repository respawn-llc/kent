package protoapi_test

import (
	"testing"
	"time"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

func TestAttentionNotificationRejectsGoZeroOccurrenceTime(t *testing.T) {
	for _, occurredAt := range []time.Time{{}, time.Unix(0, 0), time.Unix(-1, 0)} {
		id := &taskpb.AttentionNotificationID{
			Kind: taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_WORKFLOW_APPROVAL,
			Uuid: "approval-key",
		}
		messages := []proto.Message{
			&taskpb.ResolvedAttentionNotification{Id: id, Kind: id.Kind, OccurredAt: timestamppb.New(occurredAt)},
			&taskpb.AttentionNotification{
				Id: id, Kind: id.Kind, OccurredAt: timestamppb.New(occurredAt), Revision: 1,
				State: &taskpb.AttentionNotification_WorkflowApproval{WorkflowApproval: &taskpb.WorkflowApprovalState{ApprovalId: "approval-key"}},
				Target: &taskpb.AttentionNotificationTarget{
					Kind: taskpb.AttentionNotificationTargetKind_ATTENTION_NOTIFICATION_TARGET_WORKFLOW_TASK,
					Target: &taskpb.AttentionNotificationTarget_WorkflowTask{WorkflowTask: &taskpb.WorkflowTaskAttentionTarget{
						TaskId: "task-1", WorkflowId: "22222222-2222-4222-8222-222222222222",
						Focus: &taskpb.AttentionNotificationTaskFocus{
							Kind:  taskpb.AttentionNotificationFocusKind_ATTENTION_NOTIFICATION_FOCUS_APPROVAL,
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
