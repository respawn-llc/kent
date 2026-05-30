package app

import (
	"testing"

	"builder/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
)

func worktreeDeleteControllerTarget() serverapi.WorktreeView {
	return serverapi.WorktreeView{
		WorktreeID:    "wt-feature",
		DisplayName:   "feature",
		CanonicalRoot: "/wt/feature",
		BranchName:    "feature",
	}
}

func newWorktreeDeleteControllerTestModel(t *testing.T, client *worktreeCommandTestClient) *uiModel {
	t.Helper()
	model := newWorktreeControllerTestModel(t, client, uiWorktreeOverlayPhaseDeleteConfirm)
	model.worktrees.deleteConfirm = uiWorktreeDeleteDialogState{target: worktreeDeleteControllerTarget()}
	model.worktrees.deleteConfirm.clampSelection()
	model.setInputMode(uiInputModeWorktree)
	return model
}

func applyWorktreeDeleteControllerKey(model *uiModel, key tea.KeyMsg) (*uiModel, tea.Cmd) {
	next, cmd := uiInputController{model: model}.handleWorktreeDeleteDialogKey(key)
	return next.(*uiModel), cmd
}

func TestWorktreeDeleteControllerEscapeAndCancelCloseDialog(t *testing.T) {
	model := newWorktreeDeleteControllerTestModel(t, nil)
	updated, cmd := applyWorktreeDeleteControllerKey(model, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("escape should not return command")
	}
	if updated.worktrees.phase != uiWorktreeOverlayPhaseList {
		t.Fatalf("phase after escape = %q, want list", updated.worktrees.phase)
	}

	model = newWorktreeDeleteControllerTestModel(t, nil)
	model.worktrees.deleteConfirm.selectedAction = uiWorktreeDeleteActionCancel
	updated, cmd = applyWorktreeDeleteControllerKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("cancel should not return command")
	}
	if updated.worktrees.phase != uiWorktreeOverlayPhaseList {
		t.Fatalf("phase after cancel = %q, want list", updated.worktrees.phase)
	}
}

func TestWorktreeDeleteControllerCyclesActions(t *testing.T) {
	model := newWorktreeDeleteControllerTestModel(t, nil)
	model.worktrees.deleteConfirm.selectedAction = uiWorktreeDeleteActionCancel

	updated, _ := applyWorktreeDeleteControllerKey(model, tea.KeyMsg{Type: tea.KeyTab})
	if updated.worktrees.deleteConfirm.selectedAction != uiWorktreeDeleteActionDelete {
		t.Fatalf("action after tab = %v, want delete", updated.worktrees.deleteConfirm.selectedAction)
	}
	updated, _ = applyWorktreeDeleteControllerKey(updated, tea.KeyMsg{Type: tea.KeyRight})
	if updated.worktrees.deleteConfirm.selectedAction != uiWorktreeDeleteActionDeleteBranch {
		t.Fatalf("action after right = %v, want delete branch", updated.worktrees.deleteConfirm.selectedAction)
	}
	updated, _ = applyWorktreeDeleteControllerKey(updated, tea.KeyMsg{Type: tea.KeyShiftTab})
	if updated.worktrees.deleteConfirm.selectedAction != uiWorktreeDeleteActionDelete {
		t.Fatalf("action after shift+tab = %v, want delete", updated.worktrees.deleteConfirm.selectedAction)
	}
}

func TestWorktreeDeleteControllerSubmitsDelete(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	model := newWorktreeDeleteControllerTestModel(t, client)
	model.worktrees.deleteConfirm.selectedAction = uiWorktreeDeleteActionDelete

	updated, cmd := applyWorktreeDeleteControllerKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected delete command")
	}
	if !updated.worktrees.deleteConfirm.submitting {
		t.Fatal("expected delete submitting state")
	}
	result := cmd()
	if _, ok := result.(worktreeDeleteDoneMsg); !ok {
		t.Fatalf("command message type = %T, want worktreeDeleteDoneMsg", result)
	}
	if len(client.deleteRequests) != 1 {
		t.Fatalf("delete requests = %d, want 1", len(client.deleteRequests))
	}
	if got := client.deleteRequests[0]; got.WorktreeID != "wt-feature" || got.DeleteBranch {
		t.Fatalf("delete request = %+v, want worktree-only delete", got)
	}
}

func TestWorktreeDeleteControllerSubmitsDeleteBranch(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	model := newWorktreeDeleteControllerTestModel(t, client)
	model.worktrees.deleteConfirm.selectedAction = uiWorktreeDeleteActionDeleteBranch

	_, cmd := applyWorktreeDeleteControllerKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected delete-branch command")
	}
	_ = cmd()
	if len(client.deleteRequests) != 1 {
		t.Fatalf("delete requests = %d, want 1", len(client.deleteRequests))
	}
	if got := client.deleteRequests[0]; got.WorktreeID != "wt-feature" || !got.DeleteBranch {
		t.Fatalf("delete request = %+v, want delete branch", got)
	}
}

func TestWorktreeDeleteControllerUpdateRoutesToDeleteDialog(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	model := newWorktreeDeleteControllerTestModel(t, client)
	model.worktrees.deleteConfirm.selectedAction = uiWorktreeDeleteActionDelete

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected delete command through uiModel.Update")
	}
	if !updated.worktrees.deleteConfirm.submitting {
		t.Fatal("expected submitting state through uiModel.Update")
	}
	result := cmd()
	if _, ok := result.(worktreeDeleteDoneMsg); !ok {
		t.Fatalf("command message type = %T, want worktreeDeleteDoneMsg", result)
	}
	if len(client.deleteRequests) != 1 || client.deleteRequests[0].WorktreeID != "wt-feature" {
		t.Fatalf("delete requests = %+v, want wt-feature delete", client.deleteRequests)
	}
}
