package app

import (
	"context"
	"errors"
	"testing"

	"core/cli/app/internal/status"
	"core/shared/apicontract"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

type pickerConnectionCatalogClient struct {
	apicontract.ConnectionManagementService
	catalog *authpb.ConnectionCatalog
	err     error
}

func (c pickerConnectionCatalogClient) GetConnections(context.Context, *authpb.GetConnectionsRequest) (*authpb.ConnectionCatalog, error) {
	return c.catalog, c.err
}

func TestSessionPickerProviderStatusUsesExactSingleConnectionWithoutUsage(t *testing.T) {
	for _, method := range []authpb.AuthMethod{
		authpb.AuthMethod_AUTH_METHOD_NONE,
		authpb.AuthMethod_AUTH_METHOD_API_KEY,
		authpb.AuthMethod_AUTH_METHOD_OAUTH,
	} {
		t.Run(method.String(), func(t *testing.T) {
			auth := &staticAuthStatusClient{response: authStatusResponse(method)}
			connections := pickerConnectionCatalogClient{catalog: &authpb.ConnectionCatalog{
				Connections: []*authpb.ConnectionDefinition{{Id: "sole-provider"}},
			}}
			info, err := loadSessionPickerProviderInfo(t.Context(), connections, auth)
			if err != nil {
				t.Fatal(err)
			}
			single, ok := info.(sessionPickerSingleConnection)
			if !ok || single.auth.Method != method {
				t.Fatalf("single provider status = %+v", info)
			}
			if auth.request.GetProvider().GetConnectionId() != "sole-provider" || !auth.request.SkipSubscriptionUsage {
				t.Fatalf("provider display did not use a metadata-only exact selection: %+v", auth.request)
			}
			if got := sessionPickerProviderSummary(info); got != status.AuthDisplayLabel(status.AuthStageFromResponse(auth.response).Auth) {
				t.Fatal("provider display did not reuse the existing auth presentation")
			}
		})
	}
}

func TestSessionPickerMultipleConnectionsSkipAuthRead(t *testing.T) {
	auth := &staticAuthStatusClient{err: errors.New("must not request one provider for a multiple-connection display")}
	connections := pickerConnectionCatalogClient{catalog: &authpb.ConnectionCatalog{
		Connections: []*authpb.ConnectionDefinition{{Id: "one"}, {Id: "two"}},
	}}
	info, err := loadSessionPickerProviderInfo(t.Context(), connections, auth)
	count, ok := info.(sessionPickerConnectionCount)
	if err != nil || !ok || count != 2 || auth.calls != 0 {
		t.Fatalf("multiple-provider status = %+v, err=%v, auth calls=%d", info, err, auth.calls)
	}
}

func TestSessionPickerProviderReadFailuresRemainRetryable(t *testing.T) {
	failure := errors.New("connection metadata unavailable")
	for _, connections := range []pickerConnectionCatalogClient{
		{err: failure},
		{catalog: &authpb.ConnectionCatalog{Connections: []*authpb.ConnectionDefinition{{Id: "one"}}}},
	} {
		_, err := loadSessionPickerProviderInfo(t.Context(), connections, &staticAuthStatusClient{err: failure})
		if !errors.Is(err, failure) {
			t.Fatalf("provider read lost its failure: %v", err)
		}
	}
}

func TestSessionPickerTypedUnavailableAuthRemainsDisplayable(t *testing.T) {
	auth := &staticAuthStatusClient{response: &authpb.Status{
		Resolution: &authpb.StatusResolution{
			Resolution: &authpb.StatusResolution_Unavailable{
				Unavailable: &authpb.StatusFailure{Cause: "stored credential is unavailable"},
			},
		},
	}}
	connections := pickerConnectionCatalogClient{catalog: &authpb.ConnectionCatalog{
		Connections: []*authpb.ConnectionDefinition{{Id: "one"}},
	}}
	info, err := loadSessionPickerProviderInfo(t.Context(), connections, auth)
	single, ok := info.(sessionPickerSingleConnection)
	if err != nil || !ok || !single.auth.Unavailable {
		t.Fatalf("typed unavailable status = %+v, err=%v", info, err)
	}
}
