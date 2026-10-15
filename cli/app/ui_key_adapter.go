package app

import (
	"reflect"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	xansiparser "github.com/charmbracelet/x/ansi/parser"
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
	parameters []terminalCSIParameter
}

type terminalCSIParameter struct {
	values  []int
	present []bool
	hasMore bool
}

func (p terminalCSIParameter) value(defaultValue int) (int, bool) {
	if len(p.values) != 1 || len(p.present) != 1 {
		return 0, false
	}
	if !p.present[0] {
		return defaultValue, true
	}
	return p.values[0], true
}

func (p terminalCSIParameter) requiredValue() (int, bool) {
	if len(p.values) != 1 || len(p.present) != 1 || !p.present[0] {
		return 0, false
	}
	return p.values[0], true
}

func (p terminalCSIParameter) textRunes() ([]rune, bool) {
	if len(p.values) == 0 || len(p.values) != len(p.present) {
		return nil, false
	}
	runes := make([]rune, len(p.values))
	for index, value := range p.values {
		if !p.present[index] || value < 0 || value > utf8.MaxRune || !utf8.ValidRune(rune(value)) {
			return nil, false
		}
		character := rune(value)
		if character < 0x20 || character >= 0x7f && character <= 0x9f {
			return nil, false
		}
		runes[index] = character
	}
	return runes, true
}

func (s terminalCSISequence) parameter(index, defaultValue int) (int, bool) {
	if index >= len(s.parameters) {
		return defaultValue, true
	}
	return s.parameters[index].value(defaultValue)
}

func parseTerminalCSIParameters(params xansi.Params) []terminalCSIParameter {
	parameters := make([]terminalCSIParameter, 0, len(params))
	for _, packed := range params {
		if len(parameters) == 0 || !parameters[len(parameters)-1].hasMore {
			parameters = append(parameters, terminalCSIParameter{})
		}
		parameter := &parameters[len(parameters)-1]
		raw := int(packed)
		parameter.values = append(parameter.values, packed.Param(0))
		parameter.present = append(parameter.present, raw&xansiparser.ParamMask != xansiparser.MissingParam)
		parameter.hasMore = packed.HasMore()
	}
	return parameters
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

	return terminalCSISequence{final: command.Final(), parameters: parseTerminalCSIParameters(parser.Params())}, true
}

func decodeTerminalCSIKey(sequence terminalCSISequence) (tea.Msg, bool) {
	if key, ok := decodeTerminalFunctionalKey(sequence); ok {
		return key, true
	}
	if key, ok := decodeTerminalTildeKey(sequence); ok {
		return key, true
	}
	if sequence.final == 'u' {
		return decodeTerminalCSIUKey(sequence)
	}
	return nil, false
}

func decodeTerminalTildeKey(sequence terminalCSISequence) (tea.Msg, bool) {
	if sequence.final != '~' {
		return nil, false
	}
	if len(sequence.parameters) == 3 {
		first, firstOK := sequence.parameters[0].requiredValue()
		modifier, modifierOK := sequence.parameters[1].requiredValue()
		codepoint, codepointOK := sequence.parameters[2].requiredValue()
		if !firstOK || !modifierOK || !codepointOK || first != 27 {
			return nil, false
		}
		codepoint, modifier, ok := normalizeCSIKey(codepoint, modifier)
		if ok && (codepoint == 13 || codepoint == 8 || codepoint == 127) {
			return decodeTerminalSpecialKey(codepoint, modifier)
		}
		return nil, false
	}
	if len(sequence.parameters) != 2 {
		return nil, false
	}
	codepoint, codepointOK := sequence.parameters[0].requiredValue()
	modifier, modifierOK := sequence.parameters[1].requiredValue()
	if !codepointOK || !modifierOK {
		return nil, false
	}
	codepoint, modifier, ok := normalizeCSIKey(codepoint, modifier)
	if !ok || modifier != 1 {
		return nil, false
	}
	switch codepoint {
	case 3:
		return tea.KeyMsg{Type: tea.KeyDelete}, true
	case 5:
		return tea.KeyMsg{Type: tea.KeyPgUp}, true
	case 6:
		return tea.KeyMsg{Type: tea.KeyPgDown}, true
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
			first, firstOK := sequence.parameters[0].requiredValue()
			if first == 1 && firstOK {
				modifier, modifierOK := sequence.parameters[1].requiredValue()
				_, modifier, ok := normalizeCSIKey(0, modifier)
				if modifierOK && ok && modifier == 1 {
					return tea.KeyMsg{Type: tea.KeyF1}, true
				}
			}
		}
		return nil, false
	}
	if len(sequence.parameters) != 2 {
		return nil, false
	}
	first, firstOK := sequence.parameters[0].requiredValue()
	modifier, modifierOK := sequence.parameters[1].requiredValue()
	if !firstOK || first != 1 || !modifierOK {
		return nil, false
	}
	_, modifier, ok := normalizeCSIKey(0, modifier)
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

