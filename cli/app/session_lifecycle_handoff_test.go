package app

import (
	"context"
	"errors"
	"testing"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
)

func TestSessionLaunchInitialStateReturnsLifecycleError(t *testing.T) {
	lookupErr := errors.New("initial input lookup failed")
	server := narrowSessionLifecycleServer{
		lifecycle: &recordingSessionLifecycleClient{
			getInitialInput: func(context.Context, *sessionlaunchpb.SessionInitialInputRequest) (*sessionlaunchpb.SessionInitialInputSuccess, error) {
				return nil, lookupErr
			},
		},
	}

	state, err := sessionLaunchInitialStateFromServer(
		context.Background(),
		server,
		"parent-session",
		"child final",
		true,
	)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("initial state error = %v, want %v", err, lookupErr)
	}
	if state.Input != "" {
		t.Fatalf("failed initial state lookup returned state %+v", state)
	}
}
