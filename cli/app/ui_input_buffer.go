package app

import tuiinput "builder/cli/tui/input"

func (m *uiModel) cursorIndex() int {
	return bufferCursorIndex(m.input, m.inputCursor)
}

func nextNonZeroToken(token uint64) uint64 {
	token++
	if token == 0 {
		return 1
	}
	return token
}

func (m *uiModel) invalidateMainInputDraftToken() {
	m.mainInputDraftToken = nextNonZeroToken(m.mainInputDraftToken)
}

func (m *uiModel) replaceMainInput(text string, cursor int) {
	m.invalidateMainInputDraftToken()
	m.input = text
	m.inputCursor = cursor
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
}

func (m *uiModel) clearInput() {
	m.replaceMainInput("", -1)
	m.resetPromptHistoryNavigation()
}

func (m *uiModel) insertInputRunes(chars []rune) {
	updated, nextCursor, ok := insertBufferRunes(m.input, m.inputCursor, chars)
	if !ok {
		return
	}
	m.input = updated
	m.inputCursor = nextCursor
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
}

func (m *uiModel) backspaceInput() bool {
	updated, nextCursor, ok := backspaceBuffer(m.input, m.inputCursor)
	if !ok {
		return false
	}
	m.input = updated
	m.inputCursor = nextCursor
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
	return true
}

func (m *uiModel) deleteForwardInput() bool {
	updated, nextCursor, ok := deleteForwardBuffer(m.input, m.inputCursor)
	if !ok {
		return false
	}
	m.input = updated
	m.inputCursor = nextCursor
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
	return true
}

func (m *uiModel) deleteBackwardWordInput() bool {
	updated, nextCursor, killBuffer, ok := deleteBackwardWordBuffer(m.input, m.inputCursor, m.inputKillBuffer)
	if !ok {
		return false
	}
	m.input = updated
	m.inputCursor = nextCursor
	m.inputKillBuffer = killBuffer
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
	return true
}

func (m *uiModel) deleteForwardWordInput() bool {
	updated, nextCursor, killBuffer, ok := deleteForwardWordBuffer(m.input, m.inputCursor, m.inputKillBuffer)
	if !ok {
		return false
	}
	m.input = updated
	m.inputCursor = nextCursor
	m.inputKillBuffer = killBuffer
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
	return true
}

func (m *uiModel) killInputToLineStart() bool {
	updated, nextCursor, killBuffer, ok := killToLineStartBuffer(m.input, m.inputCursor, m.inputKillBuffer)
	if !ok {
		return false
	}
	m.input = updated
	m.inputCursor = nextCursor
	m.inputKillBuffer = killBuffer
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
	return true
}

func (m *uiModel) killInputToLineEnd() bool {
	updated, nextCursor, killBuffer, ok := killToLineEndBuffer(m.input, m.inputCursor, m.inputKillBuffer)
	if !ok {
		return false
	}
	m.input = updated
	m.inputCursor = nextCursor
	m.inputKillBuffer = killBuffer
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
	return true
}

func (m *uiModel) yankInput() bool {
	updated, nextCursor, ok := yankBuffer(m.input, m.inputCursor, m.inputKillBuffer)
	if !ok {
		return false
	}
	m.input = updated
	m.inputCursor = nextCursor
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
	return true
}

func (m *uiModel) moveCursorLeft() {
	m.inputCursor = moveBufferCursorLeft(m.input, m.inputCursor)
	m.refreshAutocompleteFromInput()
}

func (m *uiModel) moveCursorRight() {
	m.inputCursor = moveBufferCursorRight(m.input, m.inputCursor)
	m.refreshAutocompleteFromInput()
}

func (m *uiModel) moveCursorStart() {
	m.inputCursor = moveBufferCursorStart()
	m.refreshAutocompleteFromInput()
}

func (m *uiModel) moveCursorEnd() {
	m.inputCursor = moveBufferCursorEnd()
	m.refreshAutocompleteFromInput()
}

func (m *uiModel) moveCursorWordLeft() {
	m.inputCursor = moveBufferCursorWordLeft(m.input, m.inputCursor)
	m.refreshAutocompleteFromInput()
}

func (m *uiModel) moveCursorWordRight() {
	m.inputCursor = moveBufferCursorWordRight(m.input, m.inputCursor)
	m.refreshAutocompleteFromInput()
}

func (m *uiModel) moveCursorUpLine() bool {
	nextCursor, moved := moveBufferCursorUpLine(m.input, m.inputCursor, m.effectiveWidth(), m.layout().mainInputPrefix())
	m.inputCursor = nextCursor
	m.refreshAutocompleteFromInput()
	return moved
}

