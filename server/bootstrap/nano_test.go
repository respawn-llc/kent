package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"core/shared/config"
)

func TestNanoContextValidationAtConfigurationResolution(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := "[connections.local]\nprotocol = \"responses\"\nendpoint = \"http://localhost:1234/v1\"\n[nano]\nconnection = \"local\"\nmodel = \"unknown-local\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	req := Request{LoadOptions: config.LoadOptions{ConfigRoot: root}}
	if _, err := ResolveConfig(req); err == nil {
		t.Fatal("unknown nano context accepted without override")
	}
	if err := os.WriteFile(path, []byte(body+"model_context_window = 2500\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveConfig(req); err != nil {
		t.Fatalf("valid minimum override: %v", err)
	}
}
