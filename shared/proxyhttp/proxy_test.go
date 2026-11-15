package proxyhttp_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"core/internal/testharness/proxyfixture"
	"core/shared/config"
	"core/shared/proxyhttp"
)

func TestHTTPSProxyVerifiesSeparatePeers(t *testing.T) {
	for _, peers := range []struct {
		proxy, destination string
		succeeds           bool
	}{
		{"proxy.test", "destination.test", true},
		{"destination.test", "destination.test", false},
		{"proxy.test", "proxy.test", false},
	} {
		t.Run(peers.proxy+"/"+peers.destination, func(t *testing.T) {
			fixture := proxyfixture.NewTLS(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}), peers.proxy, peers.destination, true)
			transport, err := proxyhttp.NewTransport(config.Proxy{HTTPS: &fixture.ProxyURL})
			if err != nil {
				t.Fatal(err)
			}
			defer transport.CloseIdleConnections()
			transport.TLSClientConfig = &tls.Config{RootCAs: fixture.Roots}
			transport.DialContext = fixture.DialContext
			response, err := (&http.Client{Transport: transport}).Get(fixture.TargetURL)
			if response != nil {
				response.Body.Close()
			}
			if (err == nil) != peers.succeeds {
				t.Fatalf("peer verification: %v", err)
			}
			if peers.succeeds && (fixture.ProxyCalls.Load() != 1 || fixture.TargetCalls.Load() != 1) {
				t.Fatal("HTTPS request must tunnel through the HTTPS proxy")
			}
		})
	}
}

func TestProxyFailureDoesNotReachDestinationOrLeakCredentials(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	address := "http://synthetic-user:synthetic-secret@" + proxy.Listener.Addr().String()
	transport, err := proxyhttp.NewTransport(config.Proxy{HTTPS: &address})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	var directCalls int
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "destination.test:443" {
			directCalls++
		}
		return dial(ctx, network, addr)
	}
	_, err = (&http.Client{Transport: transport}).Get("https://destination.test/")
	if err == nil || directCalls != 0 || strings.Contains(err.Error(), "synthetic-secret") || strings.Contains(err.Error(), "synthetic-user") {
		t.Fatalf("unsafe failure: direct=%d, error=%v", directCalls, err)
	}
	proxy.Close()
	_, err = (&http.Client{Transport: transport}).Get("https://destination.test/")
	var operation *net.OpError
	if err == nil || !errors.As(err, &operation) || directCalls != 0 || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatalf("unreachable proxy must fail without fallback: %v", err)
	}
}

func TestProxyBypasses(t *testing.T) {
	var targetCalls, proxyCalls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls++
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials reached the destination")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyCalls++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	for _, bypass := range []string{"destination.test", "*"} {
		transport, err := proxyhttp.NewTransport(config.Proxy{HTTP: &proxy.URL, NoProxy: &bypass})
		if err != nil {
			t.Fatal(err)
		}
		transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target.Listener.Addr().String())
		}
		client := &http.Client{Transport: transport}
		for _, address := range []string{target.URL, "http://localhost/", "http://destination.test/"} {
			response, err := client.Get(address)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
		}
		transport.CloseIdleConnections()
	}
	if proxyCalls != 0 || targetCalls != 6 {
		t.Fatalf("bypass observations: proxy=%d target=%d", proxyCalls, targetCalls)
	}
}

func TestInvalidProxyErrorsIdentifySourceWithoutCredentials(t *testing.T) {
	for _, address := range []string{"socks5://synthetic-user:synthetic-secret@proxy.test", "http://synthetic-user:synthetic-secret@%", "http://"} {
		_, err := proxyhttp.NewTransport(config.Proxy{HTTP: &address, Sources: map[string]config.Origin{
			"proxy.http": {Kind: config.SourceEnv, Property: config.PropertyAddress{Key: "proxy.http"}},
		}})
		if err == nil || strings.Contains(err.Error(), "synthetic-user") || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatalf("invalid address must fail safely: %v", err)
		}
		var configurationError *proxyhttp.ConfigurationError
		if !errors.As(err, &configurationError) || configurationError.Key != "proxy.http" || configurationError.Origin.Kind != config.SourceEnv {
			t.Fatal("invalid address must identify its setting and source structurally")
		}
	}
}

func TestHTTPRoutingAndProxyCredentials(t *testing.T) {
	var calls int
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Host != "destination.test" || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Error("proxy must receive destination and URL credentials")
		}
		_, _ = io.WriteString(w, "proxied")
	}))
	defer proxy.Close()
	address := "http://user:pass@" + proxy.Listener.Addr().String()
	transport, err := proxyhttp.NewTransport(config.Proxy{HTTP: &address})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	response, err := client.Get("http://destination.test/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if calls != 1 {
		t.Fatalf("proxy calls = %d", calls)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://destination.test/", nil)
	selected, err := transport.Proxy(request)
	if err != nil || selected != nil {
		t.Fatal("HTTP proxy must not apply to HTTPS")
	}
}

func TestProxyAddressShorthand(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "proxy.test:8080", "[::1]:8080"} {
		transport, err := proxyhttp.NewTransport(config.Proxy{HTTP: &address})
		if err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequest(http.MethodGet, "http://destination.test/", nil)
		selected, err := transport.Proxy(request)
		if err != nil || selected == nil || selected.Scheme != "http" || selected.Host != address {
			t.Fatalf("shorthand %q not normalized: %v, %v", address, selected, err)
		}
	}
}
