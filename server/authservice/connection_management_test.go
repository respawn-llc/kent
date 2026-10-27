package authservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"core/internal/testharness/httpclient"
	"core/server/authmanager"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/serverapi"
	"core/shared/textutil"

	"google.golang.org/protobuf/types/known/emptypb"
)

func TestConnectionCatalogPreservesConfiguredSetsAndDefaultEdit(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(`
connection = ["a", "unknown", "b", "a"]
[connections.a]
protocol = "responses"
endpoint = "http://localhost:1234/v1"
[connections.b]
protocol = "responses"
endpoint = "http://localhost:1234/v1"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, config.ConfigDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, config.ConfigDirName, "config.toml"), []byte(`connection = ["b", "unknown"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := NewConnectionResolver(root, nil, func(string) (string, bool) {
		t.Fatal("catalog/default editing accessed credentials")
		return "", false
	})
	service := NewBootstrapService(t.Context(), resolver, auth.OpenAIOAuthOptions{})
	for range 2 {
		catalog, err := service.GetConnections(t.Context(), &authpb.GetConnectionsRequest{WorkspaceRoot: &workspace})
		if err != nil {
			t.Fatal(err)
		}
		if catalog.DefaultSelection == nil || !reflect.DeepEqual(catalog.DefaultSelection.Ids, []string{"a", "unknown", "b"}) ||
			catalog.WorkspaceSelection == nil || !reflect.DeepEqual(catalog.WorkspaceSelection.Ids, []string{"b", "unknown"}) {
			t.Fatalf("configured selections = %+v", catalog)
		}
	}
	if _, err := service.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{
		Change: &authpb.ConfigureConnectionRequest_DefaultConnectionId{DefaultConnectionId: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := service.GetConnections(t.Context(), &authpb.GetConnectionsRequest{})
	if err != nil || catalog.DefaultSelection == nil || !reflect.DeepEqual(catalog.DefaultSelection.Ids, []string{"b"}) || catalog.WorkspaceSelection != nil {
		t.Fatalf("default edit = %+v, %v", catalog, err)
	}
}

func TestConfigureConnectionRemovesSelectedDefinitionAndCredentials(t *testing.T) {
	initial := auth.EmptyState()
	initial.Connections["work"] = auth.OAuthMethod{
		AccessToken: "work-token", RefreshToken: "work-refresh",
		AccountID: "work-account", Email: "work@example.invalid",
	}
	initial.Connections["personal"] = auth.OAuthMethod{
		AccessToken: "personal-token", RefreshToken: "personal-refresh",
		AccountID: "personal-account", Email: "personal@example.invalid",
	}
	manager := auth.NewManager(auth.NewMemoryStore(initial), nil)
	resolver := authServiceResolver(t, manager, func(string) (string, bool) {
		t.Fatal("removing a connection read an environment secret")
		return "", false
	})
	service := NewBootstrapService(context.Background(), resolver, auth.OpenAIOAuthOptions{})
	request := &authpb.ConfigureConnectionRequest{
		Change: &authpb.ConfigureConnectionRequest_RemoveConnectionId{RemoveConnectionId: "work"},
	}

	if _, err := service.ConfigureConnection(t.Context(), request); err != nil {
		t.Fatal(err)
	}

	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: resolver.root})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := app.Settings.Connections["work"]; present {
		t.Fatal("removed connection definition remains")
	}
	if _, present := app.Settings.Connections["personal"]; !present {
		t.Fatal("unrelated connection definition was removed")
	}
	api := app.Settings.Connections["api"]
	if api.EnvironmentVariable == nil || *api.EnvironmentVariable != "SERVER_KEY" {
		t.Fatalf("unrelated API-key reference changed: %+v", api)
	}
	if app.Settings.Connection == nil || !reflect.DeepEqual(*app.Settings.Connection, config.ConnectionSelection{"work"}) {
		t.Fatalf("existing default reference changed: %v", app.Settings.Connection)
	}

	state, err := manager.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, auth.State{Connections: map[config.ConnectionID]auth.OAuthMethod{
		"personal": initial.Connections["personal"],
	}}) {
		t.Fatalf("OAuth records after removal = %+v", state)
	}

	if _, err := service.ConfigureConnection(t.Context(), request); err != nil {
		t.Fatalf("repeating removal should succeed: %v", err)
	}
}

func TestConfigureConnectionCredentialRemovalFailurePreservesDefinition(t *testing.T) {
	saveErr := errors.New("credential store write failed")
	initial := auth.EmptyState()
	initial.Connections["work"] = auth.OAuthMethod{AccessToken: "work-token", AccountID: "work-account"}
	store := connectionRemovalFailingStore{MemoryStore: auth.NewMemoryStore(initial), saveErr: saveErr}
	manager := auth.NewManager(store, nil)
	resolver := authServiceResolver(t, manager, nil)
	service := NewBootstrapService(context.Background(), resolver, auth.OpenAIOAuthOptions{})

	_, err := service.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{
		Change: &authpb.ConfigureConnectionRequest_RemoveConnectionId{RemoveConnectionId: "work"},
	})
	var failure *serverapi.ConnectionFailure
	if !errors.As(err, &failure) ||
		failure.Details.Reason != authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_CREDENTIAL_CLEAR_FAILED ||
		failure.Details.GetConnectionId() != "work" ||
		failure.Details.GetDiagnostic() != saveErr.Error() {
		t.Fatalf("credential removal failure details = %+v, want typed credential-clear failure: %v", failure, err)
	}

	app, configErr := config.LoadGlobal(config.LoadOptions{ConfigRoot: resolver.root})
	if configErr != nil {
		t.Fatal(configErr)
	}
	if _, present := app.Settings.Connections["work"]; !present {
		t.Fatal("credential write failure removed the connection definition")
	}
	state, stateErr := manager.Load(t.Context())
	if stateErr != nil || state.Connections["work"] != initial.Connections["work"] {
		t.Fatalf("credentials after failed clear = %+v, %v", state, stateErr)
	}
}

