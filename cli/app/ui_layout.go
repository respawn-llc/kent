package app

import (
	"strings"

	"builder/cli/tui"
)

const (
	ansiHideCursor        = "\x1b[?25l"
	ansiClearLine         = "\x1b[2K"
	statusContextBarWidth = 10
	queuedMessagesLimit   = 5
)

type uiViewLayout struct {
	model *uiModel
}

type uiRenderFrame struct {
	width            int
	height           int
	chatPanel        []string
	pickerPane       []string
	queuePane        []string
	inputPane        []string
	helpPane         []string
	statusLine       string
	padToHeight      bool
	tailOnly         bool
	inputCursor      uiInputFieldCursor
	cursorFrameCount int
}

type nativeLiveRegionState struct {
	pad             int
	lines           int
	streamingActive bool
}

func (m *uiModel) View() string {
	return m.layout().render()
}

func (l uiViewLayout) render() string {
	if l.model.surface() == uiSurfaceOngoingTranscript {
		return l.renderNativeOngoing()
	}
	style := uiThemeStyles(l.model.theme)
	frame, ok := l.composeStandardFrame(style)
	if !ok {
		return ""
	}
	return l.renderFrame(frame)
}

func (l uiViewLayout) composeStandardFrame(style uiStyles) (uiRenderFrame, bool) {
	m := l.model
	width := l.effectiveWidth()
	height := l.effectiveHeight()
	if width <= 0 || height <= 0 {
		return uiRenderFrame{}, false
	}
	frame := uiRenderFrame{width: width, height: height, statusLine: l.renderStatusLine(width, style), padToHeight: true, tailOnly: true}
	if m.surface() == uiSurfaceOngoingTranscript {
		frame.inputPane = l.renderInputLines(width, style)
		frame.inputCursor = l.inputPaneCursor(width)
		frame.queuePane = l.renderQueuedMessagesPane(width)
		frame.pickerPane = l.renderActivePicker(width)
	}
	frame.helpPane = l.renderHelpPane(width, helpPaneMaxLines(height, len(frame.inputPane), len(frame.queuePane), len(frame.pickerPane)), style)
	chatLines := height - len(frame.inputPane) - len(frame.queuePane) - len(frame.pickerPane) - len(frame.helpPane) - 1
	if chatLines < 1 {
		chatLines = 1
	}
	frame.chatPanel = l.renderChatPanel(width, chatLines, style)
	if m.worktrees.inputCursor.Visible {
		frame.inputCursor = m.worktrees.inputCursor
	}
	return frame, true
}

func (l uiViewLayout) renderNativeOngoing() string {
	if !l.model.windowSizeKnown {
		return ""
	}
	return l.renderNativeOngoingSized()
}

func (l uiViewLayout) renderNativeOngoingSized() string {
	m := l.model
	style := uiThemeStyles(m.theme)
	frame, status := l.composeNativeSizedFrame(style)
	if status == nativeFrameInvalid {
		return ""
	}
	return l.renderFrame(frame)
}

func (l uiViewLayout) renderFrame(frame uiRenderFrame) string {
	l.updateTerminalCursor(frame)
	frame.cursorFrameCount = l.realCursorFrameCount(frame)
	return frame.renderWithCursorVisibility(!l.shouldShowRealTerminalCursor(frame))
}

func (f uiRenderFrame) renderLines() []string {
	allLines := make([]string, 0, f.height)
	allLines = append(allLines, f.chatPanel...)
	allLines = append(allLines, f.pickerPane...)
	allLines = append(allLines, f.queuePane...)
	allLines = append(allLines, f.helpPane...)
	allLines = append(allLines, f.inputPane...)
	if strings.TrimSpace(f.statusLine) != "" || f.height > 0 {
		allLines = append(allLines, f.statusLine)
	}
	if f.padToHeight {
		for len(allLines) < f.height {
			allLines = append(allLines, padRight("", f.width))
		}
	}
	if len(allLines) > f.height {
		if f.tailOnly {
			allLines = allLines[len(allLines)-f.height:]
		} else {
			allLines = allLines[:f.height]
		}
	}
	return allLines
}

