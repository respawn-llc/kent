package app

import (
	"core/shared/clientui"
	"core/shared/serverapi"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWorktreeDeleteCommandOpensDeleteDialogInOverlay(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testLinkedWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree delete"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if updated.worktrees.phase != uiWorktreeOverlayPhaseDeleteConfirm {
		t.Fatalf("phase = %q, want %q", updated.worktrees.phase, uiWorktreeOverlayPhaseDeleteConfirm)
	}
	if updated.worktrees.deleteConfirm.target.WorktreeID != "wt-feature" {
		t.Fatalf("delete target = %+v", updated.worktrees.deleteConfirm.target)
	}
	if len(client.listRequests) == 0 || !client.listRequests[0].IncludeDirtyCount {
		t.Fatalf("expected delete command list request to include dirty count, got %+v", client.listRequests)
	}
	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "Delete feature-a?") {
		t.Fatalf("expected delete dialog render, got %q", plain)
	}
	for _, want := range []string{"Will delete:", "• Workspace folder at /wt/feature-a", "• Git worktree feature-a"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected delete preview line %q, got %q", want, plain)
		}
	}
	if strings.Contains(plain, "Local branch feature/a") {
		t.Fatalf("did not expect branch preview before explicit branch delete action, got %q", plain)
	}
	for _, removed := range []string{"Delete worktree", "Delete feature-a?\n\nDelete feature-a?", "Branch cleanup target", "Branch cleanup needs explicit confirmation", "Esc back | Left/Right choose action | Enter confirm"} {
		if strings.Contains(plain, removed) {
			t.Fatalf("did not expect removed delete dialog copy %q, got %q", removed, plain)
		}
	}
}

func TestWorktreeDeleteDialogBranchPreviewFollowsSelectedAction(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testLinkedWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree delete"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	plain := stripANSIAndTrimRight(updated.View())
	if strings.Contains(plain, "• Local branch feature/a") {
		t.Fatalf("did not expect branch preview for plain delete action, got %q", plain)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRight})
	updated = next.(*uiModel)
	plain = stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "• Local branch feature/a") {
		t.Fatalf("expected branch preview for delete branch action, got %q", plain)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyLeft})
	updated = next.(*uiModel)
	plain = stripANSIAndTrimRight(updated.View())
	if strings.Contains(plain, "• Local branch feature/a") {
		t.Fatalf("did not expect branch preview after returning to plain delete action, got %q", plain)
	}
}

func TestWorktreeDeleteDialogPreviewOmitsBranchWhenActionKeepsBranch(t *testing.T) {
	resp := testLinkedWorktreeListResponse()
	resp.Worktrees[1].BuilderManaged = false
	resp.Worktrees[1].CreatedBranch = false
	client := &worktreeCommandTestClient{listResp: resp}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree delete"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	plain := stripANSIAndTrimRight(updated.View())
	if strings.Contains(plain, "Local branch feature/a") {
		t.Fatalf("did not expect branch preview before explicit branch delete action, got %q", plain)
	}
	if !strings.Contains(plain, "• Workspace folder at /wt/feature-a") || !strings.Contains(plain, "• Git worktree feature-a") {
		t.Fatalf("expected non-branch delete preview, got %q", plain)
	}
}

func TestWorktreeDeleteDialogPreviewWarnsAboutDirtyFiles(t *testing.T) {
	resp := testLinkedWorktreeListResponse()
	resp.Worktrees[1].DirtyFileCount = 2
	client := &worktreeCommandTestClient{listResp: resp}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree delete"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "• Drop 2 modified/untracked files") {
		t.Fatalf("expected dirty file warning, got %q", plain)
	}
}

func TestWorktreeDeleteDialogPreviewWarnsWhenDirtyCountUnavailable(t *testing.T) {
	resp := testLinkedWorktreeListResponse()
	resp.Worktrees[1].DirtyFileCount = -1
	client := &worktreeCommandTestClient{listResp: resp}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree delete"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "• Dirty file count unavailable; delete will force removal") {
		t.Fatalf("expected unknown dirty file warning, got %q", plain)
	}
}