func TestConfigureConnectionDefinitionRemovalFailureReportsClearedCredentials(t *testing.T) {
	initial := auth.EmptyState()
	initial.Connections["work"] = auth.OAuthMethod{
		AccessToken: "work-token", RefreshToken: "work-refresh",
		AccountID: "work-account", Email: "work@example.invalid",
	}
	manager := auth.NewManager(auth.NewMemoryStore(initial), nil)
	resolver := authServiceResolver(t, manager, nil)
	if err := os.Chmod(resolver.root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(resolver.root, 0o700); err != nil {
			t.Error(err)
		}
	})
	probe, probeErr := os.CreateTemp(resolver.root, "write-permission-probe-*")
	if probeErr == nil {
		probePath := probe.Name()
		if err := probe.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(probePath); err != nil {
			t.Fatal(err)
		}
		t.Skip("directory permission changes do not prevent writes for this user")
	}
	service := NewBootstrapService(context.Background(), resolver, auth.OpenAIOAuthOptions{})

	_, err := service.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{
		Change: &authpb.ConfigureConnectionRequest_RemoveConnectionId{RemoveConnectionId: "work"},
	})
	var failure *serverapi.ConnectionFailure
	if !errors.As(err, &failure) ||
		failure.Details.Reason != authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_DEFINITION_REMOVE_FAILED ||
		failure.Details.GetConnectionId() != "work" ||
		failure.Details.GetDiagnostic() == "" {
		t.Fatalf("definition removal failure details = %+v, want typed failure with a diagnostic: %v", failure, err)
	}

	state, stateErr := manager.Load(t.Context())
	if stateErr != nil || len(state.Connections) != 0 {
		t.Fatalf("credentials after definition write failure = %+v, %v", state, stateErr)
	}
	if err := os.Chmod(resolver.root, 0o700); err != nil {
		t.Fatal(err)
	}
	app, configErr := config.LoadGlobal(config.LoadOptions{ConfigRoot: resolver.root})
	if configErr != nil {
		t.Fatal(configErr)
	}
	if _, present := app.Settings.Connections["work"]; !present {
		t.Fatal("definition changed despite the failed settings write")
	}
}

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
	if *app.Settings.Connections["new-api"].EnvironmentVariable != "OTHER_MISSING_KEY" || !reflect.DeepEqual(app.Settings.Connection, config.SingleConnection("work")) {
		t.Fatal("reference edit changed the default or failed to save")
	}
	id := config.ConnectionID("new-api")
	app.Settings.Connection = config.SingleConnection(id)
	dispatchResolver := NewConnectionResolver(filepath.Dir(app.Source.File(config.FileGlobal).Path), resolver.manager, func(string) (string, bool) { return "", false })
	connection, err := dispatchResolver.Resolve(app.Settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Auth.ResolveDispatchAuth(t.Context()); err == nil {
		t.Fatal("missing credentials must fail when the saved connection is used")
	}
}

