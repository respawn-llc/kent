package registry

import (
	"context"

	"core/server/attentionnotify"
	"core/server/workflowview"
	"core/shared/clientui"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/textutil"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type attentionSubscription struct {
	native   *attentionnotify.Subscription
	registry *RuntimeRegistry
}

func (s *attentionSubscription) Close() error { return s.native.Close() }

func (s *attentionSubscription) Next(ctx context.Context) (*taskpb.AttentionNotificationEvent, error) {
	event, err := s.native.Next(ctx)
	if err != nil {
		return nil, err
	}
	result := &taskpb.AttentionNotificationEvent{Sequence: event.Sequence}
	switch event.Type {
	case clientui.AttentionNotificationEventSessionTurnEnd:
		result.Type = taskpb.AttentionNotificationEventType_ATTENTION_NOTIFICATION_EVENT_SESSION_TURN_END
		result.Payload = &taskpb.AttentionNotificationEvent_SessionTurnEnd{SessionTurnEnd: event.SessionTurnEnd}
	case clientui.AttentionNotificationEventSessionNamingFailure:
		result.Type = taskpb.AttentionNotificationEventType_ATTENTION_NOTIFICATION_EVENT_SESSION_NAMING_FAILURE
		result.Payload = &taskpb.AttentionNotificationEvent_SessionNamingFailure{SessionNamingFailure: event.SessionNamingFailure}
	case clientui.AttentionNotificationEventResolved:
		id := attentionNotificationID(*event.ID)
		result.Type = taskpb.AttentionNotificationEventType_ATTENTION_NOTIFICATION_EVENT_RESOLVED
		result.Payload = &taskpb.AttentionNotificationEvent_Resolved{Resolved: &taskpb.ResolvedAttentionNotification{
			Id: id, Kind: id.Kind, OccurredAt: timestamppb.New(*event.OccurredAt),
		}}
	case clientui.AttentionNotificationEventPending:
		notification, err := attentionNotification(event.Pending)
		if err != nil {
			return nil, err
		}
		if event.Pending.Target.SessionID != "" {
			shortID, err := s.registry.attentionSessionShortID(ctx, event.Pending.Target.SessionID)
			if err != nil {
				return nil, err
			}
			switch target := notification.Target.Target.(type) {
			case *taskpb.AttentionNotificationTarget_SessionPrompt:
				target.SessionPrompt.ShortId = shortID
			case *taskpb.AttentionNotificationTarget_WorkflowTask:
				target.WorkflowTask.SessionShortId = shortID
			}
		}
		result.Type = taskpb.AttentionNotificationEventType_ATTENTION_NOTIFICATION_EVENT_PENDING
		result.Payload = &taskpb.AttentionNotificationEvent_Pending{Pending: notification}
	}
	return result, nil
}

func attentionNotification(pending *clientui.AttentionNotification) (*taskpb.AttentionNotification, error) {
	id := attentionNotificationID(pending.ID)
	target := attentionNotificationTarget(pending.Target)
	result := &taskpb.AttentionNotification{
		Id: id, Kind: id.Kind, OccurredAt: timestamppb.New(pending.OccurredAt), Revision: pending.Revision, Target: target,
	}
	switch pending.Kind {
	case clientui.AttentionNotificationKindQuestion:
		question, err := attentionQuestionState(pending.Question)
		if err != nil {
			return nil, err
		}
		result.State = &taskpb.AttentionNotification_Question{Question: question}
	case clientui.AttentionNotificationKindApproval:
		result.State = &taskpb.AttentionNotification_Approval{Approval: attentionApprovalState(pending.Approval)}
	case clientui.AttentionNotificationKindWorkflowApproval:
		result.State = &taskpb.AttentionNotification_WorkflowApproval{WorkflowApproval: &taskpb.WorkflowApprovalState{
			ApprovalId: pending.WorkflowApproval.ApprovalID, Message: textutil.OptionalExactString(pending.WorkflowApproval.Message),
		}}
	case clientui.AttentionNotificationKindInterruptedCurrentNode:
		state := pending.InterruptedCurrentNode
		detail := &taskpb.InterruptedCurrentNodeState{
			Message: textutil.OptionalExactString(state.Message), Reason: textutil.OptionalExactString(state.Reason),
		}
		if state.DetailJSON != "" {
			var err error
			detail.Details, err = workflowview.InterruptionDetails(state.DetailJSON)
			if err != nil {
				return nil, err
			}
		}
		result.State = &taskpb.AttentionNotification_InterruptedCurrentNode{InterruptedCurrentNode: detail}
	}
	return result, nil
}

func attentionNotificationID(id clientui.AttentionNotificationID) *taskpb.AttentionNotificationID {
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
	}
	return result
}

func attentionNotificationTarget(target clientui.AttentionNotificationTarget) *taskpb.AttentionNotificationTarget {
	if target.Kind == clientui.AttentionNotificationTargetSessionPrompt {
		return &taskpb.AttentionNotificationTarget{
			Kind: taskpb.AttentionNotificationTargetKind_ATTENTION_NOTIFICATION_TARGET_SESSION_PROMPT,
			Target: &taskpb.AttentionNotificationTarget_SessionPrompt{SessionPrompt: &taskpb.SessionPromptAttentionTarget{
				ProjectId: target.ProjectID, SessionId: target.SessionID,
			}},
		}
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
	}
	return &taskpb.AttentionNotificationTarget{
		Kind: taskpb.AttentionNotificationTargetKind_ATTENTION_NOTIFICATION_TARGET_WORKFLOW_TASK,
		Target: &taskpb.AttentionNotificationTarget_WorkflowTask{WorkflowTask: &taskpb.WorkflowTaskAttentionTarget{
			ProjectId: textutil.OptionalExactString(target.ProjectID), WorkflowId: target.WorkflowID.String(),
			TaskId: target.TaskID, TaskShortId: textutil.OptionalExactString(target.TaskShortID),
			TaskTitle: textutil.OptionalExactString(target.TaskTitle), SessionId: textutil.OptionalExactString(target.SessionID),
			CurrentNodeId: target.CurrentNodeID, CurrentNodeBranchKey: target.CurrentNodeBranchKey, Focus: focus,
		}},
	}
}
