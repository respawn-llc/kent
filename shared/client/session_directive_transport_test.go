package client

import (
	"testing"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
)

func TestSessionLifecycleResultRemoteRoundTrip(t *testing.T) {
	want := &sessionlaunchpb.SessionDirective{Directive: &sessionlaunchpb.SessionDirective_SelectSession{
		SelectSession: &sessionlaunchpb.SessionSelectDirective{Auth: sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE},
	}}
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		request := &sessionlaunchpb.SessionResolveTransitionRequest{}
		call := receiveRemoteGeneratedCall(t, ws, "SessionLifecycleService", "ResolveTransition", request)
		if request.Transition.Action != sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_LOGOUT {
			t.Errorf("transition = %v, want logout", request.Transition)
		}
		sendRemoteGeneratedResult(t, ws, call, &sessionlaunchpb.SessionResolveTransitionResult{
			Outcome: &sessionlaunchpb.SessionResolveTransitionResult_Success{Success: want},
		})
	})
	remote, err := DialRemoteURL(t.Context(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remote.Close() }()
	got, err := remote.ResolveTransition(t.Context(), &sessionlaunchpb.SessionResolveTransitionRequest{
		Transition: &sessionlaunchpb.SessionTransition{Action: sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_LOGOUT},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(got, want) {
		t.Fatalf("result = %v, want %v", got, want)
	}
}
