package authservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"core/internal/testharness/httpclient"
	"core/internal/testharness/testsetup"
	"core/server/auth"
	"core/server/launch"
	"core/server/llm"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/textutil"
)

func TestGrokConnectionCredentialIsolation(t *testing.T) {
	transport := http.DefaultTransport
	requests := map[string]*http.Request{}
	requestCounts := map[string]int{}
	http.DefaultTransport = httpclient.RoundTripFunc(func(request *http.Request) (*http.Response, error) {
		authorization := request.Header.Get("Authorization")
		requests[authorization] = request.Clone(request.Context())
		requestCounts[authorization]++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"config":{"creditUsagePercent":0}}`)),
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = transport })
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	store := auth.NewMemoryStore(auth.EmptyState())
	manager := auth.NewManager(store, nil)
	for _, protocol := range []config.ConnectionProtocol{config.ConnectionGrokCLIProxy, config.ConnectionGrokOAuthAPI, config.ConnectionGrokAPIKey} {
		id := config.ConnectionID(protocol)
		definition := config.ProviderConnection{Protocol: protocol, Capabilities: config.ProviderCapabilitiesOverride{
			ProviderID: "chatgpt-codex", SupportsResponsesAPI: true, IsOpenAIFirstParty: true,
		}}
		if protocol == config.ConnectionGrokAPIKey {
			definition.EnvironmentVariable = textutil.Value("SELECTED_GROK_KEY")
		}
		if err := config.AddProviderConnection(filepath.Join(root, "config.toml"), id, definition); err != nil {
			t.Fatal(err)
		}
		if err := manager.SaveOAuth(t.Context(), id, auth.OAuthMethod{AccessToken: string(id), AccountID: "must-not-send-chatgpt-account"}); err != nil {
			t.Fatal(err)
		}
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewConnectionResolver(root, manager, func(name string) (string, bool) {
		if name != "SELECTED_GROK_KEY" {
			t.Fatalf("read unrelated credential %q", name)
		}
		return "selected-api-key", true
	})
	for id := range app.Settings.Connections {
		t.Run(string(id), func(t *testing.T) {
			settings := app.Settings
			settings.Connection = config.SingleConnection(id)
			connection, err := resolver.Resolve(settings)
			if err != nil {
				t.Fatal(err)
			}
			credential, err := connection.Auth.ResolveDispatchAuth(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			expected := string(id)
			if id == config.ConnectionID(config.ConnectionGrokAPIKey) {
				expected = "selected-api-key"
			}
			if credential == nil || credential.Header != "Bearer "+expected || credential.Mode.AccountID != "" {
				t.Fatalf("wrong selected credential: %+v", credential)
			}
			authStatus, err := NewStatusService(resolver, nil).GetStatus(t.Context(), &authpb.GetStatusRequest{
				Provider: &authpb.ProviderSelection{ConnectionId: string(id)},
			})
			wantApplicable := id == config.ConnectionID(config.ConnectionGrokCLIProxy) ||
				id == config.ConnectionID(config.ConnectionGrokOAuthAPI)
			if err != nil || authStatus.GetResolution().GetKnown() == nil || authStatus.GetSubscription().GetApplicable() != wantApplicable {
				t.Fatalf("Grok subscription status = %+v, %v", authStatus, err)
			}
			if wantApplicable && (authStatus.GetSubscription().GetQuotaSource() != authpb.SubscriptionQuotaSource_SUBSCRIPTION_QUOTA_SOURCE_GROK ||
				len(authStatus.GetSubscription().GetWindows()) != 1 ||
				authStatus.GetSubscription().GetWindows()[0].GetDurationSeconds() != 7*24*60*60 ||
				authStatus.GetSubscription().GetWindows()[0].GetUsedPercent() != 0) {
				t.Fatalf("Grok billing projection = %+v", authStatus.GetSubscription())
			}
			if facts := authStatus.GetResolution().GetKnown().GetProvider(); facts.GetKind() != authpb.ProviderKind_PROVIDER_KIND_CONFIGURED_PROVIDER || facts.Identifier != string(id) {
				t.Fatalf("capability override changed authentication provider identity: %+v", facts)
			}
			if connection.Definition.Protocol.IsSubscription() {
				service := NewBootstrapService(t.Context(), resolver, auth.OpenAIOAuthOptions{})
				target := protoapi.ExistingConnectionTarget(id)
				status, err := service.GetBootstrapStatus(t.Context(), &authpb.GetBootstrapStatusRequest{Target: target})
				if err != nil {
					t.Fatal(err)
				}
				if len(status.SupportedModes) != 1 || status.SupportedModes[0] != authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE || status.CallbackTransport != nil {
					t.Fatalf("Grok auth modes = %+v", status)
				}
				_, err = service.StartBootstrap(t.Context(), &authpb.StartBootstrapRequest{
					Target: target, Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
					RedirectUri: textutil.Value("http://127.0.0.1:48219/callback"),
				})
				if !errors.Is(err, auth.ErrInvalidAuthMethod) {
					t.Fatalf("unsupported Grok browser grant = %v", err)
				}
			}
		})
	}
	for _, id := range []config.ConnectionID{
		config.ConnectionID(config.ConnectionGrokCLIProxy),
		config.ConnectionID(config.ConnectionGrokOAuthAPI),
	} {
		status, err := NewStatusService(resolver, nil).GetStatus(t.Context(), &authpb.GetStatusRequest{
			Provider:              &authpb.ProviderSelection{ConnectionId: string(id)},
			SkipSubscriptionUsage: true,
		})
		if err != nil || status.GetSubscription().GetApplicable() {
			t.Fatalf("skipped Grok subscription status = %+v, %v", status, err)
		}
		if requestCounts["Bearer "+string(id)] != 1 {
			t.Fatalf("billing requests for %s = %d", id, requestCounts["Bearer "+string(id)])
		}
	}
	for _, id := range []config.ConnectionID{
		config.ConnectionID(config.ConnectionGrokCLIProxy),
		config.ConnectionID(config.ConnectionGrokOAuthAPI),
	} {
		request := requests["Bearer "+string(id)]
		if request == nil || request.Method != http.MethodGet ||
			request.URL.String() != "https://cli-chat-proxy.grok.com/v1/billing?format=credits" ||
			request.Header.Get("User-Agent") != "kent/dev" ||
			request.Header.Get("ChatGPT-Account-Id") != "" ||
			request.Header.Get("x-userid") != "" ||
			request.Header.Get("X-XAI-Token-Auth") != "" ||
			request.Header.Get("x-grok-client-version") != "" {
			t.Fatalf("Grok billing request for %s = %+v", id, request)
		}
	}
	if len(requests) != 2 {
		t.Fatalf("billing requests = %v", requests)
	}
}

func TestConnectionDispatchCredentialIsolation(t *testing.T) {
	root := t.TempDir()
	body := `
[connections.work]
protocol = "chatgpt-codex"
[connections.personal]
protocol = "chatgpt-codex"
[connections.api]
protocol = "responses"
endpoint = "https://compatible.example/v1"
environment_variable = %q
[connections.local]
protocol = "responses"
endpoint = "https://compatible.example/v1"
`
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(fmt.Sprintf(body, "SELECTED_KEY")), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)
	for _, id := range []config.ConnectionID{"work", "personal"} {
		if err := manager.SaveOAuth(t.Context(), id, auth.OAuthMethod{AccessToken: string(id), AccountID: "account-" + string(id)}); err != nil {
			t.Fatal(err)
		}
	}
	resolver := NewConnectionResolver(root, manager, func(name string) (string, bool) {
		values := map[string]string{"SELECTED_KEY": "selected-key", "REPLACEMENT_KEY": "replacement-key"}
		value, present := values[name]
		return value, present
	})
	headers := make(chan http.Header, 8)
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		if r.Header.Get("Authorization") == "Bearer work" {
			close(started)
			<-release
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[]}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(release) })
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: httpclient.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		copy.URL.Scheme, copy.URL.Host = target.Scheme, target.Host
		return server.Client().Transport.RoundTrip(copy)
	})}
	transports := make(map[config.ConnectionID]*llm.HTTPTransport)
	for _, id := range []config.ConnectionID{"work", "personal", "api", "local"} {
		settings := app.Settings
		settings.Connection = config.SingleConnection(id)
		connection, err := resolver.Resolve(settings)
		if err != nil {
			t.Fatal(err)
		}
		registration, err := llm.ResolveConnectionVariant(connection.Definition)
		if err != nil {
			t.Fatal(err)
		}
		transport, err := llm.NewHTTPTransport(connection.Auth, registration)
		if err != nil {
			t.Fatal(err)
		}
		transport.Client = client
		transports[id] = transport
	}
	send := func(id config.ConnectionID) error {
		request := llm.ResponsesRequest{Model: "gpt-6-sol", SessionID: textutil.Value(string(id)), ToolChoiceMode: llm.ToolChoiceModeAutomatic, ReasoningEffort: "high"}
		if id == "work" || id == "personal" {
			var err error
			request.CodexDispatch, err = llm.NewCodexDispatchContext(llm.CodexDispatchFacts{
				SessionID: string(id), StepID: "step", RequestKind: llm.CodexRequestKindTurn.Optional(),
			})
			if err != nil {
				return err
			}
		}
		_, err := transports[id].Generate(t.Context(), request, llm.StreamCallbacks{})
		return err
	}
	var wg sync.WaitGroup
	for id := range transports {
		wg.Go(func() {
			if err := send(id); err != nil {
				t.Error(err)
			}
		})
	}
	<-started
	if err := manager.SaveOAuth(t.Context(), "work", auth.OAuthMethod{AccessToken: "new-work", AccountID: "new-account"}); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	wg.Wait()
	for range transports {
		select {
		case header := <-headers:
			id := header.Get("session-id")
			wantToken, wantAccount := "", ""
			switch id {
			case "work", "personal":
				wantToken, wantAccount = "Bearer "+id, "account-"+id
			case "api":
				wantToken = "Bearer selected-key"
			case "local":
			default:
				t.Fatalf("unexpected Session %q", id)
			}
			if header.Get("Authorization") != wantToken || header.Get("ChatGPT-Account-Id") != wantAccount {
				t.Fatalf("wrong credential/account pairing for %s", id)
			}
		default:
			t.Fatal("a connection did not dispatch")
		}
	}
	if err := send("work"); err != nil {
		t.Fatal(err)
	}
	header := <-headers
	if header.Get("Authorization") != "Bearer new-work" || header.Get("ChatGPT-Account-Id") != "new-account" {
		t.Fatal("future dispatch did not use re-authenticated credentials")
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(fmt.Sprintf(body, "REPLACEMENT_KEY")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := send("api"); err != nil {
		t.Fatal(err)
	}
	header = <-headers
	if header.Get("Authorization") != "Bearer replacement-key" {
		t.Fatal("future dispatch retained the old environment reference")
	}
}

func TestConnectionMissingEnvironmentDoesNotBecomeAnonymous(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(`
connection = ["api", "local"]
[connections.api]
protocol = "responses"
endpoint = "https://compatible.example/v1"
environment_variable = "MISSING_KEY"
[connections.local]
protocol = "responses"
endpoint = "http://127.0.0.1:1/v1"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, present := range []bool{false, true} {
		rotation := launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
		id, err := rotation.Prepare(t.Context(), app.Settings, true)
		if err != nil || id != "api" {
			t.Fatalf("selected member = %v, %v", id, err)
		}
		settings := app.Settings
		settings.Connection = config.SingleConnection(id)
		resolved, err := NewConnectionResolver(root, nil, func(string) (string, bool) { return "", present }).Resolve(settings)
		if err != nil {
			t.Fatal(err)
		}
		registration, err := llm.ResolveConnectionVariant(resolved.Definition)
		if err != nil {
			t.Fatal(err)
		}
		transport, err := llm.NewHTTPTransport(resolved.Auth, registration)
		if err != nil {
			t.Fatal(err)
		}
		transport.Client = &http.Client{Transport: httpclient.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("missing key reached the network")
			return nil, context.Canceled
		})}
		_, err = transport.Generate(t.Context(), llm.ResponsesRequest{
			Model: "local-model", SessionID: textutil.Value("session"), ToolChoiceMode: llm.ToolChoiceModeAutomatic,
		}, llm.StreamCallbacks{})
		if err == nil {
			t.Fatal("missing or empty key must block dispatch")
		}
		next, err := rotation.Prepare(t.Context(), app.Settings, true)
		if err != nil || next != "local" {
			t.Fatalf("credential failure consumed another member: %v, %v", next, err)
		}
	}
}

