package llm

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"core/shared/config"
	"core/shared/proxyhttp"
	"github.com/klauspost/compress/zstd"
)

func TestNewHTTPClientPreservesTimeout(t *testing.T) {
	client := NewHTTPClient(7 * time.Second)
	if client.Timeout != 7*time.Second {
		t.Fatalf("Timeout = %v, want %v", client.Timeout, 7*time.Second)
	}

	withoutTimeout := NewHTTPClient(0)
	if withoutTimeout.Timeout != 0 {
		t.Fatalf("Timeout = %v, want 0", withoutTimeout.Timeout)
	}
}

func TestNewProviderHTTPClientKeepsLoopbackTransportUncompressed(t *testing.T) {
	if os.Getenv("LOOPBACK_PROXY_HELPER") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestNewProviderHTTPClientKeepsLoopbackTransportUncompressed$")
		command.Env = append(os.Environ(), "LOOPBACK_PROXY_HELPER=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("loopback child: %v\n%s", err, output)
		}
		return
	}
	if err := proxyhttp.Configure(config.Proxy{}); err != nil {
		t.Fatal(err)
	}
	var acceptEncoding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptEncoding = r.Header.Get("Accept-Encoding")
		_, _ = w.Write([]byte("plain response"))
	}))
	defer server.Close()

	local := NewProviderHTTPClient(server.URL, 0)
	response, err := local.Get(server.URL)
	if err != nil {
		t.Fatalf("GET loopback response: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read loopback response: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Errorf("close loopback response: %v", err)
	}
	if got := string(body); got != "plain response" {
		t.Fatalf("loopback body = %q, want plain response", got)
	}
	if acceptEncoding != "" {
		t.Fatalf("loopback Accept-Encoding = %q, want empty", acceptEncoding)
	}

}

func TestNewProviderHTTPClientNegotiatesAndDecodesCompressedResponse(t *testing.T) {
	payload := []byte("decoded provider response")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "zstd,gzip" {
			t.Fatalf("Accept-Encoding = %q, want zstd,gzip", got)
		}
		encoder, err := zstd.NewWriter(nil)
		if err != nil {
			t.Fatalf("create zstd encoder: %v", err)
		}
		defer encoder.Close()
		w.Header().Set("Content-Encoding", "zstd")
		_, _ = w.Write(encoder.EncodeAll(payload, nil))
	}))
	defer server.Close()

	response, err := NewProviderHTTPClient("https://api.openai.com/v1", 0).Get(server.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("body = %q, want %q", body, payload)
	}
}