func (f uiRenderFrame) render() string {
	return f.renderWithCursorVisibility(true)
}

func (f uiRenderFrame) renderWithCursorVisibility(hideCursor bool) string {
	rendered := strings.Join(f.renderLines(), "\n")
	if hideCursor {
		return rendered + ansiHideCursor
	}
	if f.cursorFrameCount > 0 {
		return rendered + realCursorFrameMarker(f.cursorFrameCount)
	}
	return rendered
}

func realCursorFrameMarker(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat("\x1b[m", count)
}

func (l uiViewLayout) realCursorFrameCount(frame uiRenderFrame) int {
	if !l.shouldShowRealTerminalCursor(frame) {
		return 0
	}
	row := frame.inputCursor.Row
	if !frame.inputCursor.Absolute {
		row = len(frame.chatPanel) + len(frame.pickerPane) + len(frame.queuePane) + len(frame.helpPane) + frame.inputCursor.Row
	}
	return max(0, row)*max(1, frame.width) + max(0, frame.inputCursor.Col) + 1
}

func (l uiViewLayout) shouldShowRealTerminalCursor(frame uiRenderFrame) bool {
	return l.model.terminalCursor != nil && frame.inputCursor.Visible
}

func (l uiViewLayout) updateTerminalCursor(frame uiRenderFrame) {
	state := l.model.terminalCursor
	if state == nil {
		return
	}
	cursor := frame.inputCursor
	if !cursor.Visible {
		state.Clear()
		return
	}
	absoluteRow := cursor.Row
	if !cursor.Absolute {
		absoluteRow = len(frame.chatPanel) + len(frame.pickerPane) + len(frame.queuePane) + len(frame.helpPane) + cursor.Row
	}
	lines := frame.renderLines()
	trimStart := 0
	totalBeforeTrim := len(frame.chatPanel) + len(frame.pickerPane) + len(frame.queuePane) + len(frame.helpPane) + len(frame.inputPane)
	if strings.TrimSpace(frame.statusLine) != "" || frame.height > 0 {
		totalBeforeTrim++
	}
	if totalBeforeTrim > len(lines) {
		trimStart = totalBeforeTrim - len(lines)
	}
	absoluteRow -= trimStart
	if absoluteRow < 0 || absoluteRow >= len(lines) {
		state.Clear()
		return
	}
	anchorRow := len(lines) - 1
	state.Set(uiTerminalCursorPlacement{
		Visible:   true,
		CursorRow: absoluteRow,
		CursorCol: cursor.Col,
		AnchorRow: anchorRow,
		AltScreen: l.model.altScreenActive,
	})
}

type nativeFrameStatus uint8

const (
	nativeFrameInvalid nativeFrameStatus = iota
	nativeFrameReady
)

func (l uiViewLayout) composeNativeSizedFrame(style uiStyles) (uiRenderFrame, nativeFrameStatus) {
	m := l.model
	width := l.effectiveWidth()
	height := l.effectiveHeight()
	if width <= 0 {
		return uiRenderFrame{}, nativeFrameInvalid
	}
	if height <= 0 {
		return uiRenderFrame{}, nativeFrameInvalid
	}
	frame := uiRenderFrame{
		width:       width,
		height:      height,
		pickerPane:  l.renderActivePicker(width),
		queuePane:   l.renderQueuedMessagesPane(width),
		inputPane:   l.renderInputLines(width, style),
		inputCursor: l.inputPaneCursor(width),
		statusLine:  l.renderStatusLine(width, style),
		tailOnly:    true,
		padToHeight: false,
	}
	frame.helpPane = l.renderHelpPane(width, helpPaneMaxLines(height, len(frame.inputPane), len(frame.queuePane), len(frame.pickerPane)), style)
	availableStreamingLines := l.nativeStreamingViewportLineBudget(width, style)
	if availableStreamingLines < 0 {
		availableStreamingLines = 0
	}
	frame.chatPanel = l.renderNativeStreamingLines(width, availableStreamingLines, style)
	if m.worktrees.inputCursor.Visible {
		frame.inputCursor = m.worktrees.inputCursor
	}
	if m.nativeLiveRegionPad > 0 {
		pad := make([]string, 0, m.nativeLiveRegionPad+len(frame.chatPanel))
		for i := 0; i < m.nativeLiveRegionPad; i++ {
			pad = append(pad, padRight("", width))
		}
		frame.chatPanel = append(pad, frame.chatPanel...)
	}
	return frame, nativeFrameReady
}

