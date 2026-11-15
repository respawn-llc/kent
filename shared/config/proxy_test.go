package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"core/shared/config"
)

func TestServerProxySources(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(name, "")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[proxy]\nhttp='global:80'\nhttps='global:443'\nno_proxy='global.test'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("http_proxy", "process:80")
	t.Setenv("HTTPS_PROXY", "process:443")
	t.Setenv("https_proxy", "lower:443")
	dotenv := map[string]string{"HTTP_PROXY": "dotenv:80", "HTTPS_PROXY": "dotenv:443"}
	got, err := config.ResolveServerProxy(root, func(key string) (string, bool) {
		value, ok := dotenv[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.HTTP == nil || *got.HTTP != "process:80" || got.HTTPS == nil || *got.HTTPS != "process:443" || got.NoProxy == nil || *got.NoProxy != "global.test" {
		t.Fatalf("incorrect independent source resolution")
	}
	if got.Sources["proxy.http"].Kind != config.SourceEnv || *got.Sources["proxy.http"].Option != "http_proxy" {
		t.Fatalf("missing source attribution")
	}
}

func TestServerIgnoresWorkspaceProxy(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".kent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".kent", "config.toml"), []byte("[proxy]\nhttp='workspace:80'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	proxy, err := config.ResolveServerProxy(root, nil)
	if err != nil || proxy.HTTP != nil {
		t.Fatal("workspace must not configure server proxy")
	}
}

func TestClientProxyUsesExactCWD(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(name, "")
	}
	root, selected, parent := t.TempDir(), t.TempDir(), t.TempDir()
	cwd := filepath.Join(parent, "child")
	for _, dir := range []string{root, selected, parent, cwd} {
		if err := os.MkdirAll(filepath.Join(dir, ".kent"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(root, "config.toml"):               "[proxy]\nhttp='global:80'\nhttps='global:443'\nno_proxy='global.test'\n",
		filepath.Join(selected, ".kent", "config.toml"):  "[proxy]\nhttp='selected:80'\n",
		filepath.Join(parent, ".kent", "config.toml"):    "[proxy]\nno_proxy='parent.test'\n",
		filepath.Join(cwd, ".kent", "config.toml"):       "[proxy]\nhttp='shared:80'\nhttps='shared:443'\n",
		filepath.Join(cwd, ".kent", "config.local.toml"): "model=false\nserver_port=false\n[connections.'not a valid id']\nprotocol=false\n[proxy]\nhttp='private:80'\nhttps=''\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(cwd)
	t.Setenv("no_proxy", "env.test")
	connection, err := config.LoadConnectionDiscovery(selected, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := connection.Proxy
	if got.HTTP == nil || *got.HTTP != "private:80" || got.HTTPS == nil || *got.HTTPS != "shared:443" || got.NoProxy == nil || *got.NoProxy != "env.test" {
		t.Fatal("incorrect CWD per-key proxy resolution")
	}
	if got.Sources["proxy.http"].File.Path != filepath.Join(cwd, ".kent", "config.local.toml") {
		t.Fatal("private CWD source missing")
	}
	t.Setenv("no_proxy", "")
	connection, err = config.LoadConnectionDiscovery(selected, config.LoadOptions{ConfigRoot: root})
	if err != nil || connection.Proxy.NoProxy == nil || *connection.Proxy.NoProxy != "global.test" {
		t.Fatal("missing CWD keys must inherit global, not parent or selected workspace")
	}
	t.Setenv("HTTP_PROXY", "upper:80")
	t.Setenv("http_proxy", "lower:80")
	connection, err = config.LoadConnectionDiscovery(selected, config.LoadOptions{ConfigRoot: root})
	if err != nil || connection.Proxy.HTTP == nil || *connection.Proxy.HTTP != "upper:80" {
		t.Fatal("uppercase process proxy must beat lowercase and CWD-private")
	}
}

func TestClientProxyMissingFilesAndConsumedErrors(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	connection, err := config.LoadConnectionDiscovery(cwd, config.LoadOptions{ConfigRoot: root})
	if err != nil || connection.Proxy.HTTP != nil {
		t.Fatal("missing files must not supply a proxy")
	}
	if err := os.Mkdir(filepath.Join(cwd, ".kent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".kent", "config.local.toml"), []byte("[proxy]\nhttp=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadConnectionDiscovery(cwd, config.LoadOptions{ConfigRoot: root}); err == nil {
		t.Fatal("consumed proxy properties must be type checked")
	}
}