func TestSelectedConnectionProviderRejectionDoesNotSwitchMembers(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Model != "operator-model" {
			t.Errorf("authored model changed: %v", payload.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"unsupported model","type":"invalid_request_error","code":"model_not_found"}}`)
	}))
	t.Cleanup(endpoint.Close)
	selection := config.ConnectionSelection{"a", "b"}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(fmt.Sprintf(`
[connections.a]
protocol = "responses"
endpoint = %q
[connections.b]
protocol = "responses"
endpoint = %q
`, endpoint.URL, endpoint.URL)), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{
		Connection: &selection, ConnectionOrder: []config.ConnectionID{"a", "b"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"a": {Protocol: config.ConnectionResponses, Endpoint: &endpoint.URL},
			"b": {Protocol: config.ConnectionResponses, Endpoint: &endpoint.URL},
		},
	}
	rotation := launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	id, err := rotation.Prepare(t.Context(), settings, true)
	if err != nil || id != "a" {
		t.Fatalf("selected member = %v, %v", id, err)
	}
	concrete := settings
	concrete.Connection = config.SingleConnection(id)
	resolved, err := NewConnectionResolver(root, nil, nil).Resolve(concrete)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := llm.ResolveConnectionVariant(resolved.Definition)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := llm.NewHTTPTransport(resolved.Auth, registration)
	if err != nil {
		t.Fatal(err)
	}
	transport.Client = endpoint.Client()
	_, err = transport.Generate(t.Context(), llm.ResponsesRequest{
		Model: "operator-model", SessionID: textutil.Value("session"), ToolChoiceMode: llm.ToolChoiceModeAutomatic,
	}, llm.StreamCallbacks{})
	var providerErr *llm.ProviderAPIError
	if !errors.As(err, &providerErr) || !HasHTTPStatus(providerErr, http.StatusBadRequest) || requests.Load() != 1 {
		t.Fatalf("selected-provider failure = %v, requests=%d", err, requests.Load())
	}
	next, err := rotation.Prepare(t.Context(), settings, true)
	if err != nil || next != "b" {
		t.Fatalf("provider rejection consumed another member: %v, %v", next, err)
	}
}
