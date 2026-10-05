package registry

import (
	"context"
	"fmt"

	"core/server/attentionnotify"
	"core/server/workflowview"
	"core/shared/clientui"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/textutil"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type attentionSubscription struct {
	native *attentionnotify.Subscription
}

func (s *attentionSubscription) Close() error { return s.native.Close() }

func (s *attentionSubscription) Next(ctx context.Context) (*taskpb.AttentionNotificationEvent, error) {
	event, err := s.native.Next(ctx)
	if err != nil {
		return nil, err
	}
	result := &taskpb.AttentionNotificationEvent{Sequence: event.Sequence}
	switch event.Type {
	case clientui.AttentionNotificationEventResolved:
		if event.ID == nil || event.OccurredAt == nil {
			return nil, fmt.Errorf("resolved attention identity and time are required")
		}
		id, err := attentionNotificationID(*event.ID)
		if err != nil {
			return nil, err
		}
		result.Type = taskpb.AttentionNotificationEventType_ATTENTION_NOTIFICATION_EVENT_RESOLVED
		result.Payload = &taskpb.AttentionNotificationEvent_Resolved{Resolved: &taskpb.ResolvedAttentionNotification{
			Id: id, Kind: id.Kind, OccurredAt: timestamppb.New(*event.OccurredAt),
		}}
	case clientui.AttentionNotificationEventPending:
		if event.Pending == nil {
			return nil, fmt.Errorf("pending attention notification is required")
		}
		notification, err := attentionNotification(event.Pending)
		if err != nil {
			return nil, err
		}
		result.Type = taskpb.AttentionNotificationEventType_ATTENTION_NOTIFICATION_EVENT_PENDING
		result.Payload = &taskpb.AttentionNotificationEvent_Pending{Pending: notification}
	default:
		return nil, fmt.Errorf("invalid attention event type %q", event.Type)
	}
	return result, protoapi.Validate(result)
}

func attentionNotification(pending *clientui.AttentionNotification) (*taskpb.AttentionNotification, error) {
	id, err := attentionNotificationID(pending.ID)
	if err != nil {
		return nil, err
	}
	target, err := attentionNotificationTarget(pending.Target)
	if err != nil {
		return nil, err
	}
	result := &taskpb.AttentionNotification{
		Id: id, Kind: id.Kind, OccurredAt: timestamppb.New(pending.OccurredAt), Revision: pending.Revision, Target: target,
	}
	switch pending.Kind {
	case clientui.AttentionNotificationKindQuestion:
		if pending.Question == nil {
			return nil, fmt.Errorf("attention Question state is required")
		}
		question, err := attentionQuestionState(pending.Question)
		if err != nil {
			return nil, err
		}
		result.State = &taskpb.AttentionNotification_Question{Question: question}
	case clientui.AttentionNotificationKindApproval:
		if pending.Approval == nil {
			return nil, fmt.Errorf("attention Approval state is required")
		}
		result.State = &taskpb.AttentionNotification_Approval{Approval: attentionApprovalState(pending.Approval)}
	case clientui.AttentionNotificationKindWorkflowApproval:
		if pending.WorkflowApproval == nil {
			return nil, fmt.Errorf("Workflow approval state is required")
		}
		result.State = &taskpb.AttentionNotification_WorkflowApproval{WorkflowApproval: &taskpb.WorkflowApprovalState{
			ApprovalId: pending.WorkflowApproval.ApprovalID, Message: textutil.OptionalExactString(pending.WorkflowApproval.Message),
		}}
	case clientui.AttentionNotificationKindInterruptedCurrentNode:
		if pending.InterruptedCurrentNode == nil {
			return nil, fmt.Errorf("interrupted Current Node state is required")
		}
		state := pending.InterruptedCurrentNode
		detail := &taskpb.InterruptedCurrentNodeState{
			Message: textutil.OptionalExactString(state.Message), Reason: textutil.OptionalExactString(state.Reason),
		}
		if state.DetailJSON != "" {
			detail.Details, err = workflowview.InterruptionDetails(state.DetailJSON)
			if err != nil {
				return nil, err
			}
		}
		result.State = &taskpb.AttentionNotification_InterruptedCurrentNode{InterruptedCurrentNode: detail}
	default:
		return nil, fmt.Errorf("invalid attention kind %q", pending.Kind)
	}
	return result, nil
}

