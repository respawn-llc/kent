package startupconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/server/metadata"
	"core/server/session"
	"core/shared/config"
	"core/shared/sessioncontract"
)

func TestResolveWorkspaceRootUsesCWDWhenEmpty(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	got, err := ResolveWorkspaceRoot(" ")
	if err != nil {
		t.Fatalf("ResolveWorkspaceRoot: %v", err)
	}
	if got != cwd {
		t.Fatalf("workspace root = %q, want %q", got, cwd)
	}
}

func TestFreshStartupDiscoversConnectionWithoutMetadataOrPrivateSettings(t *testing.T) {
	for _, headless := range []bool{false, true} {
		t.Run(map[bool]string{false: "tui", true: "headless"}[headless], func(t *testing.T) {
			root, workspace := t.TempDir(), t.TempDir()
			if err := os.MkdirAll(filepath.Join(workspace, config.ConfigDirName), 0o755); err != nil {
				t.Fatal(err)
			}
			for path, body := range map[string]string{
				filepath.Join(root, "config.toml"):                                  "server_host = \"server.example\"\nserver_port = 53000\n",
				filepath.Join(root, "db"):                                           "metadata access must not be needed",
				filepath.Join(workspace, config.ConfigDirName, "config.toml"):       "server_port = 53001\n",
				filepath.Join(workspace, config.ConfigDirName, "config.local.toml"): "malformed = [",
			} {
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			req := Request{WorkspaceRoot: workspace, LoadOptions: config.LoadOptions{ConfigRoot: root}}
			resolve := func() config.App {
				t.Helper()
				if headless {
					result, err := ResolveRunPromptConfig(req)
					if err != nil {
						t.Fatal(err)
					}
					return result.Config
				}
				result, err := ResolveSessionConfig(req)
				if err != nil {
					t.Fatal(err)
				}
				return result.Config
			}
			app := resolve()
			if app.Settings.ServerHost != "server.example" || app.Settings.ServerPort != 53001 {
				t.Fatalf("connection settings = %s:%d", app.Settings.ServerHost, app.Settings.ServerPort)
			}
			t.Setenv("KENT_SERVER_PORT", "53002")
			if app := resolve(); app.Settings.ServerPort != 53002 {
				t.Fatalf("environment port = %d", app.Settings.ServerPort)
			}
		})
	}
}

func TestResolveRunPromptConfigWrapsMissingImplicitWorkspaceContextSession(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	_, err := ResolveRunPromptConfig(Request{
		WorkspaceRoot:             workspace,
		WorkspaceContextSessionID: "missing-context-session",
		LoadOptions:               config.LoadOptions{ConfigRoot: t.TempDir()},
	})
	if err == nil {
		t.Fatal("expected missing context session error")
	}
	if !errors.Is(err, sessioncontract.ErrSessionNotFound) {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
	if !errors.Is(err, ErrWorkspaceContextSessionMissing) {
		t.Fatalf("error = %v, want workspace context guidance", err)
	}
}

func TestResolveRunPromptConfigKeepsExplicitSessionLookupStrict(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	_, err := ResolveRunPromptConfig(Request{
		WorkspaceRoot: workspace,
		SessionID:     "missing-explicit-session",
		LoadOptions:   config.LoadOptions{ConfigRoot: t.TempDir()},
	})
	if !errors.Is(err, sessioncontract.ErrSessionNotFound) {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
	if errors.Is(err, ErrWorkspaceContextSessionMissing) {
		t.Fatalf("explicit session error should not be rewritten as workspace context guidance: %v", err)
	}
}

func TestResolveSessionConfigAppliesLoadOptions(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	resolved, err := ResolveSessionConfig(Request{
		WorkspaceRoot: workspace,
		LoadOptions: config.LoadOptions{
			Model:         "gpt-5",
			ThinkingLevel: "high",
		},
	})
	if err != nil {
		t.Fatalf("ResolveSessionConfig: %v", err)
	}
	cfg := resolved.Config
	if cfg.Settings.Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", cfg.Settings.Model)
	}
	if cfg.Settings.ThinkingLevel != "high" {
		t.Fatalf("thinking level = %q, want high", cfg.Settings.ThinkingLevel)
	}
}

func TestResolveRunPromptConfigThreadsPersistenceRoot(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()

	res, err := ResolveRunPromptConfig(Request{
		WorkspaceRoot: workspace,
		LoadOptions:   config.LoadOptions{ConfigRoot: root},
	})
	if err != nil {
		t.Fatalf("ResolveRunPromptConfig: %v", err)
	}
	if res.Config.PersistenceRoot != root {
		t.Fatalf("persistence root = %q, want %q", res.Config.PersistenceRoot, root)
	}
}

func TestResolveRunPromptConfigClassifiesValidatedProvenance(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	meta, err := metadata.Open(root)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	binding, err := meta.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := filepath.Join(root, "projects", binding.ProjectID, "sessions")
	store, err := session.Create(containerDir, filepath.Base(containerDir), workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}

	kentSession, err := ResolveRunPromptConfig(Request{
		WorkspaceRoot:             workspace,
		WorkspaceRootExplicit:     true,
		WorkspaceContextSessionID: store.Meta().SessionID,
		LoadOptions:               config.LoadOptions{ConfigRoot: root},
	})
	if err != nil {
		t.Fatalf("ResolveRunPromptConfig Kent session: %v", err)
	}
	if kentSession.CallerContext.Kind != CallerKindKentSession {
		t.Fatalf("caller kind = %q, want Kent session", kentSession.CallerContext.Kind)
	}

	human, err := ResolveRunPromptConfig(Request{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
		LoadOptions:           config.LoadOptions{ConfigRoot: root},
	})
	if err != nil {
		t.Fatalf("ResolveRunPromptConfig human: %v", err)
	}
	if human.CallerContext.Kind != CallerKindHuman {
		t.Fatalf("caller kind = %q, want human", human.CallerContext.Kind)
	}
}

func TestResolveSessionConfigThreadsPersistenceRoot(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()

	resolved, err := ResolveSessionConfig(Request{
		WorkspaceRoot: workspace,
		LoadOptions:   config.LoadOptions{ConfigRoot: root},
	})
	if err != nil {
		t.Fatalf("ResolveSessionConfig: %v", err)
	}
	if resolved.Config.PersistenceRoot != root {
		t.Fatalf("persistence root = %q, want %q", resolved.Config.PersistenceRoot, root)
	}
}

func TestResolveSessionConfigCarriesClientSettingsFromResolvedSnapshot(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(configPath, []byte("[hooks.client]\nlifecycle = [\"notify\", \"fixed\"]\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolved, err := ResolveSessionConfig(Request{
		WorkspaceRoot: workspace,
		LoadOptions:   config.LoadOptions{ConfigRoot: root},
	})
	if err != nil {
		t.Fatalf("ResolveSessionConfig: %v", err)
	}
	if got := resolved.Client.Hooks.LifecycleCommand(); !reflect.DeepEqual(got, []string{"notify", "fixed"}) {
		t.Fatalf("lifecycle command = %#v", got)
	}
}
