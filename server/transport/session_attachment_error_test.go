package transport

import (
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"core/shared/client"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

func TestRuntimeLifetimeFailureUsesCapturedOutcome(t *testing.T) {
	id := runtimeids.NewSessionID()
	shortID, err := runtimeids.NewSessionShortID(73)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []serverapi.SessionTarget{
		serverapi.UnresolvedSessionTarget{},
		serverapi.CanonicalSessionTarget{ID: id},
		serverapi.ProjectedSessionTarget{Identity: runtimeids.SessionIdentity{ID: id, ShortID: &shortID}},
	} {
		failure := serverapi.WithSessionTarget(target, serverapi.ErrRuntimeUnavailable)
		details, ok := binaryRuntimeLifetimeFailure(failure).(*sessionlaunchpb.SessionRuntimeUnavailableDetails)
		if !ok {
			t.Fatalf("runtime failure changed category: %v", failure)
		}
		if err := protoapi.Validate(details); err != nil {
			t.Fatal(err)
		}
		decoded := protoapi.SessionTargetContextErrorFromProto(details.SessionTarget, serverapi.ErrRuntimeUnavailable)
		var captured *serverapi.SessionTargetError
		if !errors.Is(decoded, serverapi.ErrRuntimeUnavailable) || !errors.As(decoded, &captured) || !reflect.DeepEqual(captured.Target, target) {
			t.Fatalf("runtime failure changed captured outcome: %v", decoded)
		}
	}
}

func TestSessionAttachmentReportsMissingSessionThroughRPC(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	gateway, err := NewGateway(fixture.appCore, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	remote, err := client.DialRemoteURLForSession(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http")+"/rpc", "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	if remote != nil {
		_ = remote.Close()
	}
	if !errors.Is(err, sessioncontract.ErrSessionNotFound) {
		t.Fatalf("missing session attachment returned %v", err)
	}
}

func TestSessionAttachmentKeepsInternalFailuresDistinctFromMissingSession(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	gateway, err := NewGateway(fixture.appCore, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	if err := fixture.appCore.MetadataStore().Close(); err != nil {
		t.Fatal(err)
	}
	remote, err := client.DialRemoteURLForSession(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http")+"/rpc", fixture.ownSessionID)
	if remote != nil {
		_ = remote.Close()
	}
	if err == nil || errors.Is(err, sessioncontract.ErrSessionNotFound) {
		t.Fatalf("database failure mislabeled as missing Session: %v", err)
	}
}