func (m *uiModel) moveCursorDownLine() bool {
	nextCursor, moved := moveBufferCursorDownLine(m.input, m.inputCursor, m.effectiveWidth(), m.layout().mainInputPrefix())
	m.inputCursor = nextCursor
	m.refreshAutocompleteFromInput()
	return moved
}

func (m *uiModel) deleteCurrentInputLine() bool {
	updated, nextCursor, ok := deleteCurrentBufferLine(m.input, m.inputCursor)
	if !ok {
		return false
	}
	m.input = updated
	m.inputCursor = nextCursor
	m.syncPromptHistorySelectionToInput()
	m.refreshAutocompleteFromInput()
	return true
}

func (m *uiModel) clearAskInput() {
	m.ask.input = ""
	m.ask.inputCursor = -1
}

func (m *uiModel) insertAskInputRunes(chars []rune) {
	updated, nextCursor, ok := insertBufferRunes(m.ask.input, m.ask.inputCursor, chars)
	if !ok {
		return
	}
	m.ask.input = updated
	m.ask.inputCursor = nextCursor
}

func (m *uiModel) backspaceAskInput() bool {
	updated, nextCursor, ok := backspaceBuffer(m.ask.input, m.ask.inputCursor)
	if !ok {
		return false
	}
	m.ask.input = updated
	m.ask.inputCursor = nextCursor
	return true
}

func (m *uiModel) deleteForwardAskInput() bool {
	updated, nextCursor, ok := deleteForwardBuffer(m.ask.input, m.ask.inputCursor)
	if !ok {
		return false
	}
	m.ask.input = updated
	m.ask.inputCursor = nextCursor
	return true
}

func (m *uiModel) deleteBackwardWordAskInput() bool {
	updated, nextCursor, killBuffer, ok := deleteBackwardWordBuffer(m.ask.input, m.ask.inputCursor, m.ask.inputKill)
	if !ok {
		return false
	}
	m.ask.input = updated
	m.ask.inputCursor = nextCursor
	m.ask.inputKill = killBuffer
	return true
}

func (m *uiModel) deleteForwardWordAskInput() bool {
	updated, nextCursor, killBuffer, ok := deleteForwardWordBuffer(m.ask.input, m.ask.inputCursor, m.ask.inputKill)
	if !ok {
		return false
	}
	m.ask.input = updated
	m.ask.inputCursor = nextCursor
	m.ask.inputKill = killBuffer
	return true
}

func (m *uiModel) killAskInputToLineStart() bool {
	updated, nextCursor, killBuffer, ok := killToLineStartBuffer(m.ask.input, m.ask.inputCursor, m.ask.inputKill)
	if !ok {
		return false
	}
	m.ask.input = updated
	m.ask.inputCursor = nextCursor
	m.ask.inputKill = killBuffer
	return true
}

func (m *uiModel) killAskInputToLineEnd() bool {
	updated, nextCursor, killBuffer, ok := killToLineEndBuffer(m.ask.input, m.ask.inputCursor, m.ask.inputKill)
	if !ok {
		return false
	}
	m.ask.input = updated
	m.ask.inputCursor = nextCursor
	m.ask.inputKill = killBuffer
	return true
}

func (m *uiModel) yankAskInput() bool {
	updated, nextCursor, ok := yankBuffer(m.ask.input, m.ask.inputCursor, m.ask.inputKill)
	if !ok {
		return false
	}
	m.ask.input = updated
	m.ask.inputCursor = nextCursor
	return true
}

func (m *uiModel) moveAskCursorLeft() {
	m.ask.inputCursor = moveBufferCursorLeft(m.ask.input, m.ask.inputCursor)
}

func (m *uiModel) moveAskCursorRight() {
	m.ask.inputCursor = moveBufferCursorRight(m.ask.input, m.ask.inputCursor)
}

func (m *uiModel) moveAskCursorStart() {
	m.ask.inputCursor = moveBufferCursorStart()
}

func (m *uiModel) moveAskCursorEnd() {
	m.ask.inputCursor = moveBufferCursorEnd()
}

func (m *uiModel) moveAskCursorWordLeft() {
	m.ask.inputCursor = moveBufferCursorWordLeft(m.ask.input, m.ask.inputCursor)
}

func (m *uiModel) moveAskCursorWordRight() {
	m.ask.inputCursor = moveBufferCursorWordRight(m.ask.input, m.ask.inputCursor)
}

