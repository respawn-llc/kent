package app

import (
	"reflect"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

const (
	csiModifierCapsLock = 1 << 6
	csiModifierNumLock  = 1 << 7
)

func adaptTerminalKeyMsg(msg tea.Msg) tea.Msg {
	bytes, ok := extractUnknownCSISequence(msg)
	if !ok {
		return msg
	}

	sequence, ok := parseTerminalCSISequence(bytes)
	if !ok {
		return msg
	}
	key, ok := decodeTerminalCSIKey(sequence)
	if !ok {
		return msg
	}
	return key
}

func extractUnknownCSISequence(msg tea.Msg) ([]byte, bool) {
	bytes, ok := decodeUnknownCSISequenceMsgBytes(msg)
	if !ok || len(bytes) < 3 || bytes[0] != '\x1b' || bytes[1] != '[' {
		return nil, false
	}
	return bytes, true
}

func decodeUnknownCSISequenceMsgBytes(msg tea.Msg) ([]byte, bool) {
	value := reflect.ValueOf(msg)
	if !value.IsValid() || value.Kind() != reflect.Slice {
		return nil, false
	}
	if value.Type().Elem().Kind() != reflect.Uint8 {
		return nil, false
	}
	bytes := make([]byte, value.Len())
	for i := range len(bytes) {
		part := value.Index(i).Uint()
		if part > 255 {
			return nil, false
		}
		bytes[i] = byte(part)
	}
	return bytes, true
}

type terminalCSISequence struct {
	final      byte
	parameters []int
}

func parseTerminalCSISequence(bytes []byte) (terminalCSISequence, bool) {
	parser := xansi.GetParser()
	defer xansi.PutParser(parser)

	_, width, consumed, state := xansi.GraphemeWidth.DecodeSequenceInString(string(bytes), 0, parser)
	if width != 0 || consumed != len(bytes) || state != 0 {
		return terminalCSISequence{}, false
	}
	command := xansi.Cmd(parser.Command())
	if command.Final() == 0 || command.Prefix() != 0 || command.Intermediate() != 0 {
		return terminalCSISequence{}, false
	}

	parameters := make([]int, len(parser.Params()))
	for index, parameter := range parser.Params() {
		if parameter.HasMore() {
			return terminalCSISequence{}, false
		}
		parameters[index] = parameter.Param(0)
	}
	return terminalCSISequence{final: command.Final(), parameters: parameters}, true
}

func decodeTerminalCSIKey(sequence terminalCSISequence) (tea.Msg, bool) {
	if key, ok := decodeTerminalFunctionalKey(sequence); ok {
		return key, true
	}
	if key, ok := decodeTerminalTildeKey(sequence); ok {
		return key, true
	}
	codepoint, modifier, ok := modifiedCSIKey(sequence)
	if !ok {
		return nil, false
	}
	if key, ok := decodeTerminalSpecialKey(codepoint, modifier); ok {
		return key, true
	}
	if modifier == 5 && codepoint >= 'a' && codepoint <= 'z' {
		return tea.KeyMsg{Type: tea.KeyType(codepoint - 'a' + 1)}, true
	}
	if modifier == 3 && codepoint >= 0x20 && codepoint <= 0x10ffff {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(codepoint)}, Alt: true}, true
	}
	return nil, false
}

func decodeTerminalTildeKey(sequence terminalCSISequence) (tea.Msg, bool) {
	if sequence.final != '~' {
		return nil, false
	}
	if len(sequence.parameters) == 3 && sequence.parameters[0] == 27 {
		codepoint, modifier, ok := normalizeCSIKey(sequence.parameters[2], sequence.parameters[1])
		if ok && (codepoint == 13 || codepoint == 8 || codepoint == 127) {
			return decodeTerminalSpecialKey(codepoint, modifier)
		}
		return nil, false
	}
	if len(sequence.parameters) != 2 {
		return nil, false
	}
	codepoint, modifier, ok := normalizeCSIKey(sequence.parameters[0], sequence.parameters[1])
	if !ok || modifier != 1 {
		return nil, false
	}
	switch codepoint {
	case 3:
		return tea.KeyMsg{Type: tea.KeyDelete}, true
	case 13:
		return tea.KeyMsg{Type: tea.KeyF3}, true
	default:
		return nil, false
	}
}

