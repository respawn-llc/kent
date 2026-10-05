package registry

import (
	"context"
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"
)

func subscribeTranscriptForTest(t *testing.T, registry *RuntimeRegistry, sessionID string) serverapi.TranscriptSubscription {
	t.Helper()
	subscription, err := registry.SubscribeSessionTranscript(context.Background(), &transcriptpb.SubscribeRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("SubscribeSessionTranscript: %v", err)
	}
	return subscription
}

func nextTranscriptMessageOfKind[T any](t *testing.T, subscription serverapi.TranscriptSubscription) *transcriptpb.Message {
	t.Helper()
	for range 8 {
		message := nextTranscriptMessage(t, subscription)
		if _, ok := message.Event.Payload.(T); ok {
			return message
		}
	}
	t.Fatalf("did not receive transcript payload type %T", *new(T))
	return nil
}
