package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"core/shared/proxyhttp"
)

func TestStartupRejectsInvalidEffectiveProxyBeforeDial(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"} {
		t.Setenv(name, "")
	}
	root, cwd, selected := t.TempDir(), t.TempDir(), t.TempDir()
	t.Chdir(cwd)
	if err := os.Mkdir(filepath.Join(cwd, ".kent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".kent", "config.local.toml"), []byte("[proxy]\nhttp='socks5://synthetic-user:synthetic-secret@proxy.test'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{WorkspaceRoot: selected, ConfigRoot: root}
	_, err := resolveRunPromptWorkspaceConfig(opts)
	var configurationError *proxyhttp.ConfigurationError
	if !errors.As(err, &configurationError) || configurationError.Origin.File == nil || configurationError.Origin.File.Layer != "private" {
		t.Fatalf("headless startup must validate CWD proxy before dialing: %v", err)
	}
	_, err = startSessionServer(context.Background(), opts, nil, false)
	if !errors.As(err, &configurationError) {
		t.Fatalf("interactive startup must validate CWD proxy before dialing: %v", err)
	}
}