func (m *uiModel) moveAskCursorUpLine() bool {
	nextCursor, moved := moveBufferCursorUpLine(m.ask.input, m.ask.inputCursor, m.effectiveWidth(), m.askInputPrefix())
	m.ask.inputCursor = nextCursor
	return moved
}

func (m *uiModel) moveAskCursorDownLine() bool {
	nextCursor, moved := moveBufferCursorDownLine(m.ask.input, m.ask.inputCursor, m.effectiveWidth(), m.askInputPrefix())
	m.ask.inputCursor = nextCursor
	return moved
}

func (m *uiModel) deleteCurrentAskInputLine() bool {
	updated, nextCursor, ok := deleteCurrentBufferLine(m.ask.input, m.ask.inputCursor)
	if !ok {
		return false
	}
	m.ask.input = updated
	m.ask.inputCursor = nextCursor
	return true
}

func bufferCursorIndex(text string, cursor int) int {
	return clampCursor(cursor, len([]rune(text)))
}

func insertBufferRunes(text string, cursor int, chars []rune) (string, int, bool) {
	if len(chars) == 0 {
		return text, cursor, false
	}
	filtered, _ := stripMouseSGRRunes(chars)
	if len(filtered) == 0 {
		return text, cursor, false
	}
	editor := bufferEditor(text, cursor)
	editor.InsertString(string(filtered))
	nextCursor := runeOffsetForByteCursor(editor.Text(), editor.Cursor())
	cleaned, cleanedCursor, _ := stripMouseSGRRunesWithCursor([]rune(editor.Text()), nextCursor)
	return string(cleaned), cleanedCursor, true
}

func backspaceBuffer(text string, cursor int) (string, int, bool) {
	editor := bufferEditor(text, cursor)
	if !editor.DeleteBackward() {
		return text, cursor, false
	}
	return editor.Text(), runeOffsetForByteCursor(editor.Text(), editor.Cursor()), true
}

func deleteForwardBuffer(text string, cursor int) (string, int, bool) {
	editor := bufferEditor(text, cursor)
	if !editor.DeleteForward() {
		return text, bufferCursorIndex(text, cursor), false
	}
	return editor.Text(), runeOffsetForByteCursor(editor.Text(), editor.Cursor()), true
}

func moveBufferCursorLeft(text string, cursor int) int {
	editor := bufferEditor(text, cursor)
	editor.MoveLeft()
	return runeOffsetForByteCursor(text, editor.Cursor())
}

func moveBufferCursorRight(text string, cursor int) int {
	editor := bufferEditor(text, cursor)
	editor.MoveRight()
	return runeOffsetForByteCursor(text, editor.Cursor())
}

func moveBufferCursorStart() int {
	return 0
}

func moveBufferCursorEnd() int {
	return -1
}

func moveBufferCursorWordLeft(text string, cursor int) int {
	editor := bufferEditor(text, cursor)
	editor.MoveWordLeft()
	return runeOffsetForByteCursor(text, editor.Cursor())
}

func moveBufferCursorWordRight(text string, cursor int) int {
	editor := bufferEditor(text, cursor)
	editor.MoveWordRight()
	return runeOffsetForByteCursor(text, editor.Cursor())
}

func moveBufferCursorUpLine(text string, cursor int, width int, prefix string) (int, bool) {
	return moveBufferCursorVertical(text, cursor, width, prefix, -1)
}

func moveBufferCursorDownLine(text string, cursor int, width int, prefix string) (int, bool) {
	return moveBufferCursorVertical(text, cursor, width, prefix, 1)
}

func moveBufferCursorVertical(text string, cursor int, width int, prefix string, delta int) (int, bool) {
	if width < 1 {
		width = 1
	}
	renderText := prefix + text
	editor := tuiinput.NewEditor()
	editor.Replace(renderText)
	editor.SetCursor(len(prefix) + byteOffsetForRuneCursor(text, cursor))
	lines := editor.WrappedLines(width)
	lineIndex := bufferWrappedLineIndex(lines, editor.Cursor())
	if lineIndex < 0 {
		return bufferCursorIndex(text, cursor), false
	}
	if delta < 0 && lineIndex == 0 {
		nextCursor := 0
		return nextCursor, nextCursor != bufferCursorIndex(text, cursor)
	}
	if delta > 0 && lineIndex+1 >= len(lines) {
		nextCursor := len([]rune(text))
		return nextCursor, nextCursor != bufferCursorIndex(text, cursor)
	}
	targetIndex := lineIndex + delta
	currentLine := lines[lineIndex]
	targetLine := lines[targetIndex]
	targetCol := tuiinput.DisplayWidth(renderText[currentLine.Start:editor.Cursor()])
	prefixWidth := tuiinput.DisplayWidth(prefix)
	currentHasPrefix := currentLine.Start < len(prefix)
	targetHasPrefix := targetLine.Start < len(prefix)
	switch {
	case currentHasPrefix && !targetHasPrefix:
		targetCol -= prefixWidth
	case !currentHasPrefix && targetHasPrefix:
		targetCol += prefixWidth
	}
	if targetCol < 0 {
		targetCol = 0
	}
	nextByteCursor := editor.CursorAtDisplayColumn(targetLine, targetCol) - len(prefix)
	if nextByteCursor < 0 {
		nextByteCursor = 0
	}
	nextCursor := runeOffsetForByteCursor(text, nextByteCursor)
	return nextCursor, nextCursor != bufferCursorIndex(text, cursor)
}

