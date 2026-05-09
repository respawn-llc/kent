package oauthadapter

import (
	"strings"
	"testing"
)

func TestBeginOpenAIBrowserFlowUsesManualRedirectFallback(t *testing.T) {
	session, err := BeginOpenAIBrowserFlow(OpenAIOAuthOptions{
		Issuer:   "https://auth.example.test/",
		ClientID: "client-test",
	}, "")
	if err != nil {
		t.Fatalf("begin browser flow: %v", err)
	}
	if !strings.Contains(session.AuthorizeURL, "https://auth.example.test/oauth/authorize") {
		t.Fatalf("unexpected authorize URL: %s", session.AuthorizeURL)
	}
	if !strings.Contains(session.AuthorizeURL, "client_id=client-test") {
		t.Fatalf("authorize URL missing client ID: %s", session.AuthorizeURL)
	}
	if session.RedirectURI == "" || session.State == "" || session.CodeVerifier == "" {
		t.Fatalf("expected redirect URI/state/verifier, got %+v", session)
	}
}