func TestWorktreeSwitchCommandRemainsDirectShortcut(t *testing.T) {
	client := &worktreeCommandTestClient{
		listResp: testLinkedWorktreeListResponse(),
		switchResp: serverapi.WorktreeSwitchResponse{
			Target:   clientui.SessionExecutionTarget{EffectiveWorkdir: "/repo"},
			Worktree: serverapi.WorktreeView{WorktreeID: "wt-main", DisplayName: "main", CanonicalRoot: "/repo", IsMain: true},
		},
	}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree switch main"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if updated.worktrees.open {
		t.Fatal("did not expect overlay for direct switch")
	}
	if len(client.switchRequests) != 1 || client.switchRequests[0].WorktreeID != "wt-main" {
		t.Fatalf("unexpected switch requests: %+v", client.switchRequests)
	}
	plain := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if strings.Contains(plain, "Switched to main") || strings.Contains(plain, "Current workdir:") {
		t.Fatalf("expected no synthetic switch transcript feedback, got %q", plain)
	}
	if status := worktreeStatusLine(updated); !strings.Contains(status, "Switched to main") {
		t.Fatalf("expected switch status, got %q", status)
	}
}

func TestWorktreeSwitchCommandRefreshesStatusLineBranchAfterRuntimeTargetRefresh(t *testing.T) {
	mainRoot := initStatusLineGitRepo(t, "main-branch")
	featureRoot := initStatusLineGitRepo(t, "feature-branch")
	client := &worktreeCommandTestClient{
		listResp: worktreeListResponseForRoots(mainRoot, featureRoot),
		switchResp: serverapi.WorktreeSwitchResponse{
			Target:   clientui.SessionExecutionTarget{WorkspaceRoot: mainRoot, WorktreeRoot: featureRoot, EffectiveWorkdir: featureRoot},
			Worktree: serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature", CanonicalRoot: featureRoot, BranchName: "feature-branch"},
		},
	}
	m := newWorktreeTestModel(t, client, WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: mainRoot}))
	m.status.snapshot.Git = uiStatusGitInfo{Visible: true, Branch: "main-branch"}
	m.input = "/worktree switch feature"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	updated = applyRuntimeTargetRefreshAndDrainStatus(t, updated, client.switchResp.Target)

	status := worktreeStatusLine(updated)
	if !strings.Contains(status, "feature-branch") || strings.Contains(status, "main-branch") {
		t.Fatalf("status line = %q, want refreshed feature branch without stale main branch", status)
	}
}

func TestProjectedSessionMetadataAppliesExecutionTarget(t *testing.T) {
	m := newProjectedTestUIModel(&runtimeControlFakeClient{}, nil, nil, WithUISessionID("session-1"))
	m.statusConfig.WorkspaceRoot = "/repo"

	_ = m.runtimeAdapter().applyProjectedSessionMetadata(clientui.RuntimeSessionView{
		SessionID:       "session-1",
		ExecutionTarget: clientui.SessionExecutionTarget{EffectiveWorkdir: "/wt/feature-a"},
	})

	if got := m.statusConfig.WorkspaceRoot; got != "/wt/feature-a" {
		t.Fatalf("status workspace root = %q, want canonical execution target", got)
	}
}

func TestWorktreeSwitchDoneAppliesTargetAfterMainViewRefresh(t *testing.T) {
	m := newWorktreeTestModel(t, &worktreeCommandTestClient{})
	m.worktrees.switchToken = 7
	m.worktrees.switchPending = true
	m.statusConfig.WorkspaceRoot = "/repo"

	next, _ := m.Update(worktreeSwitchDoneMsg{
		token: 7,
		resp: serverapi.WorktreeSwitchResponse{
			Target:   clientui.SessionExecutionTarget{EffectiveWorkdir: "/wt/feature-a"},
			Worktree: serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature-a", CanonicalRoot: "/wt/feature-a"},
		},
	})
	updated := next.(*uiModel)

	if got := updated.statusConfig.WorkspaceRoot; got != "/repo" {
		t.Fatalf("status workspace root before refresh = %q, want old target", got)
	}
	if updated.worktrees.switchPending {
		t.Fatal("expected switchPending cleared after switch completion")
	}

	next, _ = updated.Update(runtimeMainViewRefreshedMsg{
		token: updated.runtimeMainViewToken,
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID:       "session-1",
			ExecutionTarget: clientui.SessionExecutionTarget{EffectiveWorkdir: "/wt/feature-a"},
		}},
	})
	updated = next.(*uiModel)
	if got := updated.statusConfig.WorkspaceRoot; got != "/wt/feature-a" {
		t.Fatalf("status workspace root after refresh = %q, want switched worktree root", got)
	}
}

