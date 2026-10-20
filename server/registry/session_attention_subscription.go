package registry

import (
	"context"
	"fmt"

	"core/server/attentionnotify"
	"core/shared/clientui"
	"core/shared/protoapi"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sessionAttentionSubscription struct {
	native   *attentionnotify.Subscription
	registry *RuntimeRegistry
}

func (s *sessionAttentionSubscription) Close() error {
	return s.native.Close()
}

func (s *sessionAttentionSubscription) Next(ctx context.Context) (*attentionpb.NotificationEvent, error) {
	event, err := s.native.Next(ctx)
	if err != nil {
		return nil, err
	}
	result := &attentionpb.NotificationEvent{Sequence: event.Sequence}
	switch event.Type {
	case clientui.AttentionNotificationEventSessionNamingFailure:
		result.Payload = &attentionpb.NotificationEvent_SessionNamingFailure{SessionNamingFailure: event.SessionNamingFailure}
	case clientui.AttentionNotificationEventResolved:
		id, err := sessionAttentionID(*event.ID)
		if err != nil {
			return nil, err
		}
		result.Payload = &attentionpb.NotificationEvent_Resolved{Resolved: &attentionpb.Resolved{
			Id: id, OccurredAt: timestamppb.New(*event.OccurredAt),
		}}
	case clientui.AttentionNotificationEventPending:
		pending := event.Pending
		id, err := sessionAttentionID(pending.ID)
		if err != nil {
			return nil, err
		}
		notification := &attentionpb.Notification{
			Id: id, OccurredAt: timestamppb.New(pending.OccurredAt),
			Revision: pending.Revision, SessionId: pending.Target.SessionID,
		}
		notification.ShortId, err = s.registry.attentionSessionShortID(ctx, pending.Target.SessionID)
		if err != nil {
			return nil, err
		}
		switch pending.Kind {
		case clientui.AttentionNotificationKindQuestion:
			question, err := attentionQuestionState(pending.Question)
			if err != nil {
				return nil, err
			}
			notification.State = &attentionpb.Notification_Question{Question: question}
		case clientui.AttentionNotificationKindApproval:
			notification.State = &attentionpb.Notification_Approval{Approval: attentionApprovalState(pending.Approval)}
		}
		result.Payload = &attentionpb.NotificationEvent_Pending{Pending: notification}
	}
	return result, nil
}

func attentionQuestionState(question *clientui.AttentionNotificationQuestionState) (*attentionpb.QuestionState, error) {
	displayCount, err := protoapi.Int32(question.DisplayCount, "display count")
	if err != nil {
		return nil, err
	}
	materializedCount, err := protoapi.Int32(question.MaterializedCount, "materialized count")
	if err != nil {
		return nil, err
	}
	return &attentionpb.QuestionState{
		PreparedAskIds: question.PreparedAskIDs, MaterializedAskIds: question.MaterializedAskIDs,
		CurrentUnresolvedAskIds: question.CurrentUnresolvedAskIDs, SkippedAskIds: question.SkippedAskIDs,
		Preview: question.Preview, DisplayCount: displayCount, MaterializedCount: materializedCount,
	}, nil
}

func attentionApprovalState(approval *clientui.AttentionNotificationApprovalState) *attentionpb.ApprovalState {
	return &attentionpb.ApprovalState{
		Message: approval.Message, AccessTargets: protoapi.FileAccessTargetsToProto(approval.AccessTargets),
	}
}

func sessionAttentionID(id clientui.AttentionNotificationID) (*attentionpb.NotificationID, error) {
	result := &attentionpb.NotificationID{Uuid: id.UUID}
	switch id.Kind {
	case clientui.AttentionNotificationKindQuestion:
		result.Kind = attentionpb.Kind_ATTENTION_KIND_QUESTION
	case clientui.AttentionNotificationKindApproval:
		result.Kind = attentionpb.Kind_ATTENTION_KIND_APPROVAL
	default:
		return nil, fmt.Errorf("invalid Session attention identity kind %q", id.Kind)
	}
	return result, nil
}
