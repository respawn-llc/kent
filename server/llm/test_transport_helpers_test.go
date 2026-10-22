package llm

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"core/internal/testharness/httpclient"
	"core/shared/config"
)

func testConnectionRegistration(t *testing.T, connection config.ProviderConnection) ProviderVariantRegistration {
	t.Helper()
	registration, err := ResolveConnectionVariant(connection)
	if err != nil {
		t.Fatal(err)
	}
	return registration
}

func newTestHTTPTransport(t *testing.T, auth DispatchAuthProvider, registration ProviderVariantRegistration) *HTTPTransport {
	t.Helper()
	transport, err := NewHTTPTransport(auth, registration)
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func newCanonicalOAuthTestTransport(t *testing.T, server *httptest.Server) *HTTPTransport {
	t.Helper()
	transport := newTestHTTPTransport(t, oauthStaticAuth{}, testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionChatGPT}))

	transport.Client = newRewritingHTTPClient(t, server)
	return transport
}

func newRewritingHTTPClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return &http.Client{
		Transport: httpclient.NewURLRewriteTransport(target, server.Client().Transport, ""),
	}
}
