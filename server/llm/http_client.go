package llm

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"core/server/httpcompression"
)

// NewHTTPClient returns an HTTP client that shares a tuned transport across
// runtimes so local/LAN model backends can reuse warm connections aggressively.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return newHTTPClient(httpcompression.Transport(nil), timeout)
}

func newHTTPClient(transport http.RoundTripper, timeout time.Duration) *http.Client {
	client := &http.Client{Transport: transport}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return client
}

// NewProviderHTTPClient returns the default model-provider client while
// keeping loopback model servers on the uncompressed local transport.
func NewProviderHTTPClient(baseURL string, timeout time.Duration) *http.Client {
	var transport http.RoundTripper = httpcompression.Transport(nil)
	if isLoopbackHTTPURL(baseURL) {
		transport = http.DefaultTransport
	}
	return newHTTPClient(transport, timeout)
}

func isLoopbackHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	hostname := strings.TrimSpace(parsed.Hostname())
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}
