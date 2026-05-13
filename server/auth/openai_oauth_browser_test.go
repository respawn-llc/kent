package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBeginOpenAIBrowserFlowBuildsOAuthAuthorizeURL(t *testing.T) {
	session, err := BeginOpenAIBrowserFlow(OpenAIOAuthOptions{
		Issuer:   "https://auth.openai.com",
		ClientID: "client-1",
	}, "http://localhost:5555/auth/callback")
	if err != nil {
		t.Fatalf("begin flow: %v", err)
	}

	u, err := url.Parse(session.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	if got := u.Path; got != "/oauth/authorize" {
		t.Fatalf("unexpected authorize path %q", got)
	}
	q := u.Query()
	if got := q.Get("client_id"); got != "client-1" {
		t.Fatalf("client_id=%q", got)
	}
	if got := q.Get("response_type"); got != "code" {
		t.Fatalf("response_type=%q", got)
	}
	if got := q.Get("redirect_uri"); got != "http://localhost:5555/auth/callback" {
		t.Fatalf("redirect_uri=%q", got)
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Fatalf("code_challenge_method=%q", got)
	}
	if got := q.Get("scope"); got != "openid profile email offline_access" {
		t.Fatalf("scope=%q", got)
	}
	if got := q.Get("id_token_add_organizations"); got != "true" {
		t.Fatalf("id_token_add_organizations=%q", got)
	}
	if got := q.Get("codex_cli_simplified_flow"); got != "true" {
		t.Fatalf("codex_cli_simplified_flow=%q", got)
	}
	if got := q.Get("originator"); got != "builder" {
		t.Fatalf("originator=%q", got)
	}
	if got := q.Get("state"); got == "" {
		t.Fatal("expected non-empty state")
	}
	if got := q.Get("code_challenge"); got == "" {
		t.Fatal("expected non-empty code_challenge")
	}
}

func TestStartOAuthCallbackListenerUsesLocalhostAuthCallback(t *testing.T) {
	listener, err := StartOAuthCallbackListener()
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "address already in use") {
			t.Skipf("oauth callback port in use: %v", err)
		}
		t.Fatalf("start listener: %v", err)
	}
	defer listener.Close()

	u, err := url.Parse(listener.RedirectURI())
	if err != nil {
		t.Fatalf("parse redirect uri: %v", err)
	}
	if got := u.Scheme; got != "http" {
		t.Fatalf("scheme=%q", got)
	}
	if got := u.Hostname(); got != "localhost" {
		t.Fatalf("hostname=%q", got)
	}
	if got := u.Path; got != "/auth/callback" {
		t.Fatalf("path=%q", got)
	}
	if got := u.Port(); got != "1455" {
		t.Fatalf("port=%q", got)
	}
}

func TestOAuthCallbackSuccessResponseServesAuthCompleteHTML(t *testing.T) {
	listener, err := StartOAuthCallbackListener()
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "address already in use") {
			t.Skipf("oauth callback port in use: %v", err)
		}
		t.Fatalf("start listener: %v", err)
	}
	defer listener.Close()

	resp, err := http.Get(listener.RedirectURI() + "?code=auth-code&state=state-1")
	if err != nil {
		t.Fatalf("get callback: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read callback body: %v", err)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("content-type=%q, want text/html", got)
	}
	for _, want := range []string{"Auth complete", "You can close this tab now.", "color-scheme: dark", "--primary:"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("expected callback response to contain %q, got %q", want, string(body))
		}
	}
}

func TestAuthCompleteHTMLUsesDarkTerminalConfirmation(t *testing.T) {
	body := authCompleteHTML()
	for _, want := range []string{"Auth complete", "You can close this tab now.", "color-scheme: dark", "--primary:", "font-size: 16px"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected auth confirmation html to contain %q, got %q", want, body)
		}
	}
}

func TestCompleteOpenAIBrowserFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "authorization_code" {
				t.Fatalf("grant_type=%q", got)
			}
			if got := r.Form.Get("code"); got != "auth-code-1" {
				t.Fatalf("code=%q", got)
			}
			if got := r.Form.Get("redirect_uri"); got != "http://127.0.0.1:5555/callback" {
				t.Fatalf("redirect_uri=%q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "browser-access",
				"refresh_token": "browser-refresh",
				"token_type":    "Bearer",
				"expires_in":    1800,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	session, err := BeginOpenAIBrowserFlow(OpenAIOAuthOptions{
		ClientID:   "client-1",
		HTTPClient: rewriteOAuthIssuerClient(server),
	}, "http://127.0.0.1:5555/callback")
	if err != nil {
		t.Fatalf("begin flow: %v", err)
	}

	method, err := CompleteOpenAIBrowserFlow(context.Background(), OpenAIOAuthOptions{
		ClientID:   "client-1",
		HTTPClient: rewriteOAuthIssuerClient(server),
	}, session, "http://127.0.0.1:5555/callback?code=auth-code-1&state="+session.State)
	if err != nil {
		t.Fatalf("complete flow: %v", err)
	}
	if method.Type != MethodOAuth || method.OAuth == nil {
		t.Fatalf("unexpected method: %+v", method)
	}
	if method.OAuth.AccessToken != "browser-access" || method.OAuth.RefreshToken != "browser-refresh" {
		t.Fatalf("unexpected tokens: %+v", method.OAuth)
	}
}

func TestCompleteOpenAIBrowserFlowWithDefaultHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "authorization_code" {
				t.Fatalf("grant_type=%q", got)
			}
			if got := r.Form.Get("code"); got != "auth-code-2" {
				t.Fatalf("code=%q", got)
			}
			if got := r.Form.Get("redirect_uri"); got != "http://localhost:1455/auth/callback" {
				t.Fatalf("redirect_uri=%q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "browser-access-2",
				"refresh_token": "browser-refresh-2",
				"token_type":    "Bearer",
				"expires_in":    1800,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	session, err := BeginOpenAIBrowserFlow(OpenAIOAuthOptions{
		ClientID: "client-2",
	}, "http://localhost:1455/auth/callback")
	if err != nil {
		t.Fatalf("begin flow: %v", err)
	}

	method, err := CompleteOpenAIBrowserFlow(context.Background(), OpenAIOAuthOptions{
		ClientID:   "client-2",
		HTTPClient: rewriteOAuthIssuerClient(server),
	}, session, "http://localhost:1455/auth/callback?code=auth-code-2&state="+session.State)
	if err != nil {
		t.Fatalf("complete flow: %v", err)
	}
	if method.Type != MethodOAuth || method.OAuth == nil {
		t.Fatalf("unexpected method: %+v", method)
	}
	if method.OAuth.AccessToken != "browser-access-2" || method.OAuth.RefreshToken != "browser-refresh-2" {
		t.Fatalf("unexpected tokens: %+v", method.OAuth)
	}
}

func TestCompleteOpenAIBrowserFlowRejectsStateMismatch(t *testing.T) {
	session := BrowserAuthSession{
		State:        "expected",
		CodeVerifier: "verifier",
		RedirectURI:  "http://127.0.0.1:5555/callback",
	}
	_, err := CompleteOpenAIBrowserFlow(context.Background(), OpenAIOAuthOptions{}, session, "http://127.0.0.1:5555/callback?code=c1&state=wrong")
	if err == nil {
		t.Fatal("expected state mismatch error")
	}
}

func TestParseOAuthCallbackInput(t *testing.T) {
	parsed, err := ParseOAuthCallbackInput("http://localhost/callback?code=abc&state=s1")
	if err != nil {
		t.Fatalf("parse url callback: %v", err)
	}
	if parsed.Code != "abc" || parsed.State != "s1" {
		t.Fatalf("unexpected parsed callback: %+v", parsed)
	}

	parsed, err = ParseOAuthCallbackInput("code=abc&state=s2")
	if err != nil {
		t.Fatalf("parse query callback: %v", err)
	}
	if parsed.Code != "abc" || parsed.State != "s2" {
		t.Fatalf("unexpected parsed callback: %+v", parsed)
	}

	parsed, err = ParseOAuthCallbackInput("abc")
	if err != nil {
		t.Fatalf("parse plain callback: %v", err)
	}
	if parsed.Code != "abc" {
		t.Fatalf("unexpected parsed callback: %+v", parsed)
	}
}
