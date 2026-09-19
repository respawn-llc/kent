package authservice

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"core/internal/testharness/httpclient"
	"core/server/auth"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/textutil"
)

func authServiceResolver(t *testing.T, manager *auth.Manager, lookup func(string) (string, bool)) *ConnectionResolver {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(`
connection = "work"
[connections.work]
protocol = "chatgpt-codex"
[connections.personal]
protocol = "chatgpt-codex"
[connections.local]
protocol = "responses"
endpoint = "http://localhost:1234"
[connections.api]
protocol = "responses"
endpoint = "http://localhost:1234"
environment_variable = "SERVER_KEY"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewConnectionResolver(root, manager, lookup)
}

func TestConnectionOAuthSignInReauthenticationAndFailure(t *testing.T) {
	manager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)
	if err := manager.SaveOAuth(t.Context(), "personal", auth.OAuthMethod{AccessToken: "personal"}); err != nil {
		t.Fatal(err)
	}
	var exchanges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			t.Errorf("unexpected OAuth endpoint %s", r.URL.Path)
		}
		call := exchanges.Add(1)
		if call == 3 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"work-%d","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`, call)
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	service := NewBootstrapService(authServiceResolver(t, manager, nil), auth.OpenAIOAuthOptions{
		HTTPClient: &http.Client{Transport: httpclient.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			copy := r.Clone(r.Context())
			copy.URL.Scheme, copy.URL.Host = target.Scheme, target.Host
			return server.Client().Transport.RoundTrip(copy)
		})},
	})
	request := &authpb.CompleteBootstrapRequest{
		ConnectionId: textutil.Value("work"), Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
		DeviceAuthorizationCode: textutil.Value("grant"), DeviceCodeVerifier: textutil.Value("verifier"),
	}
	for attempt := 1; attempt <= 3; attempt++ {
		request.Force = attempt > 1
		result, err := service.CompleteBootstrap(t.Context(), request)
		if attempt == 3 {
			if err == nil {
				t.Fatal("rejected OAuth exchange reported success")
			}
		} else if err != nil || !result.AuthReady || result.ConnectionId != "work" {
			t.Fatalf("sign-in attempt %d: %+v, %v", attempt, result, err)
		}
		state, err := manager.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if state.Connections["personal"].AccessToken != "personal" || state.Connections["work"].AccessToken != fmt.Sprintf("work-%d", min(attempt, 2)) {
			t.Fatal("sign-in changed another connection or replaced credentials after failure")
		}
	}
}
