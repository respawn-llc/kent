package sessionservice

import (
	"context"
	"testing"

	"core/shared/clientui"
	"core/shared/serverapi"
)

type recordingCursorSubscriber struct {
	req serverapi.SessionActivitySubscribeRequest
}

func (s *recordingCursorSubscriber) SubscribeSessionActivity(context.Context, string) (serverapi.SessionActivitySubscription, error) {
	return staticSessionActivitySubscription{}, nil
}

func (s *recordingCursorSubscriber) SubscribeSessionActivityFrom(_ context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error) {
	s.req = req
	return staticSessionActivitySubscription{}, nil
}

type recordingLegacySubscriber struct {
	sessionID string
}

func (s *recordingLegacySubscriber) SubscribeSessionActivity(_ context.Context, sessionID string) (serverapi.SessionActivitySubscription, error) {
	s.sessionID = sessionID
	return staticSessionActivitySubscription{}, nil
}

type staticSessionActivitySubscription struct{}

func (staticSessionActivitySubscription) Next(context.Context) (clientui.Event, error) {
	return clientui.Event{}, nil
}

func (staticSessionActivitySubscription) Close() error { return nil }

func TestServiceForwardsCursorSubscriptionRequest(t *testing.T) {
	subscriber := &recordingCursorSubscriber{}
	service := NewSessionActivityService(subscriber)

	if _, err := service.SubscribeSessionActivity(context.Background(), serverapi.SessionActivitySubscribeRequest{SessionID: "session-1", AfterSequence: 42}); err != nil {
		t.Fatalf("SubscribeSessionActivity: %v", err)
	}
	if subscriber.req.SessionID != "session-1" || subscriber.req.AfterSequence != 42 {
		t.Fatalf("forwarded request = %+v, want session-1 after 42", subscriber.req)
	}
}

func TestServiceFallsBackToLegacySessionSubscriber(t *testing.T) {
	subscriber := &recordingLegacySubscriber{}
	service := NewSessionActivityService(subscriber)

	if _, err := service.SubscribeSessionActivity(context.Background(), serverapi.SessionActivitySubscribeRequest{SessionID: "session-1"}); err != nil {
		t.Fatalf("SubscribeSessionActivity: %v", err)
	}
	if subscriber.sessionID != "session-1" {
		t.Fatalf("legacy session id = %q, want session-1", subscriber.sessionID)
	}
}

func TestServiceRejectsLegacySubscriberCursorReplay(t *testing.T) {
	subscriber := &recordingLegacySubscriber{}
	service := NewSessionActivityService(subscriber)

	if _, err := service.SubscribeSessionActivity(context.Background(), serverapi.SessionActivitySubscribeRequest{SessionID: "session-1", AfterSequence: 42}); err != serverapi.ErrStreamGap {
		t.Fatalf("SubscribeSessionActivity error = %v, want %v", err, serverapi.ErrStreamGap)
	}
	if subscriber.sessionID != "" {
		t.Fatalf("legacy subscriber was called with session id %q despite unsupported cursor replay", subscriber.sessionID)
	}
}