func normalizeCSIKey(codepoint, modifier int) (int, int, bool) {
	if modifier < 1 {
		return 0, 0, false
	}
	modifierBits := modifier - 1
	modifierBits &^= csiModifierCapsLock | csiModifierNumLock
	return codepoint, modifierBits + 1, true
}

func decodeTerminalCSIUKey(sequence terminalCSISequence) (tea.Msg, bool) {
	if len(sequence.parameters) < 1 || len(sequence.parameters) > 3 {
		return nil, false
	}
	codepoint, ok := sequence.parameters[0].requiredValue()
	if !ok {
		return nil, false
	}
	modifier, ok := sequence.parameter(1, 1)
	if !ok {
		return nil, false
	}
	codepoint, modifier, ok = normalizeCSIKey(codepoint, modifier)
	if !ok {
		return nil, false
	}

	if len(sequence.parameters) == 3 {
		if codepoint == 27 {
			xtermCodepoint, ok := sequence.parameters[2].requiredValue()
			if !ok {
				return nil, false
			}
			return decodeTerminalKeyWithoutText(xtermCodepoint, modifier)
		}
		text, ok := sequence.parameters[2].textRunes()
		if !ok {
			return nil, false
		}
		if modifierHasControl(modifier) {
			if key, ok := decodeTerminalKeyWithoutText(codepoint, modifier); ok {
				return key, true
			}
		}
		if len(text) == 1 && text[0] == ' ' &&
			!modifierHasAlt(modifier) && !modifierHasControl(modifier) && !modifierHasSuper(modifier) {
			return tea.KeyMsg{Type: tea.KeySpace}, true
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: text, Alt: modifierHasAlt(modifier)}, true
	}
	return decodeTerminalKeyWithoutText(codepoint, modifier)
}

func decodeTerminalKeyWithoutText(codepoint, modifier int) (tea.Msg, bool) {
	if key, ok := decodeTerminalSpecialKey(codepoint, modifier); ok {
		return key, true
	}
	if modifierHasControl(modifier) {
		switch codepoint {
		case ' ', '@':
			return tea.KeyMsg{Type: tea.KeyCtrlAt, Alt: modifierHasAlt(modifier)}, true
		case '[':
			return tea.KeyMsg{Type: tea.KeyCtrlOpenBracket, Alt: modifierHasAlt(modifier)}, true
		case '\\':
			return tea.KeyMsg{Type: tea.KeyCtrlBackslash, Alt: modifierHasAlt(modifier)}, true
		case ']':
			return tea.KeyMsg{Type: tea.KeyCtrlCloseBracket, Alt: modifierHasAlt(modifier)}, true
		case '^':
			return tea.KeyMsg{Type: tea.KeyCtrlCaret, Alt: modifierHasAlt(modifier)}, true
		case '_':
			return tea.KeyMsg{Type: tea.KeyCtrlUnderscore, Alt: modifierHasAlt(modifier)}, true
		case '?':
			return tea.KeyMsg{Type: tea.KeyCtrlQuestionMark, Alt: modifierHasAlt(modifier)}, true
		}
		if codepoint >= 'a' && codepoint <= 'z' {
			return tea.KeyMsg{Type: tea.KeyType(codepoint - 'a' + 1), Alt: modifierHasAlt(modifier)}, true
		}
	}
	return nil, false
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
		case modifier == 1:
			return tea.KeyMsg{Type: tea.KeyBackspace}, true
		case modifier == 3:
			return tea.KeyMsg{Type: tea.KeyBackspace, Alt: true}, true
		case modifierHasSuper(modifier):
			return customKeyMsg{Kind: customKeySuperBackspace}, true
		}
	case 27:
		if modifier == 1 {
			return tea.KeyMsg{Type: tea.KeyEsc}, true
		}
		if modifier == 3 {
			return tea.KeyMsg{Type: tea.KeyEsc, Alt: true}, true
		}
	case '?', '/':
		if modifierHasSuper(modifier) {
			return customKeyMsg{Kind: customKeyHelp}, true
		}
	case 9:
		switch modifier {
		case 1:
			return tea.KeyMsg{Type: tea.KeyTab}, true
		case 2:
			return tea.KeyMsg{Type: tea.KeyShiftTab}, true
		case 3:
			return tea.KeyMsg{Type: tea.KeyTab, Alt: true}, true
		default:
			if modifierHasControl(modifier) {
				return tea.KeyMsg{Type: tea.KeyCtrlI, Alt: modifierHasAlt(modifier)}, true
			}
		}
	}
	return nil, false
}

func modifierHasControl(modifier int) bool {
	return modifier > 1 && (modifier-1)&4 != 0
}

func modifierHasAlt(modifier int) bool {
	return modifier > 1 && (modifier-1)&2 != 0
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