func attentionNotificationID(id clientui.AttentionNotificationID) (*taskpb.AttentionNotificationID, error) {
	result := &taskpb.AttentionNotificationID{Uuid: id.UUID}
	switch id.Kind {
	case clientui.AttentionNotificationKindQuestion:
		result.Kind = taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_QUESTION
	case clientui.AttentionNotificationKindApproval:
		result.Kind = taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_APPROVAL
	case clientui.AttentionNotificationKindWorkflowApproval:
		result.Kind = taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_WORKFLOW_APPROVAL
	case clientui.AttentionNotificationKindInterruptedCurrentNode:
		result.Kind = taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_INTERRUPTED_CURRENT_NODE
	default:
		return nil, fmt.Errorf("invalid attention identity kind %q", id.Kind)
	}
	return result, nil
}

func attentionNotificationTarget(target clientui.AttentionNotificationTarget) (*taskpb.AttentionNotificationTarget, error) {
	switch target.Kind {
	case clientui.AttentionNotificationTargetSessionPrompt:
		return &taskpb.AttentionNotificationTarget{
			Kind: taskpb.AttentionNotificationTargetKind_ATTENTION_NOTIFICATION_TARGET_SESSION_PROMPT,
			Target: &taskpb.AttentionNotificationTarget_SessionPrompt{SessionPrompt: &taskpb.SessionPromptAttentionTarget{
				ProjectId: target.ProjectID, SessionId: target.SessionID,
			}},
		}, nil
	case clientui.AttentionNotificationTargetWorkflowTask:
		if target.WorkflowID == nil || target.Focus == nil {
			return nil, fmt.Errorf("Workflow Task attention requires Workflow identity and focus")
		}
		focus := &taskpb.AttentionNotificationTaskFocus{}
		switch target.Focus.Kind {
		case clientui.AttentionNotificationFocusQuestion:
			focus.Kind = taskpb.AttentionNotificationFocusKind_ATTENTION_NOTIFICATION_FOCUS_QUESTION
			focus.Focus = &taskpb.AttentionNotificationTaskFocus_Question{Question: &taskpb.QuestionFocus{AskIds: target.Focus.AskIDs}}
		case clientui.AttentionNotificationFocusApproval:
			focus.Kind = taskpb.AttentionNotificationFocusKind_ATTENTION_NOTIFICATION_FOCUS_APPROVAL
			focus.Focus = &taskpb.AttentionNotificationTaskFocus_Approval{Approval: &taskpb.ApprovalFocus{ApprovalId: target.Focus.ApprovalID}}
		case clientui.AttentionNotificationFocusInterruptedCurrentNode:
			focus.Kind = taskpb.AttentionNotificationFocusKind_ATTENTION_NOTIFICATION_FOCUS_INTERRUPTED_CURRENT_NODE
			focus.Focus = &taskpb.AttentionNotificationTaskFocus_InterruptedCurrentNode{InterruptedCurrentNode: &taskpb.InterruptedCurrentNodeFocus{}}
		default:
			return nil, fmt.Errorf("invalid attention focus kind %q", target.Focus.Kind)
		}
		return &taskpb.AttentionNotificationTarget{
			Kind: taskpb.AttentionNotificationTargetKind_ATTENTION_NOTIFICATION_TARGET_WORKFLOW_TASK,
			Target: &taskpb.AttentionNotificationTarget_WorkflowTask{WorkflowTask: &taskpb.WorkflowTaskAttentionTarget{
				ProjectId: textutil.OptionalExactString(target.ProjectID), WorkflowId: target.WorkflowID.String(),
				TaskId: target.TaskID, TaskShortId: textutil.OptionalExactString(target.TaskShortID),
				TaskTitle: textutil.OptionalExactString(target.TaskTitle), SessionId: textutil.OptionalExactString(target.SessionID),
				CurrentNodeId: target.CurrentNodeID, CurrentNodeBranchKey: target.CurrentNodeBranchKey, Focus: focus,
			}},
		}, nil
	default:
		return nil, fmt.Errorf("invalid attention target kind %q", target.Kind)
	}
}
