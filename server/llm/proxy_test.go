package llm

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"core/server/httpcompression"
	"core/shared/config"
	"core/shared/proxyhttp"
	"core/shared/textutil"
)

func TestGenerateReturnsForwardProxyRejection(t *testing.T) {
	var rejected, direct atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rejected.Add(1)
		if r.Method != http.MethodPost || r.URL.Host != "provider.test" || r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected proxy request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Proxy-Authorization") != "Basic c3ludGhldGljLXVzZXI6c3ludGhldGljLXNlY3JldA==" {
			t.Error("proxy credentials missing from the proxy hop")
		}
		w.Header().Set("Proxy-Authenticate", `Basic realm="fixture"`)
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	address := "http://synthetic-user:synthetic-secret@" + proxy.Listener.Addr().String()
	base, err := proxyhttp.NewTransport(config.Proxy{HTTP: &address})
	if err != nil {
		t.Fatal(err)
	}
	defer base.CloseIdleConnections()
	dial := base.DialContext
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if host == "provider.test" {
			direct.Add(1)
		}
		return dial(ctx, network, address)
	}
	transport := newTestHTTPTransport(t, anonymousAuth{}, testConnectionRegistration(t, config.ProviderConnection{
		Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://provider.test/v1"),
	}))
	transport.Client = httpcompression.NewClient(&http.Client{Transport: base})
	transport.ContextWindowTokens = 168000
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, err = transport.Generate(ctx, ResponsesRequest{
		Model: "fixture-model", SessionID: textutil.Value("proxy-fixture"), ToolChoiceMode: ToolChoiceModeAutomatic,
	}, StreamCallbacks{})
	var failure *ProviderAPIError
	if ctx.Err() != nil || !errors.As(err, &failure) || failure.StatusCode == nil || *failure.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("proxy rejection must return its HTTP status before request timeout: %v", err)
	}
	if strings.Contains(err.Error(), "synthetic-user") || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("proxy rejection disclosed credentials")
	}
	if rejected.Load() != 1 || direct.Load() != 0 {
		t.Fatalf("proxy rejection observations: rejected=%d direct=%d", rejected.Load(), direct.Load())
	}
}
