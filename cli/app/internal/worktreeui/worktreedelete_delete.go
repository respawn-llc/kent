package worktreeui

import (
	"core/shared/serverapi"
	"fmt"
	"strings"
)

type DeleteAction uint8

const (
	DeleteActionCancel DeleteAction = iota
	DeleteActionDelete
	DeleteActionDeleteBranch
)

type PreviewLineKind uint8

const (
	PreviewLineKindHeader PreviewLineKind = iota
	PreviewLineKindBullet
	PreviewLineKindWarning
)

type PreviewLine struct {
	Kind PreviewLineKind
	Text string
}

func DeleteActions(target serverapi.WorktreeView) []DeleteAction {
	actions := []DeleteAction{DeleteActionCancel, DeleteActionDelete}
	if DeleteCanAutoDeleteBranch(target) || strings.TrimSpace(target.BranchName) != "" {
		actions = append(actions, DeleteActionDeleteBranch)
	}
	return actions
}

func ClampDeleteAction(target serverapi.WorktreeView, selected DeleteAction, preferDeleteBranch bool) DeleteAction {
	actions := DeleteActions(target)
	if selected != DeleteActionCancel {
		for _, action := range actions {
			if action == selected {
				return selected
			}
		}
	}
	if preferDeleteBranch {
		for _, action := range actions {
			if action == DeleteActionDeleteBranch {
				return DeleteActionDeleteBranch
			}
		}
	}
	if len(actions) > 0 && actions[0] == DeleteActionCancel && len(actions) == 1 {
		return DeleteActionCancel
	}
	return DeleteActionDelete
}

func MoveDeleteAction(target serverapi.WorktreeView, selected DeleteAction, delta int) DeleteAction {
	actions := DeleteActions(target)
	if len(actions) == 0 {
		return selected
	}
	index := 0
	for idx, action := range actions {
		if action == selected {
			index = idx
			break
		}
	}
	index += delta
	if index < 0 {
		index = 0
	}
	if index >= len(actions) {
		index = len(actions) - 1
	}
	return actions[index]
}

func PreviewLines(target serverapi.WorktreeView, selected DeleteAction) []PreviewLine {
	items := make([]PreviewLine, 0, 5)
	if strings.TrimSpace(target.BranchName) != "" && selected == DeleteActionDeleteBranch {
		items = append(items, PreviewLine{Kind: PreviewLineKindBullet, Text: "• Local branch " + strings.TrimSpace(target.BranchName)})
	}
	if root := strings.TrimSpace(target.CanonicalRoot); root != "" {
		items = append(items, PreviewLine{Kind: PreviewLineKindBullet, Text: "• Workspace folder at " + root})
	}
	items = append(items, PreviewLine{Kind: PreviewLineKindBullet, Text: "• Git worktree " + DisplayName(target)})
	if target.DirtyFileCount < 0 {
		items = append(items, PreviewLine{Kind: PreviewLineKindWarning, Text: "• Dirty file count unavailable; delete will force removal"})
	} else if target.DirtyFileCount > 0 {
		items = append(items, PreviewLine{Kind: PreviewLineKindWarning, Text: "• Drop " + pluralizeCount(target.DirtyFileCount, "modified/untracked file")})
	}
	if len(items) == 0 {
		return nil
	}
	return append([]PreviewLine{{Kind: PreviewLineKindHeader, Text: "Will delete:"}}, items...)
}

func pluralizeCount(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}
