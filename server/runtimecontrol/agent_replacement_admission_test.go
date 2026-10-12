package runtimecontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/runtime"
	"core/server/session"
	"core/server/sessionruntime"
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
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("Agent replacement did not finish")
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
	case <-time.After(3 * time.Second):
		t.Fatal("Agent replacement did not reach commit")
	}
	prepared, err := service.PrepareUserTurn(t.Context(), id.String(), runtimeinput.Text("concurrent input"))
	if err != nil {
		t.Fatal(err)
	}
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
