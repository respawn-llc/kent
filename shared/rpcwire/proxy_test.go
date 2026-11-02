package rpcwire

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"core/internal/testharness/proxyfixture"
	"core/shared/config"
	"core/shared/proxyhttp"
)

func proxyProcess(t *testing.T) bool {
	t.Helper()
	if os.Getenv("RPC_PROXY_HELPER") == "1" {
		return true
	}
	command := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	command.Env = append(os.Environ(), "RPC_PROXY_HELPER=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("proxy child: %v\n%s", err, output)
	}
	return false
}

func echoProxyHandler() http.Handler {
	return NewWebSocketTransport().Handler(func(ctx context.Context, conn Conn) {
		select {
		case event := <-conn.Events():
			if event.Err == nil {
				_ = conn.Send(ctx, event.Frame)
			}
		case <-ctx.Done():
		}
	})
}

func proxyRoundTrip(t *testing.T, endpoint Endpoint) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, err := NewWebSocketTransport().Dial(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Send(ctx, Frame{Kind: FrameBinary, Payload: []byte("proxy roundtrip")}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-conn.Events():
		if event.Err != nil || string(event.Frame.Payload) != "proxy roundtrip" || event.Frame.Kind != FrameBinary {
			t.Fatalf("unexpected proxy echo: %+v", event)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestWebSocketHTTPProxy(t *testing.T) {
	if !proxyProcess(t) {
		return
	}
	target := httptest.NewServer(echoProxyHandler())
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	var calls atomic.Int32
	forward := &httputil.ReverseProxy{Director: func(r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" || r.URL.Host != "destination.test" {
			t.Error("proxy hop missing destination or credentials")
		}
		r.Header.Del("Proxy-Authorization")
		r.URL.Host = targetURL.Host
	}, Transport: http.DefaultTransport}
	proxy := httptest.NewServer(forward)
	defer proxy.Close()
	address := "http://user:pass@" + proxy.Listener.Addr().String()
	if err := proxyhttp.Configure(config.Proxy{HTTP: &address}); err != nil {
		t.Fatal(err)
	}
	endpoint, _ := ParseWebSocketEndpoint("ws://destination.test/")
	proxyRoundTrip(t, endpoint)
	if calls.Load() != 1 {
		t.Fatal("WebSocket must use the HTTP proxy")
	}
}

func TestWebSocketProxyTLS(t *testing.T) {
	if !proxyProcess(t) {
		return
	}
	for _, explicit := range []bool{false, true} {
		for _, peers := range []struct {
			proxy, destination string
			succeeds           bool
		}{
			{"proxy.test", "destination.test", true},
			{"destination.test", "destination.test", false},
			{"proxy.test", "proxy.test", false},
		} {
			t.Run(peers.proxy+"/"+peers.destination+"/"+map[bool]string{false: "inferred", true: "override"}[explicit], func(t *testing.T) {
				fixture := proxyfixture.NewTLS(t, echoProxyHandler(), peers.proxy, peers.destination, true)
				base, err := proxyhttp.NewTransport(config.Proxy{HTTPS: &fixture.ProxyURL})
				if err != nil {
					t.Fatal(err)
				}
				defer base.CloseIdleConnections()
				base.TLSClientConfig = &tls.Config{RootCAs: fixture.Roots}
				base.DialContext = fixture.DialContext
				http.DefaultTransport = base
				endpoint, _ := ParseWebSocketEndpoint("wss://destination.test/")
				if explicit {
					endpoint.TLSConfig = &tls.Config{RootCAs: fixture.Roots, ServerName: "destination.test",
						VerifyConnection: func(state tls.ConnectionState) error {
							if state.ServerName != "destination.test" {
								t.Error("destination TLS callback used on proxy")
							}
							return nil
						},
					}
				}
				if peers.succeeds {
					proxyRoundTrip(t, endpoint)
					if fixture.ProxyCalls.Load() != 1 || fixture.TargetCalls.Load() != 1 {
						t.Fatal("wss must use CONNECT")
					}
				} else {
					conn, err := NewWebSocketTransport().Dial(t.Context(), endpoint)
					if conn != nil {
						conn.Close()
					}
					if err == nil {
						t.Fatal("wrong peer certificate accepted")
					}
				}
			})
		}
	}
}

func TestWebSocketProxyFailure(t *testing.T) {
	if !proxyProcess(t) {
		return
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	address := "http://synthetic-user:synthetic-secret@" + proxy.Listener.Addr().String()
	if err := proxyhttp.Configure(config.Proxy{HTTP: &address, HTTPS: &address}); err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport.(*http.Transport)
	var directCalls atomic.Int32
	dial := base.DialContext
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if host == "destination.test" {
			directCalls.Add(1)
		}
		return dial(ctx, network, address)
	}
	for _, unreachable := range []bool{false, true} {
		if unreachable {
			proxy.Close()
		}
		for _, scheme := range []string{"ws", "wss"} {
			endpoint, _ := ParseWebSocketEndpoint(scheme + "://destination.test/")
			conn, err := NewWebSocketTransport().Dial(t.Context(), endpoint)
			if conn != nil {
				conn.Close()
			}
			if err == nil || strings.Contains(err.Error(), "synthetic-secret") || strings.Contains(err.Error(), "synthetic-user") {
				t.Fatalf("proxy failure must fail safely: %v", err)
			}
		}
	}
	if directCalls.Load() != 0 {
		t.Fatal("failed proxy must not fall back to destination dialing")
	}
}

func TestWebSocketWSSViaHTTPProxy(t *testing.T) {
	if !proxyProcess(t) {
		return
	}
	fixture := proxyfixture.NewTLS(t, echoProxyHandler(), "proxy.test", "destination.test", false)
	base, err := proxyhttp.NewTransport(config.Proxy{HTTPS: &fixture.ProxyURL})
	if err != nil {
		t.Fatal(err)
	}
	defer base.CloseIdleConnections()
	base.DialContext = fixture.DialContext
	http.DefaultTransport = base
	endpoint, _ := ParseWebSocketEndpoint("wss://destination.test/")
	endpoint.TLSConfig = &tls.Config{RootCAs: fixture.Roots}
	proxyRoundTrip(t, endpoint)
	if fixture.ProxyCalls.Load() != 1 {
		t.Fatal("wss must tunnel through HTTP proxy")
	}
}
