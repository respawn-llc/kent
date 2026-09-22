package startupconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/shared/config"
	"core/shared/sessioncontract"
)

func TestResolveWorkspaceRootUsesCWDWhenEmpty(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	got, err := ResolveWorkspaceRoot(" ")
	if err != nil || got != cwd {
		t.Fatalf("workspace = %q, %v", got, err)
	}
}

func TestStartupDiscoveryNeverReadsSessionMetadataOrPrivateSettings(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, config.ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(root, "config.toml"):                                  "server_host = \"server.example\"\nserver_port = 53000\n",
		filepath.Join(root, "db"):                                           "metadata must not be opened",
		filepath.Join(workspace, config.ConfigDirName, "config.toml"):       "server_port = 53001\nmodel = false\n",
		filepath.Join(workspace, config.ConfigDirName, "config.local.toml"): "malformed = [",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	req := Request{WorkspaceRoot: workspace, LoadOptions: config.LoadOptions{ConfigRoot: root}}
	interactive, err := ResolveSessionConfig(req)
	if err != nil {
		t.Fatal(err)
	}
	headless, err := ResolveRunPromptConfig(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range []config.Connection{interactive.Config, headless} {
		if connection.WorkspaceRoot != workspace || connection.PersistenceRoot != root ||
			connection.ServerHost != "server.example" || connection.ServerPort != 53001 {
			t.Fatalf("unexpected initial targeting: %+v", connection)
		}
	}
}

func TestResolveSessionConfigPreservesOnlyLocalPreferences(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(`
model = false
theme = "dark"
[hooks.client]
lifecycle = ["notify", "fixed"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveSessionConfig(Request{
		WorkspaceRoot: workspace,
		LoadOptions:   config.LoadOptions{ConfigRoot: root, Theme: "light", Model: "explicit-request-model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Local.Theme != "light" ||
		!reflect.DeepEqual(resolved.Local.Client.Hooks.LifecycleCommand(), []string{"notify", "fixed"}) {
		t.Fatalf("local preferences: %+v", resolved.Local)
	}
}

func TestInheritedContextGuidanceRequiresStructuredMissingSession(t *testing.T) {
	err := WorkspaceContextSessionError("missing", sessioncontract.ErrSessionNotFound)
	if !errors.Is(err, ErrWorkspaceContextSessionMissing) || !errors.Is(err, sessioncontract.ErrSessionNotFound) {
		t.Fatalf("missing inherited context: %v", err)
	}
	internal := errors.New("internal failure")
	err = WorkspaceContextSessionError("caller", internal)
	if errors.Is(err, ErrWorkspaceContextSessionMissing) || !errors.Is(err, internal) {
		t.Fatalf("unrelated failure mislabeled: %v", err)
	}
}
