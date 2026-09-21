package chatcontext

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"core/server/metadata"
	"core/shared/config"

	"github.com/google/uuid"
)

func TestFixedRootWorkspaceResolverUsesMainWorkspacePrivateConfigForManagedWorktree(t *testing.T) {
	root, main, worktree := t.TempDir(), t.TempDir(), t.TempDir()
	binding, err := metadata.RegisterBinding(context.Background(), root, main)
	if err != nil {
		t.Fatal(err)
	}
	store, err := metadata.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertWorktreeRecord(context.Background(), metadata.WorktreeRecord{
		ID: uuid.NewString(), WorkspaceID: binding.WorkspaceID, CanonicalRoot: worktree, Managed: true, GitMetadataJSON: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{main, worktree} {
		if err := os.MkdirAll(filepath.Join(dir, ".kent"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, contents := range map[string]string{
		filepath.Join(main, ".kent", "config.local.toml"):     "model = \"private-main\"\n",
		filepath.Join(worktree, ".kent", "config.toml"):       "thinking_level = \"high\"\n",
		filepath.Join(worktree, ".kent", "config.local.toml"): "this file must not be decoded",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app, err := NewFixedRootWorkspaceResolver(root, main, config.LoadOptions{}).Resolve(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if app.Settings.Model != "private-main" || app.Settings.ThinkingLevel != "high" {
		t.Fatalf("workspace private/shared selection = %s / %s", app.Settings.Model, app.Settings.ThinkingLevel)
	}
	private := app.Source.File(config.FilePrivate)
	if private == nil || private.Path != filepath.Join(binding.CanonicalRoot, ".kent", "config.local.toml") {
		t.Fatalf("private source = %+v", private)
	}
}

func TestFixedRootWorkspaceResolverRetainsStartupOverridesAcrossFreshLoads(t *testing.T) {
	configRoot := t.TempDir()
	workspace := t.TempDir()
	secondary := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(configRoot, "config.toml"),
		[]byte("model = \"file-model\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write startup config: %v", err)
	}
	resolver := NewFixedRootWorkspaceResolver(configRoot, workspace, config.LoadOptions{
		Model: "cli-model",
	})

	if err := os.MkdirAll(filepath.Join(workspace, ".kent"), 0o755); err != nil {
		t.Fatalf("create workspace config dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(workspace, ".kent", "config.toml"),
		[]byte("model_context_window = 80000\ncontext_compaction_threshold_tokens = 60000\npre_submit_compaction_lead_tokens = 10000\n"),
		0o600,
	); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	resolved, err := resolver.Resolve(workspace)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Settings.Model != "cli-model" {
		t.Fatalf("startup overrides were not retained: %+v", resolved.Settings)
	}
	if resolved.Settings.ModelContextWindow != 80_000 {
		t.Fatalf("fresh workspace context window = %d, want 80000", resolved.Settings.ModelContextWindow)
	}
	secondaryResolved, err := resolver.Resolve(secondary)
	if err != nil {
		t.Fatalf("Resolve secondary: %v", err)
	}
	if secondaryResolved.Settings.Model != "file-model" {
		t.Fatalf("startup overrides leaked into secondary workspace: %+v", secondaryResolved.Settings)
	}
}

func TestFixedRootWorkspaceResolverReportsLoadFailure(t *testing.T) {
	configRoot := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(configRoot, "config.toml"), []byte("invalid = ["), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	if _, err := NewFixedRootWorkspaceResolver(configRoot, workspace, config.LoadOptions{}).Resolve(workspace); err == nil {
		t.Fatal("Resolve succeeded with invalid fixed-root config")
	}
}