func TestWorktreeOverlayStaysOpenWhileSwitchIsPending(t *testing.T) {
	client := &worktreeCommandTestClient{switchResp: serverapi.WorktreeSwitchResponse{
		Target:   clientui.SessionExecutionTarget{EffectiveWorkdir: "/wt/feature-a"},
		Worktree: serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature-a", CanonicalRoot: "/wt/feature-a"},
	}}
	m := newWorktreeTestModel(t, client)
	m.worktrees.open = true
	m.worktrees.phase = uiWorktreeOverlayPhaseList
	m.worktrees.entries = []serverapi.WorktreeView{{
		WorktreeID:    "wt-feature",
		DisplayName:   "feature-a",
		CanonicalRoot: "/wt/feature-a",
	}}
	m.worktrees.selection = 1
	m.setInputMode(uiInputModeWorktree)

	next, switchCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if !updated.worktrees.switchPending {
		t.Fatal("expected switchPending set after switch command")
	}

	next, closeCmd := updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated = next.(*uiModel)
	if !updated.worktrees.open {
		t.Fatal("expected overlay to stay open while switch pending")
	}
	if closeCmd != nil {
		if msgs := collectCmdMessages(t, closeCmd); len(msgs) != 0 {
			t.Fatalf("expected no close command output while switch pending, got %+v", msgs)
		}
	}

	for _, msg := range collectCmdMessages(t, switchCmd) {
		next, cmd := updated.Update(msg)
		updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	}
	if got := updated.statusConfig.WorkspaceRoot; got == "/wt/feature-a" {
		t.Fatalf("status workspace root patched from mutation response before refresh: %q", got)
	}
	next, _ = updated.Update(runtimeMainViewRefreshedMsg{
		token: updated.runtimeMainViewToken,
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID:       "session-1",
			ExecutionTarget: clientui.SessionExecutionTarget{EffectiveWorkdir: "/wt/feature-a"},
		}},
	})
	updated = next.(*uiModel)
	if got := updated.statusConfig.WorkspaceRoot; got != "/wt/feature-a" {
		t.Fatalf("status workspace root after refresh = %q, want switched worktree root", got)
	}
}

func TestWorktreeCreateDoneAppliesTargetAfterMainViewRefresh(t *testing.T) {
	m := newWorktreeTestModel(t, &worktreeCommandTestClient{})
	m.worktrees.mutationToken = 8
	m.statusConfig.WorkspaceRoot = "/repo"

	next, _ := m.Update(worktreeCreateDoneMsg{
		token: 8,
		resp: serverapi.WorktreeCreateResponse{
			Target:         clientui.SessionExecutionTarget{EffectiveWorkdir: "/wt/new-feature"},
			Worktree:       serverapi.WorktreeView{WorktreeID: "wt-new", DisplayName: "new-feature", CanonicalRoot: "/wt/new-feature", BranchName: "feature/new"},
			CreatedBranch:  true,
			SetupScheduled: true,
		},
	})
	updated := next.(*uiModel)

	if got := updated.statusConfig.WorkspaceRoot; got != "/repo" {
		t.Fatalf("status workspace root before refresh = %q, want old target", got)
	}
	next, _ = updated.Update(runtimeMainViewRefreshedMsg{
		token: updated.runtimeMainViewToken,
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID:       "session-1",
			ExecutionTarget: clientui.SessionExecutionTarget{EffectiveWorkdir: "/wt/new-feature"},
		}},
	})
	updated = next.(*uiModel)
	if got := updated.statusConfig.WorkspaceRoot; got != "/wt/new-feature" {
		t.Fatalf("status workspace root after refresh = %q, want created worktree root", got)
	}
	plain := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	for _, unwanted := range []string{"Created new-feature at", "Created branch:", "Setup script started"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("expected no synthetic create transcript feedback %q, got %q", unwanted, plain)
		}
	}
}

