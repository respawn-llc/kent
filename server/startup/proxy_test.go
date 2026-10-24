package startup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"core/server/auth"
	"core/server/llm"
	"core/shared/config"
)

func TestStartupProxyRoutesOutbound(t *testing.T) {
	if os.Getenv("PROXY_STARTUP_HELPER") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestStartupProxyRoutesOutbound$")
		command.Env = append(os.Environ(), "PROXY_STARTUP_HELPER=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("startup child: %v\n%s", err, output)
		}
		return
	}
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(name, "")
	}
	var mu sync.Mutex
	var hosts []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hosts = append(hosts, r.Host)
		mu.Unlock()
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("HTTPS_PROXY="+proxy.URL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := StartServeServer(t.Context(), Request{LoadOptions: config.LoadOptions{ConfigRoot: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	_, err = llm.NewProviderHTTPClient("https://provider.test", time.Second).Get("https://provider.test/")
	if err == nil {
		t.Fatal("rejecting proxy must fail the provider request")
	}
	mu.Lock()
	providerProxied := len(hosts) == 1 && hosts[0] == "provider.test:443"
	mu.Unlock()
	if !providerProxied {
		t.Fatal("startup dotenv must configure the provider transport")
	}
	_, err = auth.BeginGrokDeviceFlow(context.Background(), nil)
	if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatalf("auth request must fail safely: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hosts) != 2 || hosts[1] != "auth.x.ai:443" {
		t.Fatalf("auth discovery must use the same startup proxy, hosts=%v", hosts)
	}
	if os.Getenv("HTTPS_PROXY") != "" {
		t.Fatal("dotenv proxy leaked into process environment")
	}
}