func (l uiViewLayout) nativeOngoingLineCount() int {
	m := l.model
	style := uiThemeStyles(m.theme)
	width := l.effectiveWidth()
	if width <= 0 {
		return 0
	}
	inputLines := l.renderInputLines(width, style)
	queuedLines := l.renderQueuedMessagesPane(width)
	pickerLines := l.model.activePickerPresentation().lineCount
	height := l.effectiveHeight()
	helpLines := l.renderHelpPane(width, helpPaneMaxLines(height, len(inputLines), len(queuedLines), pickerLines), style)
	availableStreamingLines := l.nativeStreamingViewportLineBudget(width, style)
	if availableStreamingLines < 0 {
		availableStreamingLines = 0
	}
	streamingLines := l.renderNativeStreamingLines(width, availableStreamingLines, style)
	return len(inputLines) + len(queuedLines) + pickerLines + len(helpLines) + len(streamingLines) + 1
}

func (l uiViewLayout) nativeStreamingViewportLineBudget(width int, style uiStyles) int {
	if width <= 0 {
		return 0
	}
	inputLines := l.renderInputLines(width, style)
	queuedLines := l.renderQueuedMessagesPane(width)
	pickerLines := l.model.activePickerPresentation().lineCount
	height := l.effectiveHeight()
	helpLines := l.renderHelpPane(width, helpPaneMaxLines(height, len(inputLines), len(queuedLines), pickerLines), style)
	budget := height - pickerLines - len(queuedLines) - len(inputLines) - len(helpLines) - 1
	if budget < 0 {
		return 0
	}
	return budget
}

func (l uiViewLayout) renderNativeStreamingLines(width, maxLines int, style uiStyles) []string {
	if width <= 0 || maxLines <= 0 {
		return nil
	}
	pendingLines := l.renderNativePendingLines(width)
	hasStreaming := l.model.isBusy() || l.model.sawAssistantDelta
	if !hasStreaming && len(pendingLines) == 0 {
		return nil
	}
	streamText := ""
	if hasStreaming {
		streamText = l.model.view.OngoingStreamingText()
	}
	errText := l.model.view.OngoingErrorText()
	if len(pendingLines) == 0 && strings.TrimSpace(streamText) == "" && strings.TrimSpace(errText) == "" {
		return nil
	}
	lines := make([]string, 0, maxLines)
	includeDivider := len(nativeCommittedEntries(l.model.transcriptEntries)) > 0 && !l.model.nativeStreamingDividerFlushed
	if includeDivider {
		lines = append(lines, style.meta.Render(strings.Repeat("─", width)))
	}
	lines = append(lines, pendingLines...)
	if strings.TrimSpace(streamText) != "" {
		lines = append(lines, l.visibleNativeStreamingAssistantLines(streamText, width)...)
	}
	if strings.TrimSpace(errText) != "" {
		for _, line := range splitPlainLines(errText) {
			for _, wrapped := range wrapLine(line, width) {
				lines = append(lines, style.meta.Render(padRight("  "+wrapped, width)))
			}
		}
	}
	if len(lines) <= maxLines {
		return lines
	}
	if includeDivider && maxLines > 1 {
		content := lines[1:]
		result := []string{lines[0], content[0]}
		remaining := maxLines - len(result)
		if remaining <= 0 {
			return result[:maxLines]
		}
		if len(content) <= 1 {
			return result
		}
		tail := content[1:]
		if len(tail) > remaining {
			tail = tail[len(tail)-remaining:]
		}
		result = append(result, tail...)
		return result
	}
	return lines[len(lines)-maxLines:]
}

