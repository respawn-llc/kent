package worktreedelete

import (
	"fmt"
	"strings"

	"builder/cli/app/internal/worktreeview"
	"builder/shared/serverapi"
)

type Action uint8

const (
	ActionCancel Action = iota
	ActionDelete
	ActionDeleteBranch
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

func Actions(target serverapi.WorktreeView) []Action {
	actions := []Action{ActionCancel, ActionDelete}
	if worktreeview.DeleteCanAutoDeleteBranch(target) || strings.TrimSpace(target.BranchName) != "" {
		actions = append(actions, ActionDeleteBranch)
	}
	return actions
}

func ClampAction(target serverapi.WorktreeView, selected Action, preferDeleteBranch bool) Action {
	actions := Actions(target)
	if selected != ActionCancel {
		for _, action := range actions {
			if action == selected {
				return selected
			}
		}
	}
	if preferDeleteBranch {
		for _, action := range actions {
			if action == ActionDeleteBranch {
				return ActionDeleteBranch
			}
		}
	}
	if len(actions) > 0 && actions[0] == ActionCancel && len(actions) == 1 {
		return ActionCancel
	}
	return ActionDelete
}

func MoveAction(target serverapi.WorktreeView, selected Action, delta int) Action {
	actions := Actions(target)
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

func PreviewLines(target serverapi.WorktreeView, selected Action) []PreviewLine {
	items := make([]PreviewLine, 0, 5)
	if strings.TrimSpace(target.BranchName) != "" && selected == ActionDeleteBranch {
		items = append(items, PreviewLine{Kind: PreviewLineKindBullet, Text: "• Local branch " + strings.TrimSpace(target.BranchName)})
	}
	if root := strings.TrimSpace(target.CanonicalRoot); root != "" {
		items = append(items, PreviewLine{Kind: PreviewLineKindBullet, Text: "• Workspace folder at " + root})
	}
	items = append(items, PreviewLine{Kind: PreviewLineKindBullet, Text: "• Git worktree " + worktreeview.DisplayName(target)})
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
