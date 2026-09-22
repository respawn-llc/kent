package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/auth"
	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
)

func TestBuildAuthSupportUsesDefaultIssuerAndEnvClientID(t *testing.T) {
	support, err := BuildAuthSupport(auth.NewMemoryStore(auth.EmptyState()), func(key string) (string, bool) {
		switch key {
		case "KENT_OAUTH_CLIENT_ID":
			return "client-test", true
		case "KENT_OAUTH_ISSUER":
			return "https://attacker.example", true
		default:
			return "", false
		}
	}, func() time.Time {
		return time.Unix(123, 0)
	})
	if err != nil {
		t.Fatalf("build auth support: %v", err)
	}
	if got := support.OAuthOptions.Issuer; got != auth.DefaultOpenAIIssuer {
		t.Fatalf("oauth issuer = %q, want %q", got, auth.DefaultOpenAIIssuer)
	}
	if got := support.OAuthOptions.ClientID; got != "client-test" {
		t.Fatalf("oauth client id = %q", got)
	}
	if _, err := support.AuthManager.Load(context.Background()); err != nil {
		t.Fatalf("load auth manager state: %v", err)
	}
}

func TestBuildShellManagerUsesConfigSettings(t *testing.T) {
	background, err := BuildShellManager(config.App{PersistenceRoot: t.TempDir(), Settings: config.Settings{
		ShellOutputMaxChars:    321,
		BGShellsOutput:         config.BGShellsOutputVerbose,
		MinimumExecToBgSeconds: 1,
		Shell: config.ShellSettings{
			MaxConcurrent:      1,
			PostprocessingMode: config.ShellPostprocessingModeBuiltin,
		},
	}})
	if err != nil {
		t.Fatalf("build runtime support: %v", err)
	}
	t.Cleanup(func() {
		_ = background.Close()
	})
	if background == nil {
		t.Fatal("expected background manager")
	}
	runner, err := postprocess.NewRunner(postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin})
	if err != nil {
		t.Fatal(err)
	}
	request := shelltool.ExecRequest{
		Command: []string{"/bin/sh", "-c", "read value"},
		Workdir: t.TempDir(), KeepStdinOpen: true, YieldTime: time.Millisecond,
		Postprocessor: runner,
	}
	if _, err := background.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	_, err = background.Start(context.Background(), request)
	var limit *shelltool.ConcurrentShellLimitError
	if !errors.As(err, &limit) || limit.Limit != 1 {
		t.Fatalf("configured shell limit not enforced: %v", err)
	}
}

func TestResolveConfigDoesNotCreateLegacyWorkspaceContainer(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	loaded, err := config.Load(workspace, workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	plan, err := ResolveConfig(Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}
	if plan.Config.WorkspaceRoot == "" {
		t.Fatal("expected workspace root")
	}
	if plan.Config.Settings.Model == "" {
		t.Fatal("expected resolved config to carry a non-empty model")
	}
	if plan.Config.Settings.Model != loaded.Settings.Model {
		t.Fatalf("resolved config model = %q, want %q", plan.Config.Settings.Model, loaded.Settings.Model)
	}
}
