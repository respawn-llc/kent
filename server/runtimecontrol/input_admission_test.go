package runtimecontrol

import (
	"context"
	"testing"
	"time"

	"core/server/agentruntime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
)

func TestChatAdmissionReturnsDispositionWithoutWaitingForModel(t *testing.T) {
	for _, explicitQueue := range []bool{false, true} {
		t.Run(map[bool]string{false: "steer", true: "queue"}[explicitQueue], func(t *testing.T) {
			client := newCancelObservingRuntimeControlClient()
			defer close(client.release)
			store, _, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
			admit := service.AdmitChatUserTurn
			if explicitQueue {
				admit = service.AdmitChatQueuedUserInput
			}
			prepared, err := service.PrepareUserTurn(t.Context(), store.Meta().SessionID, runtimeinput.Text("start"))
			if err != nil {
				t.Fatal(err)
			}
			prepared.QueueItemID = runtimeids.NewQueueItemID().String()
			result, err := admit(t.Context(), store.Meta().SessionID, prepared)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Accepted || result.Disposition != runtimeinput.InputAdmissionStartedTurn || result.QueueItemID.String() != prepared.QueueItemID {
				t.Fatalf("idle admission = %+v", result)
			}
			select {
			case <-client.started:
			case <-time.After(10 * time.Second):
				t.Fatal("accepted turn did not start")
			}
			for _, input := range []struct {
				admit func(context.Context, string, PreparedUserTurn) (serverapi.ChatInputAdmissionResult, error)
				want  runtimeinput.InputAdmissionDisposition
			}{
				{service.AdmitChatUserTurn, runtimeinput.InputAdmissionPendingSteer},
				{service.AdmitChatQueuedUserInput, runtimeinput.InputAdmissionPendingQueue},
			} {
				prepared.QueueItemID = runtimeids.NewQueueItemID().String()
				result, err := input.admit(t.Context(), store.Meta().SessionID, prepared)
				if err != nil {
					t.Fatal(err)
				}
				if !result.Accepted || result.Disposition != input.want || result.QueueItemID.String() != prepared.QueueItemID {
					t.Fatalf("busy admission = %+v, want %v", result, input.want)
				}
			}
		})
	}
}