type connectionRemovalFailingStore struct {
	*auth.MemoryStore
	saveErr error
}

func (s connectionRemovalFailingStore) Save(context.Context, auth.State) error {
	return s.saveErr
}

func TestFirstAddedConnectionBecomesDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("theme = \"dark\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewBootstrapService(t.Context(), NewConnectionResolver(root,
		auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil), nil), auth.OpenAIOAuthOptions{})
	for _, id := range []string{"first", "second"} {
		_, err := service.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{
			Change: &authpb.ConfigureConnectionRequest_Add{Add: &authpb.ConnectionDefinition{
				Id: id, Protocol: authpb.ConnectionProtocol_CONNECTION_PROTOCOL_RESPONSES,
				Endpoint: textutil.Value("http://localhost:1234/v1"),
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := service.GetConnections(t.Context(), &authpb.GetConnectionsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if catalog.DefaultSelection == nil || !reflect.DeepEqual(catalog.DefaultSelection.Ids, []string{"first"}) {
			t.Fatalf("default after adding %s: %v", id, catalog.DefaultSelection)
		}
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
			start, err := service.StartBootstrap(t.Context(), &authpb.StartBootstrapRequest{
				Target: target, Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_CODE,
				RedirectUri: textutil.Value("http://localhost:1455/auth/callback"),
			})
			if err != nil {
				t.Fatal(err)
			}
			observer, disconnect := context.WithCancel(t.Context())
			result := make(chan error, 1)
			go func() {
				_, err := service.CompleteBootstrap(observer, &authpb.CompleteBootstrapRequest{
					Target: target, Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_CODE,
					CallbackInput: textutil.Value("grant"), Continuation: start.Continuation,
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

func TestLateOAuthSignInDoesNotRestoreRemovedConnection(t *testing.T) {
	accepted, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseExchange := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseExchange()

	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(accepted)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"late-token","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`)
	}))
	defer oauth.Close()
	endpoint, err := url.Parse(oauth.URL)
	if err != nil {
		t.Fatal(err)
	}

	initial := auth.EmptyState()
	initial.Connections["work"] = auth.OAuthMethod{AccessToken: "old-work-token", AccountID: "work-account"}
	initial.Connections["personal"] = auth.OAuthMethod{AccessToken: "personal-token", AccountID: "personal-account"}
	manager := auth.NewManager(auth.NewMemoryStore(initial), nil)
	service := NewBootstrapService(t.Context(), authServiceResolver(t, manager, nil), auth.OpenAIOAuthOptions{
		HTTPClient: &http.Client{Transport: httpclient.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			request := r.Clone(r.Context())
			request.URL.Scheme, request.URL.Host = endpoint.Scheme, endpoint.Host
			return oauth.Client().Transport.RoundTrip(request)
		})},
	})
	target := &authpb.ConnectionTarget{Target: &authpb.ConnectionTarget_ConnectionId{ConnectionId: "work"}}
	start, err := service.StartBootstrap(t.Context(), &authpb.StartBootstrapRequest{
		Target: target, Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_CODE,
		RedirectUri: textutil.Value("http://localhost:1455/auth/callback"),
	})
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() {
		_, err := service.CompleteBootstrap(t.Context(), &authpb.CompleteBootstrapRequest{
			Target: target, Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_CODE,
			Force: true, CallbackInput: textutil.Value("grant"), Continuation: start.Continuation,
		})
		completed <- err
	}()
	<-accepted

	if _, err := service.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{
		Change: &authpb.ConfigureConnectionRequest_RemoveConnectionId{RemoveConnectionId: "work"},
	}); err != nil {
		t.Fatal(err)
	}
	releaseExchange()
	if err := <-completed; err == nil {
		t.Fatal("late sign-in succeeded after its selected connection was removed")
	}

	state, err := manager.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, auth.State{Connections: map[config.ConnectionID]auth.OAuthMethod{
		"personal": initial.Connections["personal"],
	}}) {
		t.Fatalf("OAuth records after late sign-in = %+v", state)
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: service.connections.root})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := app.Settings.Connections["work"]; present {
		t.Fatal("late sign-in restored the removed definition")
	}
}
