package launch

import (
	"os"
	"path/filepath"
	"testing"

	"core/server/metadata"
	"core/server/session"
	"core/shared/config"
)

func TestResolveBootstrapPlanUsesSessionWorkspace(t *testing.T) {
	persistenceRoot := t.TempDir()
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	store := createMetadataBackedSession(
		t,
		metadataStore,
		persistenceRoot,
		"/tmp/original-workspace",
		session.ContinuationContext{},
	)

	plan, err := ResolveBootstrapPlan(persistenceRoot, BootstrapRequest{
		WorkspaceRoot: "/tmp/current-dir",
		SessionID:     store.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("resolve bootstrap plan: %v", err)
	}
	if plan.WorkspaceRoot != "/tmp/original-workspace" {
		t.Fatalf("workspace root = %q, want /tmp/original-workspace", plan.WorkspaceRoot)
	}
}

func TestResolveBootstrapPlanRespectsExplicitOverrides(t *testing.T) {
	persistenceRoot := t.TempDir()
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	store := createMetadataBackedSession(
		t,
		metadataStore,
		persistenceRoot,
		"/tmp/original-workspace",
		session.ContinuationContext{},
	)

	plan, err := ResolveBootstrapPlan(persistenceRoot, BootstrapRequest{
		WorkspaceRoot:         "/tmp/override-workspace",
		WorkspaceRootExplicit: true,
		SessionID:             store.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("resolve bootstrap plan: %v", err)
	}
	if plan.WorkspaceRoot != "/tmp/override-workspace" {
		t.Fatalf("workspace root = %q, want /tmp/override-workspace", plan.WorkspaceRoot)
	}
}

func TestResolveBootstrapPlanUsesMetadataSessionLookupByID(t *testing.T) {
	persistenceRoot := t.TempDir()
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	store := createMetadataBackedSession(
		t,
		metadataStore,
		persistenceRoot,
		"/tmp/workspace-b",
		session.ContinuationContext{},
	)

	plan, err := ResolveBootstrapPlan(persistenceRoot, BootstrapRequest{SessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("resolve bootstrap plan: %v", err)
	}
	if plan.WorkspaceRoot != "/tmp/workspace-b" {
		t.Fatalf("workspace root = %q, want /tmp/workspace-b", plan.WorkspaceRoot)
	}
}

func TestResolveSessionCallerTreatsUnownedSessionAsOrdinary(t *testing.T) {
	persistenceRoot := t.TempDir()
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	ordinary := createMetadataBackedSession(
		t,
		metadataStore,
		persistenceRoot,
		"/tmp/ordinary-workspace",
		session.ContinuationContext{},
	)
	ordinaryCaller, err := ResolveSessionCaller(persistenceRoot, ordinary.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolveSessionCaller ordinary: %v", err)
	}
	if ordinaryCaller.Workflow {
		t.Fatal("ordinary session resolved as workflow")
	}
}

func TestResolveBootstrapPlanUsesReboundWorkspaceRootFromMetadataAuthority(t *testing.T) {
	ctx := t.Context()
	oldWorkspace := t.TempDir()
	cfg := loadLaunchConfig(t, oldWorkspace)
	metadataStore, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	binding, err := metadataStore.RegisterWorkspaceBinding(ctx, cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	projectSessionsDir := filepath.Join(filepath.Join(cfg.PersistenceRoot, "projects"), binding.ProjectID, "sessions")
	store := createTestSessionInContainer(t, projectSessionsDir, filepath.Base(projectSessionsDir), cfg.WorkspaceRoot, metadataStore.AuthoritativeSessionStoreOptions()...)
	if err := store.SetName("hello"); err != nil {
		t.Fatalf("SetName: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	newWorkspace := filepath.Join(t.TempDir(), "workspace-moved")
	if err := os.Rename(oldWorkspace, newWorkspace); err != nil {
		t.Fatalf("Rename workspace: %v", err)
	}
	if _, err := metadataStore.RebindWorkspace(ctx, oldWorkspace, newWorkspace); err != nil {
		t.Fatalf("RebindWorkspace: %v", err)
	}

	plan, err := ResolveBootstrapPlan(cfg.PersistenceRoot, BootstrapRequest{SessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("ResolveBootstrapPlan: %v", err)
	}
	canonicalNewWorkspace, err := config.CanonicalWorkspaceRoot(newWorkspace)
	if err != nil {
		t.Fatalf("CanonicalWorkspaceRoot newWorkspace: %v", err)
	}
	if plan.WorkspaceRoot != canonicalNewWorkspace {
		t.Fatalf("workspace root = %q, want %q", plan.WorkspaceRoot, canonicalNewWorkspace)
	}
}
