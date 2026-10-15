package runtimecontrol

import (
	"context"
	"errors"
	"testing"

	"core/server/agentruntime"
	"core/server/sessionruntime"
	"core/server/sessionstore"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
)

func TestChatInputAdmissionDoesNotEnterRetiringAgent(t *testing.T) {
	store, _, service := newRuntimeControlTestService(t, finalResponseRuntimeControlClient(), nil, runtime.Config{})
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	committing, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	t.Cleanup(func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	go func() {
		done <- service.authority.WithSessionAgentChange(t.Context(), id,
			func(context.Context, *session.Store, *runtime.Engine) (bool, error) { return true, nil },
			func(context.Context, *session.Store, *runtime.Engine) (bool, error) {
				close(committing)
				<-release
				return true, nil
			})
	}()
	select {
	case <-committing:
	}
	prepared, err := service.PrepareUserTurn(t.Context(), id.String(), runtimeinput.Text("concurrent input"))
	if err != nil {
		t.Fatal(err)
	}
	prepared.QueueItemID = runtimeids.NewQueueItemID().String()
	for _, admission := range []struct {
		name string
		run  func(*Service, context.Context, string, PreparedUserTurn) (serverapi.ChatInputAdmissionResult, error)
	}{
		{"Queue", (*Service).AdmitChatQueuedUserInput},
		{"Steer", (*Service).AdmitChatUserTurn},
	} {
		result, err := admission.run(service, t.Context(), id.String(), prepared)
		if !errors.Is(err, sessionruntime.ErrSessionStartsBlocked) || result.Accepted {
			t.Errorf("%s entered the retiring Agent: result=%+v error=%v", admission.name, result, err)
		}
	}
}
