package core

import (
	"testing"

	"builder/server/auth"
	serverbootstrap "builder/server/bootstrap"
)

func TestNewWithContextComposesRequiredBundles(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	authSupport, err := serverbootstrap.BuildAuthSupport(auth.NewMemoryStore(auth.EmptyState()), nil, nil)
	if err != nil {
		t.Fatalf("BuildAuthSupport: %v", err)
	}
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolved.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}

	appCore, err := NewWithContext(t.Context(), resolved.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("NewWithContext: %v", err)
	}
	t.Cleanup(func() { _ = appCore.Close() })

	if appCore.bundles == nil {
		t.Fatal("expected bundles")
	}
	if appCore.bundles.Auth == nil || appCore.bundles.Auth.authBootstrap == nil || appCore.bundles.Auth.authStatus == nil {
		t.Fatal("expected auth bundle clients")
	}
	if appCore.bundles.Persistence == nil || appCore.bundles.Persistence.rootLock == nil || appCore.bundles.Persistence.metadataStore == nil || appCore.bundles.Persistence.sessionStores == nil {
		t.Fatal("expected persistence bundle resources")
	}
	if appCore.bundles.Processes == nil || appCore.bundles.Processes.processControls == nil || appCore.bundles.Processes.processOutput == nil || appCore.bundles.Processes.processViews == nil {
		t.Fatal("expected process bundle clients")
	}
	if appCore.bundles.Projects == nil || appCore.bundles.Projects.projectViews == nil {
		t.Fatal("expected project bundle client")
	}
	if appCore.bundles.Prompts == nil || appCore.bundles.Prompts.askViews == nil || appCore.bundles.Prompts.approvalViews == nil || appCore.bundles.Prompts.promptControl == nil || appCore.bundles.Prompts.promptActivity == nil {
		t.Fatal("expected prompt bundle clients")
	}
	if appCore.bundles.Runtime == nil || appCore.bundles.Runtime.background == nil || appCore.bundles.Runtime.backgroundRouter == nil || appCore.bundles.Runtime.runtimeRegistry == nil || appCore.bundles.Runtime.runtimeControls == nil || appCore.bundles.Runtime.sessionRuntime == nil || appCore.bundles.Runtime.sessionActivity == nil {
		t.Fatal("expected runtime bundle services")
	}
	if appCore.bundles.Sessions == nil || appCore.bundles.Sessions.sessionLaunch == nil || appCore.bundles.Sessions.sessionViews == nil || appCore.bundles.Sessions.sessionLifecycle == nil || appCore.bundles.Sessions.runPrompt == nil {
		t.Fatal("expected session bundle clients")
	}
	if appCore.bundles.Updates == nil || appCore.bundles.Updates.updateStatus == nil {
		t.Fatal("expected update status bundle")
	}
	if appCore.bundles.Worktrees == nil || appCore.bundles.Worktrees.worktrees == nil {
		t.Fatal("expected worktree bundle client")
	}
	if appCore.bundles.Workflows == nil || appCore.bundles.Workflows.workflows == nil {
		t.Fatal("expected workflow bundle client")
	}
	if appCore.bundles.Workflows.scheduler == nil || !appCore.bundles.Workflows.scheduler.Started() {
		t.Fatal("expected started workflow scheduler")
	}
	scheduler := appCore.bundles.Workflows.scheduler
	if err := appCore.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !scheduler.Stopped() {
		t.Fatal("expected workflow scheduler to stop during core close")
	}
}
