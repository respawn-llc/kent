package transport

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"core/shared/client"
	"core/shared/sessioncontract"
)

func TestSessionAttachmentReportsMissingSessionThroughRPC(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	gateway, err := NewGateway(fixture.appCore, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	remote, err := client.DialRemoteURLForSession(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http")+"/rpc", "missing-session")
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
