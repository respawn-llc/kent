package chatfixture

import (
	"context"
	"testing"
	"time"

	"core/internal/testharness/requestfixture"
	"core/shared/apicontract"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
)

// OpenRuntime completes a real Chat shell action without dispatching a model Step.
func OpenRuntime(t testing.TB, chat apicontract.ChatMutationService, transcripts apicontract.SessionTranscriptService, sessionID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	selector := requestfixture.Selector(t, sessionID)
	result, err := chat.Steer(ctx, &chatpb.SteerRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(),
		Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{Session: &chatpb.ExistingSessionRequestTarget{
			SessionId: selector,
		}}},
		Activation: &chatpb.Activation{Input: &chatpb.Activation_Shell{Shell: &chatpb.ShellCommand{Command: ":"}}},
	})
	if err != nil || result.GetAccepted() == nil {
		t.Fatalf("Chat shell admission: %v, %v", result, err)
	}
	subscription, err := transcripts.SubscribeSessionTranscript(ctx, &transcriptpb.SubscribeRequest{SessionId: selector})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	// Admission precedes execution. Its completed result, including one already
	// in hydration, is the barrier before checking that the Runtime is idle.
	completed := false
	for {
		message, err := subscription.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		event := message.GetEvent()
		rows := event.GetHydration().GetTailSegment().GetEntries()
		if row := event.GetCommittedRow(); row != nil {
			rows = append(rows, row)
		}
		for _, row := range rows {
			if row.GetNotice().GetMessageType() == transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_USER_SHELL_COMMAND {
				completed = true
			}
		}
		update := event.GetRuntimeReadModelUpdate()
		if hydration := event.GetHydration(); hydration != nil {
			update = hydration.GetRuntimeReadModelUpdate()
		}
		if completed && update.GetActivity().GetState() == runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE {
			return
		}
	}
}
