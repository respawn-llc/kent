package worktreedelete

import (
	"reflect"
	"testing"

	"core/shared/serverapi"
)

func TestActionsExposeBranchDeletionWhenBranchExists(t *testing.T) {
	got := Actions(serverapi.WorktreeView{BranchName: "feature/a"})
	want := []Action{ActionCancel, ActionDelete, ActionDeleteBranch}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %+v, want %+v", got, want)
	}
}

func TestClampActionPrefersBranchDeletionWhenAvailable(t *testing.T) {
	target := serverapi.WorktreeView{BranchName: "feature/a"}
	got := ClampAction(target, ActionCancel, true)
	if got != ActionDeleteBranch {
		t.Fatalf("action = %v, want ActionDeleteBranch", got)
	}
}

func TestClampActionFallsBackToDeleteWhenSelectionInvalid(t *testing.T) {
	target := serverapi.WorktreeView{}
	got := ClampAction(target, Action(99), true)
	if got != ActionDelete {
		t.Fatalf("action = %v, want ActionDelete", got)
	}
}

func TestMoveActionClampsToAvailableActions(t *testing.T) {
	target := serverapi.WorktreeView{BranchName: "feature/a"}
	if got := MoveAction(target, ActionDelete, 1); got != ActionDeleteBranch {
		t.Fatalf("move right = %v, want ActionDeleteBranch", got)
	}
	if got := MoveAction(target, ActionDelete, -5); got != ActionCancel {
		t.Fatalf("move left = %v, want ActionCancel", got)
	}
}

func TestPreviewLinesDescribeBranchRootWorktreeAndDirtyFiles(t *testing.T) {
	lines := PreviewLines(serverapi.WorktreeView{
		DisplayName:    "feature-a",
		CanonicalRoot:  "/repo-feature",
		BranchName:     "feature/a",
		DirtyFileCount: 2,
	}, ActionDeleteBranch)

	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}
	want := []string{
		"Will delete:",
		"• Local branch feature/a",
		"• Workspace folder at /repo-feature",
		"• Git worktree feature-a",
		"• Drop 2 modified/untracked files",
	}
	if !reflect.DeepEqual(texts, want) {
		t.Fatalf("preview lines = %+v, want %+v", texts, want)
	}
	if lines[len(lines)-1].Kind != PreviewLineKindWarning {
		t.Fatalf("dirty line kind = %v, want warning", lines[len(lines)-1].Kind)
	}
}

func TestPreviewLinesWarnWhenDirtyCountUnavailable(t *testing.T) {
	lines := PreviewLines(serverapi.WorktreeView{DisplayName: "feature-a", DirtyFileCount: -1}, ActionDelete)
	if got := lines[len(lines)-1].Text; got != "• Dirty file count unavailable; delete will force removal" {
		t.Fatalf("warning = %q", got)
	}
}
