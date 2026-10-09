package runtimewire

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"core/internal/testharness/httpclient"
	"core/server/auth"
	"core/server/authservice"
	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/textutil"
	"core/shared/transcript"
)

type expiredConnectionRuntime struct {
	wiring    *RuntimeWiring
	store     *session.Store
	manager   *auth.Manager
	resolver  *authservice.ConnectionResolver
	id        config.ConnectionID
	refreshes atomic.Int32
}

func newExpiredConnectionRuntime(t *testing.T, factory RuntimeClientFactory) *expiredConnectionRuntime {
	t.Helper()
	fixture := &expiredConnectionRuntime{id: config.ConnectionID("expired")}
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.refreshes.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(issuer.Close)
	now := time.Now()
	manager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), auth.NewOpenAIOAuthRefresher(
		auth.OpenAIOAuthOptions{Issuer: issuer.URL, HTTPClient: issuer.Client()}, func() time.Time { return now }, time.Minute,
	))
	if err := manager.SaveOAuth(t.Context(), fixture.id, auth.OAuthMethod{
		AccessToken: "expired-access", RefreshToken: "expired-refresh", Expiry: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	active := runtimeWireShellSettings(config.ShellPostprocessingModeBuiltin, nil)
	active.Model = "gpt-6-astra"
	active.ThinkingLevel = "high"
	active.Connection = config.SingleConnection(fixture.id)
	active.Connections = map[config.ConnectionID]config.ProviderConnection{fixture.id: {Protocol: config.ConnectionChatGPT}}
	root := t.TempDir()
	store := newRuntimeWireSession(t, root, "expired-auth")
	globalDir := t.TempDir()
	wiring, err := newTestRuntimeWiringWithBackground(t, store, materializedRuntimeWireEventLog(t, store),
		active, nil, manager, nil, nil, requiredRuntimeWireTestOptions(RuntimeWiringOptions{
			FilesystemContext: runtimeWireFilesystemContext(t, root),
			GlobalConfigDir:   globalDir,
			ClientFactory:     factory,
		}))
	if err != nil {
		t.Fatalf("opening Session with saved expired credentials: %v", err)
	}
	t.Cleanup(func() { _ = wiring.Close() })
	fixture.wiring, fixture.store, fixture.manager = wiring, store, manager
	fixture.resolver = authservice.NewConnectionResolver(globalDir, manager, nil)
	return fixture
}

func TestRuntimeOpensWithExpiredPresentConnectionCredentials(t *testing.T) {
	fixture := newExpiredConnectionRuntime(t, nil)
	if got := fixture.refreshes.Load(); got != 0 {
		t.Fatalf("opening Session refreshed credentials %d times", got)
	}
	before, err := fixture.manager.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.wiring.Engine.SubmitUserMessage(t.Context(), "send with expired credentials")
	if !errors.Is(err, auth.ErrOAuthRefreshFailed) || !llm.IsNonRetriableModelError(err) {
		t.Fatalf("request failure = %v, want non-retriable OAuth refresh failure", err)
	}
	if got := fixture.refreshes.Load(); got != 1 {
		t.Fatalf("failed request refreshed credentials %d times, want once", got)
	}
	var authErr *llm.AuthError
	if !errors.As(err, &authErr) || authErr.ConnectionID == nil || *authErr.ConnectionID != fixture.id {
		t.Fatalf("refresh failure lost typed connection identity: %v", err)
	}
	if authErr.Unwrap() == nil || !errors.Is(authErr.Unwrap(), auth.ErrOAuthRefreshFailed) {
		t.Fatal("authentication error lost the original refresh failure")
	}
	if authErr.Error() != authErr.Unwrap().Error() {
		t.Fatal("authentication error changed original diagnostics")
	}
	after, loadErr := fixture.manager.Load(t.Context())
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if credential, present := after.Connections[fixture.id]; !present || credential != before.Connections[fixture.id] {
		t.Fatal("refresh failure deleted or changed saved credentials")
	}
	message := llm.UserFacingError(err)
	if message == "" {
		t.Fatal("refresh failure has no sign-in guidance")
	}
	window, err := materializedRuntimeWireEventLog(t, fixture.store).ReadNewestSegmentBackward(nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range window.Records {
		payload, err := record.Payload()
		if err != nil {
			t.Fatal(err)
		}
		if entry, ok := payload.(session.LocalEntryRecord); ok && entry.Role == string(transcript.EntryRoleDeveloperErrorFeedback) {
			if entry.Text != nil && *entry.Text == message {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("request failure was not persisted as transcript error feedback")
	}
}

func TestRuntimeNativeThinkingUsesActualConnectionDespiteLockedProvider(t *testing.T) {
	firstParty := config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://api.openai.com/v1")}
	custom := config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://proxy.example/v1")}
	for _, test := range []struct {
		name   string
		actual config.ProviderConnection
		locked config.ProviderConnection
		native bool
	}{
		{name: "native actual with custom locked", actual: firstParty, locked: custom, native: true},
		{name: "custom actual with native locked", actual: custom, locked: firstParty},
	} {
		t.Run(test.name, func(t *testing.T) {
			lockedCaps, err := llm.ResolveConnectionCapabilities(test.locked)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			store := newRuntimeWireSession(t, root, "locked-provider-thinking")
			if err := store.AdoptOriginalThinkingEffort("high"); err != nil {
				t.Fatal(err)
			}
			if err := store.MarkModelDispatchLocked(session.LockedContract{
				Model: "gpt-6-astra", ModelCapabilities: llm.LockedModelCapabilitiesForModel("gpt-6-astra", lockedCaps),
				ProviderContract: llm.LockedProviderCapabilitiesFromContract(lockedCaps),
			}); err != nil {
				t.Fatal(err)
			}
			active := runtimeWireShellSettings(config.ShellPostprocessingModeBuiltin, nil)
			active.Model, active.ThinkingLevel = "gpt-6-astra", "low"
			id := config.ConnectionID("actual")
			active.Connection = config.SingleConnection(id)
			active.Connections = map[config.ConnectionID]config.ProviderConnection{id: test.actual}
			wiring, err := newTestRuntimeWiringWithBackground(t, store, materializedRuntimeWireEventLog(t, store),
				active, nil, nil, nil, nil, requiredRuntimeWireTestOptions(RuntimeWiringOptions{FilesystemContext: runtimeWireFilesystemContext(t, root)}))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = wiring.Close() })
			request, err := runtime.PrepareInspectionRequest(t.Context(), wiring.Engine, false)
			if err != nil {
				t.Fatal(err)
			}
			native := false
			for _, item := range request.Items {
				native = native || item.Type == llm.ResponseItemTypeConfigurationUpdate
			}
			if native != test.native {
				t.Fatalf("native Thinking update = %v, want actual connection support %v", native, test.native)
			}
			if got := store.Meta().Locked.ProviderContract.ProviderID; got != lockedCaps.ProviderID {
				t.Fatalf("locked request provider = %s, want %s", got, lockedCaps.ProviderID)
			}
		})
	}
}

func TestRuntimeAcceptsNextTurnAfterConnectionReauthentication(t *testing.T) {
	var requests atomic.Int32
	var exchanges atomic.Int32
	const answer = "next turn completed"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/oauth/token" {
			exchanges.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"renewed-access","refresh_token":"renewed-refresh","token_type":"Bearer","expires_in":3600}`)
			return
		}
		requests.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer renewed-access" {
			t.Errorf("next request authorization = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-recovered\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"message-recovered\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\""+answer+"\",\"annotations\":[]}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: httpclient.NewURLRewriteTransport(target, server.Client().Transport, "")}
	fixture := newExpiredConnectionRuntime(t, RuntimeClientFactoryFunc(func(ctx context.Context, request RuntimeClientRequest) (llm.Client, error) {
		capabilities, err := llm.ResolveConnectionCapabilities(request.Connection.Definition)
		if err != nil {
			return nil, err
		}
		return llm.NewProviderClient(llm.ProviderClientOptions{
			Provider: llm.ProviderOpenAI, Model: request.ActiveSettings.Model, Auth: request.Connection.Auth,
			HTTPClient: client, ContextWindowTokens: request.ActiveSettings.ModelContextWindow,
			ProviderCapabilitiesOverride: &capabilities, RequestCapabilities: &request.RequestCapabilities,
		})
	}))
	if _, err := fixture.wiring.Engine.SubmitUserMessage(t.Context(), "failed turn"); !errors.Is(err, auth.ErrOAuthRefreshFailed) {
		t.Fatalf("expired request failure = %v", err)
	}
	service := authservice.NewBootstrapService(t.Context(), fixture.resolver, auth.OpenAIOAuthOptions{Issuer: server.URL, HTTPClient: client})
	result, err := service.CompleteBootstrap(t.Context(), &authpb.CompleteBootstrapRequest{
		Target: protoapi.ExistingConnectionTarget(fixture.id), Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
		DeviceAuthorizationCode: textutil.Value("synthetic-grant"), DeviceCodeVerifier: textutil.Value("synthetic-verifier"), Force: true,
	})
	if err != nil || !result.AuthReady || result.ConnectionId != string(fixture.id) {
		t.Fatalf("re-authentication result = %+v, %v", result, err)
	}
	response, err := fixture.wiring.Engine.SubmitUserMessage(t.Context(), "new turn after sign-in")
	if err != nil || response.Content == nil || *response.Content != answer {
		t.Fatalf("next turn = %+v, %v", response, err)
	}
	if requests.Load() != 1 || exchanges.Load() != 1 || fixture.refreshes.Load() != 1 {
		t.Fatalf("requests=%d exchanges=%d refreshes=%d, want one of each and no replay", requests.Load(), exchanges.Load(), fixture.refreshes.Load())
	}
}
