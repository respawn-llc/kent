package worktreeselection

import (
	"strings"

	"core/shared/serverapi"
)

const CreateRowID = "__create__"

func RowCount(entries []serverapi.WorktreeView) int {
	return len(entries) + 1
}

func Clamp(selection int, entries []serverapi.WorktreeView) int {
	rowCount := RowCount(entries)
	if rowCount <= 0 {
		return 0
	}
	if selection < 0 {
		return 0
	}
	if selection >= rowCount {
		return rowCount - 1
	}
	return selection
}

func SelectedWorktree(entries []serverapi.WorktreeView, selection int) (serverapi.WorktreeView, bool) {
	if selection <= 0 {
		return serverapi.WorktreeView{}, false
	}
	index := selection - 1
	if index < 0 || index >= len(entries) {
		return serverapi.WorktreeView{}, false
	}
	return entries[index], true
}

func SelectedID(entries []serverapi.WorktreeView, selection int) string {
	if item, ok := SelectedWorktree(entries, selection); ok {
		return strings.TrimSpace(item.WorktreeID)
	}
	return CreateRowID
}

func Restore(entries []serverapi.WorktreeView, currentSelection int, selectedID string) int {
	trimmed := strings.TrimSpace(selectedID)
	if trimmed == "" || trimmed == CreateRowID {
		return 0
	}
	for idx, item := range entries {
		if strings.TrimSpace(item.WorktreeID) == trimmed {
			return idx + 1
		}
	}
	if len(entries) == 0 {
		return 0
	}
	return Clamp(currentSelection, entries)
}
