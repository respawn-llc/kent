package worktreeselection

import (
	"testing"

	"core/shared/serverapi"
)

func TestClampUsesCreateRowPlusEntries(t *testing.T) {
	entries := []serverapi.WorktreeView{{WorktreeID: "wt-1"}}
	if got := Clamp(-1, entries); got != 0 {
		t.Fatalf("negative selection = %d, want 0", got)
	}
	if got := Clamp(9, entries); got != 1 {
		t.Fatalf("overflow selection = %d, want last row", got)
	}
}

func TestSelectedWorktreeMapsSelectionAfterCreateRow(t *testing.T) {
	entries := []serverapi.WorktreeView{{WorktreeID: "wt-1"}, {WorktreeID: "wt-2"}}
	item, ok := SelectedWorktree(entries, 2)
	if !ok || item.WorktreeID != "wt-2" {
		t.Fatalf("selected = %+v ok=%v, want wt-2", item, ok)
	}
	if _, ok := SelectedWorktree(entries, 0); ok {
		t.Fatal("create row should not select worktree")
	}
}

func TestSelectedIDReturnsCreateRowForCreateSelection(t *testing.T) {
	if got := SelectedID([]serverapi.WorktreeView{{WorktreeID: "wt-1"}}, 0); got != CreateRowID {
		t.Fatalf("selected id = %q, want create row id", got)
	}
}

func TestRestoreFindsRecordedWorktreeID(t *testing.T) {
	entries := []serverapi.WorktreeView{{WorktreeID: "wt-1"}, {WorktreeID: "wt-2"}}
	if got := Restore(entries, 0, " wt-2 "); got != 2 {
		t.Fatalf("selection = %d, want 2", got)
	}
}

func TestRestoreFallsBackToClampedCurrentSelection(t *testing.T) {
	entries := []serverapi.WorktreeView{{WorktreeID: "wt-1"}}
	if got := Restore(entries, 7, "missing"); got != 1 {
		t.Fatalf("selection = %d, want clamped current", got)
	}
}

func TestRestoreUsesCreateRowWhenRecordedWorktreeDisappearsAndCurrentSelectionIsCreateRow(t *testing.T) {
	entries := []serverapi.WorktreeView{{WorktreeID: "wt-1"}}
	if got := Restore(entries, 0, "missing"); got != 0 {
		t.Fatalf("selection = %d, want create row", got)
	}
}