func TestWorktreeDeleteDoneAppliesTargetAfterMainViewRefresh(t *testing.T) {
	m := newWorktreeTestModel(t, &worktreeCommandTestClient{})
	m.worktrees.mutationToken = 9
	m.statusConfig.WorkspaceRoot = "/wt/feature-a"

	next, _ := m.Update(worktreeDeleteDoneMsg{
		token: 9,
		resp: serverapi.WorktreeDeleteResponse{
			Target:   clientui.SessionExecutionTarget{EffectiveWorkdir: "/repo"},
			Worktree: serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature-a", CanonicalRoot: "/wt/feature-a"},
		},
	})
	updated := next.(*uiModel)

	if got := updated.statusConfig.WorkspaceRoot; got != "/wt/feature-a" {
		t.Fatalf("status workspace root before refresh = %q, want old target", got)
	}
	next, _ = updated.Update(runtimeMainViewRefreshedMsg{
		token: updated.runtimeMainViewToken,
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID:       "session-1",
			ExecutionTarget: clientui.SessionExecutionTarget{EffectiveWorkdir: "/repo"},
		}},
	})
	updated = next.(*uiModel)
	if got := updated.statusConfig.WorkspaceRoot; got != "/repo" {
		t.Fatalf("status workspace root after refresh = %q, want main workspace root", got)
	}
}

func TestWorktreeDeleteDoneShowsBranchCleanupOutcome(t *testing.T) {
	m := newWorktreeTestModel(t, &worktreeCommandTestClient{})
	m.worktrees.mutationToken = 9

	next, _ := m.Update(worktreeDeleteDoneMsg{
		token: 9,
		resp: serverapi.WorktreeDeleteResponse{
			Target:               clientui.SessionExecutionTarget{EffectiveWorkdir: "/repo"},
			Worktree:             serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature-a", CanonicalRoot: "/wt/feature-a"},
			BranchCleanupMessage: "Kept branch feature-a: Kent cannot prove this worktree created it",
		},
	})
	updated := next.(*uiModel)

	if !strings.Contains(updated.transientStatus, "Deleted worktree feature-a") || !strings.Contains(updated.transientStatus, "Kept branch feature-a") {
		t.Fatalf("transient status = %q, want delete and branch cleanup outcome", updated.transientStatus)
	}
}

func TestWorktreeOverlayEnterSwitchesSelectedItemAndCloses(t *testing.T) {
	resp := testMainWorktreeListResponse()
	resp.Worktrees = append(resp.Worktrees, serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature-a", CanonicalRoot: "/wt/feature-a", BranchName: "feature/a"})
	client := &worktreeCommandTestClient{
		listResp: resp,
		switchResp: serverapi.WorktreeSwitchResponse{
			Target:   clientui.SessionExecutionTarget{EffectiveWorkdir: "/wt/feature-a"},
			Worktree: serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature-a", CanonicalRoot: "/wt/feature-a", BranchName: "feature/a"},
		},
	}
	m := newWorktreeTestModel(t, client)
	m.input = "/wt"
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if updated.worktrees.open {
		t.Fatal("expected overlay closed after switch")
	}
	if len(client.switchRequests) != 1 || client.switchRequests[0].WorktreeID != "wt-feature" {
		t.Fatalf("unexpected switch request: %+v", client.switchRequests)
	}
}

func TestWorktreeCreateDialogSubmitsAndClosesOnSuccess(t *testing.T) {
	mainRoot := initStatusLineGitRepo(t, "main-branch")
	featureRoot := initStatusLineGitRepo(t, "created-branch")
	client := &worktreeCommandTestClient{
		listResp: worktreeListResponseForRoots(mainRoot, ""),
		createResp: serverapi.WorktreeCreateResponse{
			Target:        clientui.SessionExecutionTarget{WorkspaceRoot: mainRoot, WorktreeRoot: featureRoot, EffectiveWorkdir: featureRoot},
			Worktree:      serverapi.WorktreeView{WorktreeID: "wt-new", DisplayName: "feature-branch", CanonicalRoot: featureRoot, BranchName: "feature/branch"},
			CreatedBranch: true,
		},
	}
	m := newWorktreeTestModel(t, client, WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: mainRoot}))
	m.status.snapshot.Git = uiStatusGitInfo{Visible: true, Branch: "main-branch"}
	m.input = "/wt create"
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	updated.worktrees.create.baseRef.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("HEAD"))
	updated.worktrees.create.branchTarget.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("feature/branch"))
	updated.worktrees.create.focus = uiWorktreeCreateFieldActions
	updated.worktrees.create.action = uiWorktreeCreateActionCreate
	updated.worktrees.create.syncFocus()

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if updated.worktrees.open {
		t.Fatal("expected overlay closed after create")
	}
	if len(client.createRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(client.createRequests))
	}
	if len(client.resolveRequests) == 0 || client.resolveRequests[len(client.resolveRequests)-1].Target != "feature/branch" {
		t.Fatalf("expected resolve request for branch target, got %+v", client.resolveRequests)
	}
	if got := client.createRequests[0]; got.BaseRef != "HEAD" || !got.CreateBranch || got.BranchName != "feature/branch" || got.RootPath != "" {
		t.Fatalf("unexpected create request: %+v", got)
	}
	updated = applyRuntimeTargetRefreshAndDrainStatus(t, updated, client.createResp.Target)
	status := worktreeStatusLine(updated)
	if !strings.Contains(status, "created-branch") || strings.Contains(status, "main-branch") {
		t.Fatalf("status line = %q, want refreshed created branch without stale main branch", status)
	}
}

