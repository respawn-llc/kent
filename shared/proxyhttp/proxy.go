// Package proxyhttp installs the process's frozen startup proxy policy on Go's
// standard HTTP transport. net/http owns forwarding, CONNECT and authentication.
package proxyhttp

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"core/shared/config"
	"golang.org/x/net/http/httpproxy"
)

// Configure must run at composition time, before any network activity.
func Configure(settings config.Proxy) error {
	transport, err := NewTransport(settings)
	if err != nil {
		return err
	}
	http.DefaultTransport = transport
	return nil
}

func NewTransport(settings config.Proxy) (*http.Transport, error) {
	httpProxy, err := normalize(settings.HTTP, "proxy.http", settings.Sources)
	if err != nil {
		return nil, err
	}
	httpsProxy, err := normalize(settings.HTTPS, "proxy.https", settings.Sources)
	if err != nil {
		return nil, err
	}
	var noProxy string
	if settings.NoProxy != nil {
		noProxy = *settings.NoProxy
	}
	selectProxy := (&httpproxy.Config{HTTPProxy: httpProxy, HTTPSProxy: httpsProxy, NoProxy: noProxy}).ProxyFunc()
	// Match the standard defaults and retain the provider pool/compression
	// policy on the one shared pool rather than eager provider-only copies.
	return &http.Transport{
		Proxy:             func(request *http.Request) (*url.URL, error) { return selectProxy(request.URL) },
		DialContext:       (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2: true, MaxIdleConns: 128, MaxIdleConnsPerHost: 32,
		IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		ExpectContinueTimeout: time.Second, DisableCompression: true,
	}, nil
}

func normalize(raw *string, key string, sources map[string]config.Origin) (string, error) {
	if raw == nil {
		return "", nil
	}
	value := *raw
	parsed, err := url.Parse(value)
	if err == nil && parsed.Host == "" && (parsed.Scheme == "" || parsed.Opaque != "") {
		value = "http://" + value
		parsed, err = url.Parse(value)
	}
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		// url.Parse errors can contain the entire credential-bearing input.
		return "", &ConfigurationError{Key: key, Origin: sources[key]}
	}
	return parsed.String(), nil
}

type ConfigurationError struct {
	Key    string
	Origin config.Origin
}

func (e *ConfigurationError) Error() string {
	source := string(e.Origin.Kind)
	if e.Origin.File != nil {
		source += " " + e.Origin.File.Path
	}
	if e.Origin.Option != nil {
		source += " " + *e.Origin.Option
	}
	return fmt.Sprintf("invalid %s address from %s: expected an HTTP or HTTPS proxy", e.Key, source)
}
