package worktreeui

import (
	"errors"
	"strings"
)

var errInvalidSelectionIdentity = errors.New("worktree selection identity is invalid")

type SelectionIdentityKind uint8

const (
	SelectionIdentityKindCreateRow SelectionIdentityKind = iota
	SelectionIdentityKindKentWorktree
	SelectionIdentityKindGitRoot
)

type SelectionIdentity struct {
	Kind  SelectionIdentityKind
	Value string
}

func SelectionIdentityForItem(item Item) SelectionIdentity {
	if item.WorktreeID != nil {
		worktreeID := strings.TrimSpace(*item.WorktreeID)
		return SelectionIdentity{Kind: SelectionIdentityKindKentWorktree, Value: worktreeID}
	}
	root := strings.TrimSpace(item.CanonicalRoot)
	return SelectionIdentity{Kind: SelectionIdentityKindGitRoot, Value: root}
}

func StableMutationSelector(item Item) string {
	if item.DeletePreview != nil {
		return item.DeletePreview.DeletionSelector
	}
	return SelectionIdentityForItem(item).Value
}

func RowCount(entries []Item) int {
	return len(entries) + 1
}

func Clamp(selection int, entries []Item) int {
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

func SelectedWorktree(entries []Item, selection int) (Item, bool) {
	if selection <= 0 {
		return Item{}, false
	}
	index := selection - 1
	if index < 0 || index >= len(entries) {
		return Item{}, false
	}
	return entries[index], true
}

func SelectedIdentity(entries []Item, selection int) SelectionIdentity {
	if item, ok := SelectedWorktree(entries, selection); ok {
		return SelectionIdentityForItem(item)
	}
	return SelectionIdentity{Kind: SelectionIdentityKindCreateRow}
}

func FindByIdentity(entries []Item, identity SelectionIdentity) (Item, int, bool, error) {
	if identity.Kind == SelectionIdentityKindCreateRow {
		return Item{}, 0, false, nil
	}
	if identity.Kind != SelectionIdentityKindKentWorktree &&
		identity.Kind != SelectionIdentityKindGitRoot {
		return Item{}, 0, false, errInvalidSelectionIdentity
	}
	for idx, item := range entries {
		itemIdentity := SelectionIdentityForItem(item)
		if itemIdentity == identity {
			return item, idx, true, nil
		}
	}
	return Item{}, 0, false, nil
}

func Restore(entries []Item, currentSelection int, selectedIdentity SelectionIdentity) (int, error) {
	if selectedIdentity.Kind == SelectionIdentityKindCreateRow {
		return 0, nil
	}
	if _, idx, ok, err := FindByIdentity(entries, selectedIdentity); err != nil {
		return 0, err
	} else if ok {
		return idx + 1, nil
	}
	if len(entries) == 0 {
		return 0, nil
	}
	return Clamp(currentSelection, entries), nil
}
