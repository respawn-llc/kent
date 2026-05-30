package app

import (
	"strconv"
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
		if keyMsg.Type == tea.KeyRunes {
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

func isCtrlEnterCSISequence(seq string) bool {
	switch seq {
	case "13;5u", "13;5~", "27;5;13u", "27;5;13~":
		return true
	default:
		return false
	}
}

func isShiftEnterCSISequence(seq string) bool {
	switch seq {
	case "13;2u", "13;2~", "27;2;13u", "27;2;13~":
		return true
	default:
		return false
	}
}

func isCtrlBackspaceCSISequence(seq string) bool {
	modifier, ok := parseBackspaceCSIModifier(seq)
	if !ok {
		return false
	}
	return csiModifierHasCtrl(modifier)
}

func isSuperBackspaceCSISequence(seq string) bool {
	modifier, ok := parseBackspaceCSIModifier(seq)
	if !ok {
		return false
	}
	return csiModifierHasSuper(modifier)
}

func isHelpCSISequence(seq string) bool {
	codepoint, modifier, ok := parseCSIUCodepointAndModifier(seq)
	if !ok {
		return false
	}
	if !csiModifierHasSuper(modifier) {
		return false
	}
	switch rune(codepoint) {
	case '?', '/':
		return true
	default:
		return false
	}
}

func parseCSIUCodepointAndModifier(seq string) (int, int, bool) {
	if len(seq) < 4 || seq[len(seq)-1] != 'u' {
		return 0, 0, false
	}
	body := seq[:len(seq)-1]
	parts := strings.Split(body, ";")
	if len(parts) < 2 {
		return 0, 0, false
	}

	modifierIdx := 1
	codepointIdx := 0
	if parts[0] == "27" {
		if len(parts) < 3 {
			return 0, 0, false
		}
		modifierIdx = 1
		codepointIdx = 2
	}

	modifier, ok := parseCSIParamInt(parts[modifierIdx])
	if !ok {
		return 0, 0, false
	}
	codepoint, ok := parseCSIParamInt(parts[codepointIdx])
	if !ok {
		return 0, 0, false
	}
	return codepoint, modifier, true
}

func parseBackspaceCSIModifier(seq string) (int, bool) {
	if len(seq) < 3 {
		return 0, false
	}
	terminator := seq[len(seq)-1]
	if terminator != 'u' && terminator != '~' {
		return 0, false
	}
	body := seq[:len(seq)-1]
	parts := strings.Split(body, ";")
	if len(parts) < 2 {
		return 0, false
	}

	modifierIdx := 1
	keyCodeIdx := 0
	if parts[0] == "27" {
		if len(parts) < 3 {
			return 0, false
		}
		modifierIdx = 1
		keyCodeIdx = 2
	}

	modifier, ok := parseCSIParamInt(parts[modifierIdx])
	if !ok {
		return 0, false
	}
	keyCode, ok := parseCSIParamInt(parts[keyCodeIdx])
	if !ok {
		return 0, false
	}
	if keyCode != 127 && keyCode != 8 {
		return 0, false
	}
	return modifier, true
}

func parseCSIParamInt(field string) (int, bool) {
	if strings.TrimSpace(field) == "" {
		return 0, false
	}
	valueText := field
	if idx := strings.Index(valueText, ":"); idx >= 0 {
		valueText = valueText[:idx]
	}
	value, err := strconv.Atoi(valueText)
	if err != nil {
		return 0, false
	}
	return value, true
}

func csiModifierHasCtrl(modifier int) bool {
	if modifier <= 1 {
		return false
	}
	return (modifier-1)&4 != 0
}

func csiModifierHasSuper(modifier int) bool {
	if modifier <= 1 {
		return false
	}
	return (modifier-1)&8 != 0
}
