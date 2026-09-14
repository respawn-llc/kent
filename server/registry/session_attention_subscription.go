package registry

import (
	"context"
	"fmt"

	"core/shared/clientui"
	"core/shared/protoapi"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sessionAttentionSubscription struct {
	native serverapi.AttentionNotificationSubscription
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
	case clientui.AttentionNotificationEventResolved:
		if event.ID == nil || event.OccurredAt == nil {
			return nil, fmt.Errorf("resolved attention identity and time are required")
		}
		id, err := sessionAttentionID(*event.ID)
		if err != nil {
			return nil, err
		}
		result.Payload = &attentionpb.NotificationEvent_Resolved{Resolved: &attentionpb.Resolved{
			Id: id, OccurredAt: timestamppb.New(*event.OccurredAt),
		}}
	case clientui.AttentionNotificationEventPending:
		if event.Pending == nil {
			return nil, fmt.Errorf("pending attention notification is required")
		}
		pending := event.Pending
		id, err := sessionAttentionID(pending.ID)
		if err != nil {
			return nil, err
		}
		notification := &attentionpb.Notification{
			Id: id, OccurredAt: timestamppb.New(pending.OccurredAt),
			Revision: pending.Revision, SessionId: pending.Target.SessionID,
		}
		switch pending.Kind {
		case clientui.AttentionNotificationKindQuestion:
			if pending.Question == nil {
				return nil, fmt.Errorf("attention Question state is required")
			}
			question := pending.Question
			displayCount, err := protoapi.Int32(question.DisplayCount, "display count")
			if err != nil {
				return nil, err
			}
			materializedCount, err := protoapi.Int32(question.MaterializedCount, "materialized count")
			if err != nil {
				return nil, err
			}
			notification.State = &attentionpb.Notification_Question{Question: &attentionpb.QuestionState{
				PreparedAskIds: question.PreparedAskIDs, MaterializedAskIds: question.MaterializedAskIDs,
				CurrentUnresolvedAskIds: question.CurrentUnresolvedAskIDs, SkippedAskIds: question.SkippedAskIDs,
				Preview: question.Preview, DisplayCount: displayCount, MaterializedCount: materializedCount,
			}}
		case clientui.AttentionNotificationKindApproval:
			if pending.Approval == nil {
				return nil, fmt.Errorf("attention Approval state is required")
			}
			notification.State = &attentionpb.Notification_Approval{Approval: &attentionpb.ApprovalState{
				Message: pending.Approval.Message, AccessTargets: protoapi.FileAccessTargetsToProto(pending.Approval.AccessTargets),
			}}
		default:
			return nil, fmt.Errorf("invalid Session attention kind %q", pending.Kind)
		}
		result.Payload = &attentionpb.NotificationEvent_Pending{Pending: notification}
	default:
		return nil, fmt.Errorf("invalid attention event type %q", event.Type)
	}
	return result, protoapi.Validate(result)
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
