package worktreeui

import (
	"reflect"
	"testing"

	"core/shared/serverapi"
)

func TestActionsExposeBranchDeletionWhenBranchExists(t *testing.T) {
	got := DeleteActions(serverapi.WorktreeView{BranchName: "feature/a"})
	want := []DeleteAction{DeleteActionCancel, DeleteActionDelete, DeleteActionDeleteBranch}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %+v, want %+v", got, want)
	}
}

func TestClampActionPrefersBranchDeletionWhenAvailable(t *testing.T) {
	target := serverapi.WorktreeView{BranchName: "feature/a"}
	got := ClampDeleteAction(target, DeleteActionCancel, true)
	if got != DeleteActionDeleteBranch {
		t.Fatalf("action = %v, want DeleteActionDeleteBranch", got)
	}
}

func TestClampActionFallsBackToDeleteWhenSelectionInvalid(t *testing.T) {
	target := serverapi.WorktreeView{}
	got := ClampDeleteAction(target, DeleteAction(99), true)
	if got != DeleteActionDelete {
		t.Fatalf("action = %v, want DeleteActionDelete", got)
	}
}

func TestMoveActionClampsToAvailableActions(t *testing.T) {
	target := serverapi.WorktreeView{BranchName: "feature/a"}
	if got := MoveDeleteAction(target, DeleteActionDelete, 1); got != DeleteActionDeleteBranch {
		t.Fatalf("move right = %v, want DeleteActionDeleteBranch", got)
	}
	if got := MoveDeleteAction(target, DeleteActionDelete, -5); got != DeleteActionCancel {
		t.Fatalf("move left = %v, want DeleteActionCancel", got)
	}
}

func TestPreviewLinesDescribeBranchRootWorktreeAndDirtyFiles(t *testing.T) {
	lines := PreviewLines(serverapi.WorktreeView{
		DisplayName:    "feature-a",
		CanonicalRoot:  "/repo-feature",
		BranchName:     "feature/a",
		DirtyFileCount: 2,
	}, DeleteActionDeleteBranch)

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
	lines := PreviewLines(serverapi.WorktreeView{DisplayName: "feature-a", DirtyFileCount: -1}, DeleteActionDelete)
	if got := lines[len(lines)-1].Text; got != "• Dirty file count unavailable; delete will force removal" {
		t.Fatalf("warning = %q", got)
	}
}
