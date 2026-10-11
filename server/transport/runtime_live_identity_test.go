package transport

import (
	"errors"
	"testing"

	"core/shared/client"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestLiveControlErrorsRetainNumericTargetIdentityOverGateway(t *testing.T) {
	app, server := newGatewayTestServer(t)
	defer app.Close()
	defer server.Close()
	sess := createGatewayAuthoritativeSession(t, app)
	identity := sess.Identity()
	activateGatewayController(t, app, identity.ID.String())
	remote, err := client.DialRemoteURL(t.Context(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	for _, selector := range []runtimeids.SessionSelector{identity.ID, *identity.ShortID} {
		target := protoapi.SessionSelectorToProto(selector)
		caller := identity.ID.String()
		_, selfErr := remote.LiveWait(t.Context(), &runtimepb.LiveWaitRequest{SessionId: target, CallerSessionId: &caller})
		var self *serverapi.SessionSelfTargetError
		var selfTarget *serverapi.SessionTargetError
		if !errors.As(selfErr, &self) || !errors.As(selfErr, &selfTarget) || selfTarget.Projection() == nil || selfTarget.Projection().String() != identity.String() {
			t.Fatalf("self failure lost target or reason: %v", selfErr)
		}
		for _, operation := range []func() error{
			func() error {
				_, err := remote.LiveWait(t.Context(), &runtimepb.LiveWaitRequest{SessionId: target})
				return err
			},
			func() error {
				_, err := remote.LiveSteer(t.Context(), &runtimepb.LiveSteerRequest{SessionId: target, Text: "continue"})
				return err
			},
			func() error {
				_, err := remote.LiveWatch(t.Context(), &promptpb.LiveWatchRequest{SessionId: target})
				return err
			},
		} {
			var failure *serverapi.SessionTargetError
			err := operation()
			if !errors.Is(err, serverapi.ErrRuntimeNoActiveRun) || !errors.As(err, &failure) ||
				failure.Projection() == nil || failure.Projection().ID != identity.ID || failure.Projection().ShortID == nil || *failure.Projection().ShortID != *identity.ShortID {
				t.Fatalf("live target %s failure = %v, identity = %v", selector, err, failure)
			}
		}
	}
}
