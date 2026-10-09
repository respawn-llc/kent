package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestGrokRefreshFailureRetainsProviderDiagnostic(t *testing.T) {
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                        "https://auth.x.ai",
				"authorization_endpoint":        "https://auth.x.ai/oauth2/authorize",
				"token_endpoint":                "https://auth.x.ai/oauth2/token",
				"device_authorization_endpoint": "https://auth.x.ai/oauth2/device/code",
			})
		case "/oauth2/token":
			tokenRequests++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("grant_type") != "refresh_token" ||
				r.Form.Get("refresh_token") != "selected-refresh" ||
				r.Form.Get("client_id") != "b1a00492-073a-47ea-816f-4c329264a828" {
				t.Errorf("unexpected refresh grant: %v", r.Form)
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "invalid_grant", "error_description": "fixture diagnostic",
			})
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	_, err := RefreshGrokAuthToken(t.Context(), rewriteOAuthIssuerClient(server), OAuthMethod{
		AccessToken: "selected-access", RefreshToken: "selected-refresh",
	})
	var providerError *GrokOAuthError
	if !errors.Is(err, ErrOAuthRefreshFailed) || !errors.As(err, &providerError) ||
		providerError.Code != "invalid_grant" || providerError.StatusCode != http.StatusBadRequest {
		t.Fatalf("refresh error = %v", err)
	}
	if tokenRequests != 1 {
		t.Fatalf("refresh attempted %d exchanges", tokenRequests)
	}
}

func TestGrokBrowserExchangeUsesBoundRedirect(t *testing.T) {
	const redirect = "http://127.0.0.1:48219/callback"
	var verifier string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(grokDiscovery{
				Issuer: "https://auth.x.ai", AuthorizationEndpoint: "https://auth.x.ai/oauth2/authorize",
				TokenEndpoint: "https://auth.x.ai/oauth2/token", DeviceAuthorizationEndpoint: "https://auth.x.ai/oauth2/device/code",
			})
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("redirect_uri") != redirect || r.Form.Get("code_verifier") != verifier ||
				r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "fixture-code" {
				t.Errorf("unexpected browser grant: %v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "browser-token", "refresh_token": "browser-refresh"})
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := rewriteOAuthIssuerClient(server)
	session, err := BeginGrokBrowserFlow(t.Context(), client, redirect)
	if err != nil {
		t.Fatal(err)
	}
	verifier = session.CodeVerifier
	authorization, err := url.Parse(session.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	if authorization.Query().Get("redirect_uri") != redirect {
		t.Fatalf("authorization changed bound redirect: %s", authorization)
	}
	if authorization.Query().Get("code_challenge") == "" || authorization.Query().Get("state") != session.State {
		t.Fatal("authorization omitted PKCE/state")
	}
	credential, err := CompleteGrokBrowserFlow(t.Context(), client, session, redirect+"?code=fixture-code&state="+session.State)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "browser-token" || credential.RefreshToken != "browser-refresh" || credential.Expiry != nil {
		t.Fatalf("credential = %+v", credential)
	}
}