func TestWorktreeCreateDialogDetachedRefResolutionCreatesWithoutBranch(t *testing.T) {
	client := &worktreeCommandTestClient{
		listResp:    testMainWorktreeListResponse(),
		resolveResp: serverapi.WorktreeCreateTargetResolveResponse{Resolution: serverapi.WorktreeCreateTargetResolution{Input: "HEAD~1", Kind: serverapi.WorktreeCreateTargetResolutionKindDetachedRef}},
		createResp:  serverapi.WorktreeCreateResponse{Target: clientui.SessionExecutionTarget{EffectiveWorkdir: "/wt/head-1"}, Worktree: serverapi.WorktreeView{WorktreeID: "wt-detached", DisplayName: "head-1", CanonicalRoot: "/wt/head-1", Detached: true}},
	}
	m := newWorktreeTestModel(t, client)
	m.input = "/wt create"
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	updated.worktrees.create.branchTarget.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("HEAD~1"))
	updated.worktrees.create.focus = uiWorktreeCreateFieldActions
	updated.worktrees.create.action = uiWorktreeCreateActionCreate
	updated.worktrees.create.syncFocus()

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if len(client.createRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(client.createRequests))
	}
	if got := client.createRequests[0]; got.CreateBranch || got.BranchName != "" || got.BaseRef != "HEAD~1" {
		t.Fatalf("unexpected detached create request: %+v", got)
	}
}

func TestWorktreeDeleteDialogStaysOpenAfterSuccess(t *testing.T) {
	mainRoot := initStatusLineGitRepo(t, "main-branch")
	featureRoot := initStatusLineGitRepo(t, "feature-branch")
	client := &worktreeCommandTestClient{
		listResp:   worktreeListResponseForRoots(mainRoot, featureRoot),
		deleteResp: serverapi.WorktreeDeleteResponse{Target: clientui.SessionExecutionTarget{WorkspaceRoot: mainRoot, EffectiveWorkdir: mainRoot}, Worktree: serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature", CanonicalRoot: featureRoot}},
	}
	m := newWorktreeTestModel(t, client, WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: featureRoot}))
	m.status.snapshot.Git = uiStatusGitInfo{Visible: true, Branch: "feature-branch"}
	m.input = "/wt delete"
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	client.listResp = worktreeListResponseForRoots(mainRoot, "")

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if !updated.worktrees.open {
		t.Fatal("expected overlay to stay open after delete")
	}
	if updated.worktrees.phase != uiWorktreeOverlayPhaseList {
		t.Fatalf("phase = %q, want list", updated.worktrees.phase)
	}
	if len(client.deleteRequests) != 1 || client.deleteRequests[0].DeleteBranch {
		t.Fatalf("unexpected delete request: %+v", client.deleteRequests)
	}
	updated = applyRuntimeTargetRefreshAndDrainStatus(t, updated, client.deleteResp.Target)
	status := worktreeStatusLine(updated)
	if !strings.Contains(status, "main-branch") || strings.Contains(status, "feature-branch") {
		t.Fatalf("status line = %q, want refreshed main branch without stale feature branch", status)
	}
}

func TestWorktreeDeleteBranchHotkeyPrefersBranchDeleteAction(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testLinkedWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)
	m.input = "/wt"
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if updated.worktrees.phase != uiWorktreeOverlayPhaseDeleteConfirm {
		t.Fatalf("phase = %q, want delete_confirm", updated.worktrees.phase)
	}
	if len(client.listRequests) < 2 || !client.listRequests[1].IncludeDirtyCount {
		t.Fatalf("expected delete hotkey refresh to include dirty count, got %+v", client.listRequests)
	}
	if updated.worktrees.deleteConfirm.selectedAction != uiWorktreeDeleteActionDeleteBranch {
		t.Fatalf("selected action = %v, want delete branch", updated.worktrees.deleteConfirm.selectedAction)
	}
}
