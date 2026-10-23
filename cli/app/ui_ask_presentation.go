package app

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	tea "github.com/charmbracelet/bubbletea"
)

type askAnswerFocus uint8

const (
	askFocusReading askAnswerFocus = iota
	askFocusAnswer
)

type askDedicatedPresentation struct {
	scroll         int
	focus          askAnswerFocus
	freeformReturn *askAnswerFocus
}

func (m *uiModel) askHasAnswerFocus() bool {
	return m.ask.dedicated == nil || m.ask.dedicated.focus == askFocusAnswer
}

func (m *uiModel) askHasVisibleAnswerFocus() bool {
	if m.ask.dedicated == nil {
		return true
	}
	size := m.terminalGeometry.Size()
	if !m.askHasAnswerFocus() || !m.terminalGeometry.canRenderOwnedFrame() {
		return false
	}
	viewport := m.layout().askDocument(size.width).viewport(m.ask.dedicated.scroll, askDocumentCapacity(size.height), m.ask.dedicated.focus)
	if m.ask.freeform {
		return viewport.cursor.Visible
	}
	for _, line := range viewport.lines {
		if line.prompt.Selected {
			return true
		}
	}
	return false
}

func (m *uiModel) reconcileAskPresentation() tea.Cmd {
	return m.reconcileAskPresentationForMessage(nil)
}

func (m *uiModel) reconcileAskPresentationForMessage(message *transcriptpb.Message) tea.Cmd {
	if !m.ask.hasCurrent() || (m.surface() != uiSurfaceOngoingTranscript && m.surface() != uiSurfaceAsk) {
		return nil
	}
	m.prepareAskPresentation(message)
	if m.ask.dedicated != nil && m.surface() != uiSurfaceAsk {
		return m.activateSurface(uiSurfaceAsk)
	}
	return nil
}

func (m *uiModel) prepareAskPresentation(message *transcriptpb.Message) {
	size := m.terminalGeometry.Size()
	if !m.terminalGeometry.canRenderOwnedFrame() {
		return
	}
	identity, known := m.currentQuestionRenderIdentity()
	if !known || m.ask.activeProjection == nil || m.ask.activeProjection.renderedAt != identity {
		return
	}
	if m.ask.dedicated == nil {
		if m.askFitsInline(message) {
			return
		}
		m.ask.dedicated = &askDedicatedPresentation{focus: askFocusReading}
		m.helpVisible = false
	}
	document := m.layout().askDocument(size.width)
	m.ask.dedicated.scroll = min(max(0, m.ask.dedicated.scroll), max(0, len(document.lines)-askDocumentCapacity(size.height)))
	if m.askHasAnswerFocus() && !m.ask.freeform {
		m.askController().revealFocusedAnswer(document, askDocumentCapacity(size.height))
	}
}

func askDocumentCapacity(height int) int {
	return max(1, height-3)
}

func (l uiViewLayout) composeAskFrame(width, height int, style uiStyles) uiRenderFrame {
	frame := uiRenderFrame{width: width, height: height, padToHeight: true}
	if !l.model.terminalGeometry.canRenderOwnedFrame() {
		return frame
	}
	document := l.askDocument(width)
	viewport := document.viewport(l.model.ask.dedicated.scroll, askDocumentCapacity(height), l.model.ask.dedicated.focus)
	pane := l.renderAskViewport(width, viewport, style)
	frame.inputPane = pane.Lines
	frame.inputCursor = pane.Cursor
	frame.statusLine = l.renderStatusLine(width, style)
	for len(frame.inputPane) < height-1 {
		frame.inputPane = append(frame.inputPane, padRight("", width))
	}
	return frame
}
