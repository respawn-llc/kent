package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	keyTypeCtrlEnterCSI      tea.KeyType = -1024
	keyTypeShiftEnterCSI     tea.KeyType = -1025
	keyTypeCtrlBackspaceCSI  tea.KeyType = -1026
	keyTypeSuperBackspaceCSI tea.KeyType = -1027
	keyTypeHelpCSI           tea.KeyType = -1028
)

type customKeyKind uint8

const (
	customKeyUnknown customKeyKind = iota
	customKeyCtrlEnter
	customKeyShiftEnter
	customKeyCtrlBackspace
	customKeySuperBackspace
	customKeyHelp
)

type customKeyMsg struct {
	Kind customKeyKind
}

func normalizeKeyMsg(msg tea.Msg) (tea.KeyMsg, bool) {
	normalized, ok, _ := normalizeKeyMsgWithSource(msg)
	return normalized, ok
}

func normalizeKeyMsgWithSource(msg tea.Msg) (tea.KeyMsg, bool, string) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyEnter && keyMsg.Alt {
			keyMsg.Type = tea.KeyCtrlJ
			keyMsg.Alt = false
			return keyMsg, true, "alt_enter"
		}
		if keyMsg.Type == tea.KeyRunes {
			if !keyMsg.Paste && len(keyMsg.Runes) == 1 && keyMsg.Runes[0] == '\x00' {
				keyMsg.Type = tea.KeyNull
				keyMsg.Runes = nil
				return keyMsg, true, "keymsg"
			}
			if len(keyMsg.Runes) == 1 && keyMsg.Runes[0] == '\x1b' {
				keyMsg.Type = tea.KeyEsc
				keyMsg.Runes = nil
				return keyMsg, true, "keymsg_escape_rune"
			}
			filtered, removed := stripMouseSGRRunes(keyMsg.Runes)
			if removed {
				if len(filtered) == 0 {
					return tea.KeyMsg{}, false, ""
				}
				keyMsg.Runes = filtered
			}
		}
		return keyMsg, true, "keymsg"
	}
	customKey, ok := msg.(customKeyMsg)
	if !ok {
		return tea.KeyMsg{}, false, ""
	}
	switch customKey.Kind {
	case customKeyCtrlEnter:
		return tea.KeyMsg{Type: keyTypeCtrlEnterCSI}, true, "custom_key"
	case customKeyShiftEnter:
		return tea.KeyMsg{Type: keyTypeShiftEnterCSI}, true, "custom_key"
	case customKeyCtrlBackspace:
		return tea.KeyMsg{Type: keyTypeCtrlBackspaceCSI}, true, "custom_key"
	case customKeySuperBackspace:
		return tea.KeyMsg{Type: keyTypeSuperBackspaceCSI}, true, "custom_key"
	case customKeyHelp:
		return tea.KeyMsg{Type: keyTypeHelpCSI}, true, "custom_key"
	default:
		return tea.KeyMsg{}, false, ""
	}
}

func isHelpKey(msg tea.KeyMsg, model *uiModel) bool {
	if msg.Type == tea.KeyF1 || msg.Type == keyTypeHelpCSI {
		return true
	}
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return false
	}
	if msg.Alt {
		switch msg.Runes[0] {
		case '?', '/':
			return true
		default:
			return false
		}
	}
	return msg.Runes[0] == '?' && model != nil && model.canToggleHelpWithQuestionMark()
}

func isQueueSubmissionKey(msg tea.KeyMsg) bool {
	keyString := strings.ToLower(msg.String())
	return keyString == "tab" || keyString == "ctrl+enter" || msg.Type == keyTypeCtrlEnterCSI
}

func isShiftEnterKey(msg tea.KeyMsg) bool {
	return msg.Type == keyTypeShiftEnterCSI || strings.ToLower(msg.String()) == "shift+enter"
}

func isDeleteCurrentLineKeyForGOOS(msg tea.KeyMsg, goos string) bool {
	keyString := strings.ToLower(msg.String())
	if msg.Type == keyTypeCtrlBackspaceCSI || msg.Type == keyTypeSuperBackspaceCSI {
		return true
	}
	if goos == "darwin" && (msg.Type == tea.KeyCtrlU || keyString == "ctrl+u") {
		return true
	}
	return keyString == "ctrl+backspace" || keyString == "cmd+backspace" || keyString == "super+backspace"
}