func (l uiViewLayout) visibleNativeStreamingAssistantLines(streamText string, width int) []string {
	assistantLines := renderNativeStreamingAssistantLines(streamText, l.model.theme, width)
	if len(assistantLines) == 0 {
		return nil
	}
	if width != l.model.nativeStreamingWidth || streamText != l.model.nativeStreamingText {
		return assistantLines
	}
	start := l.model.nativeStreamingFlushedLineCount
	if start <= 0 {
		return assistantLines
	}
	if start >= len(assistantLines) {
		return nil
	}
	return assistantLines[start:]
}

func renderNativeStreamingAssistantLines(streamText, theme string, width int) []string {
	_ = theme
	trimmed := strings.TrimSpace(streamText)
	if trimmed == "" {
		return nil
	}
	rawLines := splitPlainLines(streamText)
	if len(rawLines) > 0 && strings.TrimSpace(rawLines[len(rawLines)-1]) == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}
	lines := make([]string, 0, len(rawLines))
	firstChunk := true
	for _, line := range rawLines {
		for _, wrapped := range wrapLine(line, width) {
			prefix := "  "
			if firstChunk {
				prefix = "❮ "
				firstChunk = false
			}
			lines = append(lines, prefix+wrapped)
		}
	}
	return lines
}

func (l uiViewLayout) renderNativePendingLines(width int) []string {
	rendered := renderNativePendingToolSnapshot(l.model.transcriptEntries, l.model.theme, width, l.model.spinnerFrame)
	pendingEntries := nativePendingEntries(l.model.transcriptEntries)
	for _, entry := range pendingEntries {
		if isNativePendingToolRole(tui.TranscriptRoleToWire(entry.Role)) {
			continue
		}
		rendered = renderNativePendingOngoingSnapshot(pendingEntries, l.model.theme, width, l.model.spinnerFrame)
		break
	}
	if strings.TrimSpace(rendered) == "" {
		return nil
	}
	return strings.Split(rendered, "\n")
}

func isNativePendingToolRole(role string) bool {
	switch strings.TrimSpace(role) {
	case "tool_call", "tool_result", "tool_result_ok", "tool_result_error":
		return true
	default:
		return false
	}
}

func (l uiViewLayout) syncNativeLiveRegionState() {
	state := l.computeNativeLiveRegionState()
	m := l.model
	m.nativeLiveRegionPad = state.pad
	m.nativeLiveRegionLines = state.lines
	m.nativeStreamingActive = state.streamingActive
}

func (l uiViewLayout) computeNativeLiveRegionState() nativeLiveRegionState {
	m := l.model
	if m.view.Mode() != tui.ModeOngoing {
		return nativeLiveRegionState{}
	}
	streamingActiveNow := strings.TrimSpace(m.view.OngoingStreamingText()) != "" || strings.TrimSpace(m.view.OngoingErrorText()) != ""
	current := l.nativeOngoingLineCount()
	if len(nativeCommittedEntries(m.transcriptEntries)) == 0 {
		pad := l.effectiveHeight() - current
		if pad < 0 {
			pad = 0
		}
		return nativeLiveRegionState{pad: pad, lines: current + pad, streamingActive: streamingActiveNow}
	}
	if !streamingActiveNow {
		return nativeLiveRegionState{lines: current}
	}
	return nativeLiveRegionState{lines: current, streamingActive: true}
}
