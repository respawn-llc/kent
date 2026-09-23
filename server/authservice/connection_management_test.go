package authservice

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"core/internal/testharness/httpclient"
	"core/server/auth"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/textutil"

	"google.golang.org/protobuf/types/known/emptypb"
)

func TestConnectionReferenceSavingDoesNotCheckCredentials(t *testing.T) {
	resolver := authServiceResolver(t, auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil), func(string) (string, bool) {
		t.Error("saving a variable reference looked up a credential")
		return "", false
	})
	service := NewBootstrapService(context.Background(), resolver, auth.OpenAIOAuthOptions{})
	_, err := service.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_Add{
		Add: &authpb.ConnectionDefinition{
			Id: "new-api", Protocol: authpb.ConnectionProtocol_CONNECTION_PROTOCOL_RESPONSES,
			Endpoint: textutil.Value("http://localhost:1234/v1"), EnvironmentVariable: textutil.Value("MISSING_KEY"),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_Reference{
		Reference: &authpb.ConnectionReferenceEdit{ConnectionId: "new-api", EnvironmentVariable: "OTHER_MISSING_KEY"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: resolver.root})
	if err != nil {
		t.Fatal(err)
	}
	if *app.Settings.Connections["new-api"].EnvironmentVariable != "OTHER_MISSING_KEY" || *app.Settings.Connection != "work" {
		t.Fatal("reference edit changed the default or failed to save")
	}
	id := config.ConnectionID("new-api")
	app.Settings.Connection = &id
	dispatchResolver := NewConnectionResolver(filepath.Dir(app.Source.File(config.FileGlobal).Path), resolver.manager, func(string) (string, bool) { return "", false })
	connection, err := dispatchResolver.Resolve(app.Settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Auth.ResolveDispatchAuth(t.Context()); err == nil {
		t.Fatal("missing credentials must fail when the saved connection is used")
	}
}

func TestPendingOAuthSurvivesObserverLossButNotExplicitDiscard(t *testing.T) {
	for _, discard := range []bool{false, true} {
		t.Run(fmt.Sprint(discard), func(t *testing.T) {
			accepted, release := make(chan struct{}), make(chan struct{})
			oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(accepted)
				<-release
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"access_token":"pending","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`)
			}))
			defer oauth.Close()
			endpoint, err := url.Parse(oauth.URL)
			if err != nil {
				t.Fatal(err)
			}
			manager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)
			service := NewBootstrapService(t.Context(), NewConnectionResolver(t.TempDir(), manager, nil), auth.OpenAIOAuthOptions{
				HTTPClient: &http.Client{Transport: httpclient.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
					request := r.Clone(r.Context())
					request.URL.Scheme, request.URL.Host = endpoint.Scheme, endpoint.Host
					return oauth.Client().Transport.RoundTrip(request)
				})},
			})
			if _, err := service.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_PendingSetup{
				PendingSetup: &authpb.ConnectionDefinition{Id: "subscription", Protocol: authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT},
			}}); err != nil {
				t.Fatal(err)
			}
			target := &authpb.ConnectionTarget{Target: &authpb.ConnectionTarget_PendingSetup{PendingSetup: &emptypb.Empty{}}}
			observer, disconnect := context.WithCancel(t.Context())
			result := make(chan error, 1)
			go func() {
				_, err := service.CompleteBootstrap(observer, &authpb.CompleteBootstrapRequest{
					Target: target, Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
					DeviceAuthorizationCode: textutil.Value("grant"), DeviceCodeVerifier: textutil.Value("verifier"),
				})
				result <- err
			}()
			<-accepted
			disconnect()
			if discard {
				if _, err := service.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_DiscardSetup{DiscardSetup: &emptypb.Empty{}}}); err != nil {
					t.Error(err)
				}
			}
			close(release)
			err = <-result
			if discard && err == nil || !discard && err != nil {
				t.Fatalf("completion after observer loss, discard=%v: %v", discard, err)
			}
			state, err := manager.Load(t.Context())
			if err != nil || len(state.Connections) != 0 {
				t.Fatalf("first-run sign-in was persisted before Finish: %+v %v", state, err)
			}
			status, err := service.GetBootstrapStatus(t.Context(), &authpb.GetBootstrapStatusRequest{Target: target})
			if discard {
				if err == nil {
					t.Fatal("late sign-in resurrected discarded setup")
				}
			} else if err != nil || !status.AuthReady {
				t.Fatalf("accepted sign-in lost after disconnect: %+v %v", status, err)
			}
		})
	}
}