func decodeTerminalFunctionalKey(sequence terminalCSISequence) (tea.Msg, bool) {
	if sequence.final == 'P' {
		switch len(sequence.parameters) {
		case 0:
			return tea.KeyMsg{Type: tea.KeyF1}, true
		case 2:
			if sequence.parameters[0] == 1 {
				_, modifier, ok := normalizeCSIKey(0, sequence.parameters[1])
				if ok && modifier == 1 {
					return tea.KeyMsg{Type: tea.KeyF1}, true
				}
			}
		}
		return nil, false
	}
	if len(sequence.parameters) != 2 || sequence.parameters[0] != 1 {
		return nil, false
	}
	_, modifier, ok := normalizeCSIKey(0, sequence.parameters[1])
	if !ok {
		return nil, false
	}
	keyType, ok := terminalFunctionalKeyType(sequence.final)
	if !ok {
		return nil, false
	}
	switch modifier {
	case 1:
		return tea.KeyMsg{Type: keyType}, true
	case 3:
		if keyType == tea.KeyLeft || keyType == tea.KeyRight {
			return tea.KeyMsg{Type: keyType, Alt: true}, true
		}
	case 5:
		switch keyType {
		case tea.KeyLeft:
			return tea.KeyMsg{Type: tea.KeyCtrlLeft}, true
		case tea.KeyRight:
			return tea.KeyMsg{Type: tea.KeyCtrlRight}, true
		case tea.KeyEnd:
			return tea.KeyMsg{Type: tea.KeyCtrlEnd}, true
		}
	}
	return nil, false
}

func terminalFunctionalKeyType(final byte) (tea.KeyType, bool) {
	switch final {
	case 'A':
		return tea.KeyUp, true
	case 'B':
		return tea.KeyDown, true
	case 'C':
		return tea.KeyRight, true
	case 'D':
		return tea.KeyLeft, true
	case 'F':
		return tea.KeyEnd, true
	case 'H':
		return tea.KeyHome, true
	default:
		return 0, false
	}
}

func modifiedCSIKey(sequence terminalCSISequence) (int, int, bool) {
	if sequence.final != 'u' {
		return 0, 0, false
	}
	switch len(sequence.parameters) {
	case 1:
		return sequence.parameters[0], 1, true
	case 2:
		return normalizeCSIKey(sequence.parameters[0], sequence.parameters[1])
	case 3:
		if sequence.parameters[0] == 27 {
			return normalizeCSIKey(sequence.parameters[2], sequence.parameters[1])
		}
	default:
	}
	return 0, 0, false
}

func normalizeCSIKey(codepoint, modifier int) (int, int, bool) {
	if modifier < 1 {
		return 0, 0, false
	}
	modifierBits := modifier - 1
	modifierBits &^= csiModifierCapsLock | csiModifierNumLock
	return codepoint, modifierBits + 1, true
}

func decodeTerminalSpecialKey(codepoint, modifier int) (tea.Msg, bool) {
	switch codepoint {
	case 13, 57414:
		switch modifier {
		case 1:
			return tea.KeyMsg{Type: tea.KeyEnter}, true
		case 2:
			return customKeyMsg{Kind: customKeyShiftEnter}, true
		case 3:
			return tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, true
		case 5:
			return customKeyMsg{Kind: customKeyCtrlEnter}, true
		}
	case 8, 127:
		switch {
		case modifierHasControl(modifier):
			return customKeyMsg{Kind: customKeyCtrlBackspace}, true
		case modifier == 3:
			return tea.KeyMsg{Type: tea.KeyBackspace, Alt: true}, true
		case modifierHasSuper(modifier):
			return customKeyMsg{Kind: customKeySuperBackspace}, true
		}
	case 27:
		if modifier == 1 {
			return tea.KeyMsg{Type: tea.KeyEsc}, true
		}
	case '?', '/':
		if modifierHasSuper(modifier) {
			return customKeyMsg{Kind: customKeyHelp}, true
		}
	case 9:
		if modifier == 2 {
			return tea.KeyMsg{Type: tea.KeyShiftTab}, true
		}
	}
	return nil, false
}

func modifierHasControl(modifier int) bool {
	return modifier > 1 && (modifier-1)&4 != 0
}

func modifierHasSuper(modifier int) bool {
	return modifier > 1 && (modifier-1)&8 != 0
}

func terminalCursorProgramFilter(state *uiTerminalCursorState) func(tea.Model, tea.Msg) tea.Msg {
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(tea.QuitMsg); ok && state != nil {
			state.Stop()
		}
		return adaptTerminalKeyMsg(msg)
	}
}
