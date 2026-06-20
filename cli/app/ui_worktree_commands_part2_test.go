package app

import (
	"core/shared/clientui"
	"core/shared/serverapi"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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
