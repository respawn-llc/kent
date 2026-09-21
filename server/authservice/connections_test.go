package authservice

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"core/internal/testharness/httpclient"
	"core/server/auth"
	"core/server/llm"
	"core/shared/config"
	"core/shared/textutil"
)

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
		settings.Connection = &id
		connection, err := resolver.Resolve(settings)
		if err != nil {
			t.Fatal(err)
		}
		transport := llm.NewHTTPTransport(connection.Auth)
		transport.Client = client
		if connection.Definition.Endpoint != nil {
			transport.BaseURL, transport.BaseURLExplicit = *connection.Definition.Endpoint, true
		}
		transports[id] = transport
	}
	send := func(id config.ConnectionID) error {
		request := llm.OpenAIRequest{Model: "gpt-5.6-sol", SessionID: textutil.Value(string(id)), ToolChoiceMode: llm.ToolChoiceModeAutomatic}
		if id == "work" || id == "personal" {
			var err error
			request.CodexDispatch, err = llm.NewCodexDispatchContext(llm.CodexDispatchFacts{
				SessionID: string(id), RunID: "run", RequestKind: llm.CodexRequestKindTurn.Optional(),
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
connection = "api"
[connections.api]
protocol = "responses"
endpoint = "https://compatible.example/v1"
environment_variable = "MISSING_KEY"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, present := range []bool{false, true} {
		resolved, err := NewConnectionResolver(root, nil, func(string) (string, bool) { return "", present }).Resolve(app.Settings)
		if err != nil {
			t.Fatal(err)
		}
		transport := llm.NewHTTPTransport(resolved.Auth)
		transport.BaseURL, transport.BaseURLExplicit = *resolved.Definition.Endpoint, true
		transport.Client = &http.Client{Transport: httpclient.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("missing key reached the network")
			return nil, context.Canceled
		})}
		_, err = transport.Generate(t.Context(), llm.OpenAIRequest{
			Model: "local-model", SessionID: textutil.Value("session"), ToolChoiceMode: llm.ToolChoiceModeAutomatic,
		}, llm.StreamCallbacks{})
		if err == nil {
			t.Fatal("missing or empty key must block dispatch")
		}
	}
}