func deleteCurrentBufferLine(text string, cursor int) (string, int, bool) {
	editor := bufferEditor(text, cursor)
	if !editor.DeleteCurrentLine() {
		return text, bufferCursorIndex(text, cursor), false
	}
	return editor.Text(), runeOffsetForByteCursor(editor.Text(), editor.Cursor()), true
}

func deleteBackwardWordBuffer(text string, cursor int, killBuffer string) (string, int, string, bool) {
	editor := bufferEditorWithKill(text, cursor, killBuffer)
	if !editor.DeleteBackwardWord() {
		return text, bufferCursorIndex(text, cursor), killBuffer, false
	}
	return editor.Text(), runeOffsetForByteCursor(editor.Text(), editor.Cursor()), editor.KillBuffer(), true
}

func deleteForwardWordBuffer(text string, cursor int, killBuffer string) (string, int, string, bool) {
	editor := bufferEditorWithKill(text, cursor, killBuffer)
	if !editor.DeleteForwardWord() {
		return text, bufferCursorIndex(text, cursor), killBuffer, false
	}
	return editor.Text(), runeOffsetForByteCursor(editor.Text(), editor.Cursor()), editor.KillBuffer(), true
}

func killToLineStartBuffer(text string, cursor int, killBuffer string) (string, int, string, bool) {
	editor := bufferEditorWithKill(text, cursor, killBuffer)
	if !editor.KillToLineStart() {
		return text, bufferCursorIndex(text, cursor), killBuffer, false
	}
	return editor.Text(), runeOffsetForByteCursor(editor.Text(), editor.Cursor()), editor.KillBuffer(), true
}

func killToLineEndBuffer(text string, cursor int, killBuffer string) (string, int, string, bool) {
	editor := bufferEditorWithKill(text, cursor, killBuffer)
	if !editor.KillToLineEnd() {
		return text, bufferCursorIndex(text, cursor), killBuffer, false
	}
	return editor.Text(), runeOffsetForByteCursor(editor.Text(), editor.Cursor()), editor.KillBuffer(), true
}

func yankBuffer(text string, cursor int, killBuffer string) (string, int, bool) {
	editor := bufferEditorWithKill(text, cursor, killBuffer)
	if !editor.Yank() {
		return text, bufferCursorIndex(text, cursor), false
	}
	return editor.Text(), runeOffsetForByteCursor(editor.Text(), editor.Cursor()), true
}

func clampCursor(cursor, size int) int {
	if cursor < 0 {
		return size
	}
	if cursor > size {
		return size
	}
	return cursor
}

func bufferEditor(text string, cursor int) tuiinput.Editor {
	editor := tuiinput.NewEditor()
	editor.Replace(text)
	editor.SetCursor(byteOffsetForRuneCursor(text, cursor))
	return editor
}

func bufferEditorWithKill(text string, cursor int, killBuffer string) tuiinput.Editor {
	editor := bufferEditor(text, cursor)
	editor.SetKillBuffer(killBuffer)
	return editor
}

func bufferWrappedLineIndex(lines []tuiinput.LineRange, cursor int) int {
	if len(lines) == 0 {
		return -1
	}
	for index, line := range lines {
		if cursor < line.Start {
			continue
		}
		if cursor < line.End {
			return index
		}
		if cursor == line.End {
			if index+1 < len(lines) && lines[index+1].Start == cursor {
				continue
			}
			return index
		}
	}
	return len(lines) - 1
}

func runeOffsetForByteCursor(text string, cursor int) int {
	if cursor <= 0 {
		return 0
	}
	if cursor >= len(text) {
		return len([]rune(text))
	}
	offset := 0
	for byteIndex := range text {
		if byteIndex >= cursor {
			return offset
		}
		offset++
	}
	return len([]rune(text))
}
