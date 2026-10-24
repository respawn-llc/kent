package app

import (
	tuiinput "core/cli/tui/input"

	tea "github.com/charmbracelet/bubbletea"
)

func (c uiAskController) handleDedicatedNavigation(msg tea.KeyMsg) bool {
	m := c.model
	presentation := m.ask.dedicated
	if presentation == nil || m.surface() != uiSurfaceAsk {
		return false
	}
	document := m.layout().askDocument(m.layout().effectiveWidth())
	capacity := askDocumentCapacity(m.layout().effectiveHeight())
	delta := 0
	switch msg.Type {
	case tea.KeyPgUp, tea.KeyPgDown:
		delta = capacity
		if msg.Type == tea.KeyPgUp {
			delta = -delta
		}
	case tea.KeyUp, tea.KeyDown:
		delta = 1
		if msg.Type == tea.KeyUp {
			delta = -1
		}
		if m.ask.freeform && m.askHasAnswerFocus() {
			if m.moveAskCursorVertical(delta) {
				document = m.layout().askDocument(m.layout().effectiveWidth())
				presentation.row = document.cursor.Row
				c.revealAskRow(document, capacity)
				return true
			}
		}
	default:
		return false
	}
	if m.ask.freeform && m.askHasAnswerFocus() {
		presentation.row = document.cursor.Row
	}
	presentation.row = min(max(0, presentation.row+delta), max(0, len(document.lines)-1))
	c.focusReadingRow(document)
	c.revealAskRow(document, capacity)
	return true
}

func (c uiAskController) focusReadingRow(document askDocument) {
	m := c.model
	presentation := m.ask.dedicated
	presentation.focus = askFocusReading
	if m.ask.freeform {
		if document.editor != nil && presentation.row >= document.editor.start && presentation.row < document.editor.end {
			// The Field remains the authority for positions within wrapped editor rows.
			presentation.focus = askFocusAnswer
			field := tuiinput.NewField()
			field.Editor = m.ask.editor
			field.Prefix = document.lines[document.editor.start].prompt.InputPrefix
			field.MoveToRow(m.layout().effectiveWidth(), presentation.row-document.editor.start)
			m.ask.editor = field.Editor
		}
		return
	}
	for option, rows := range document.options {
		if presentation.row >= rows.start && presentation.row < rows.end {
			presentation.focus = askFocusAnswer
			m.ask.cursor = option
			c.chooseFocusedApproval()
			return
		}
	}
}

func (c uiAskController) chooseFocusedApproval() {
	m := c.model
	if m.ask.freeformMode == askFreeformModeApprovalCommentary {
		return
	}
	if decision, ok := selectedApprovalDecision(m.ask.current.prompt, m.ask.cursor); ok {
		m.ask.chosenApproval = &decision
	}
}

func (m *uiModel) askEditorAcceptsInput() bool {
	return m.ask.freeform || (m.surface() == uiSurfaceAsk &&
		m.ask.dedicated != nil && m.ask.dedicated.focus == askFocusReading &&
		(!transcriptPromptIsApproval(m.ask.current.prompt) || m.ask.chosenApproval != nil))
}

func (m *uiModel) focusDedicatedAskEditor() {
	if m.ask.dedicated == nil || !m.askEditorAcceptsInput() {
		return
	}
	if !m.ask.freeform {
		if transcriptPromptIsApproval(m.ask.current.prompt) {
			m.ask.freeformMode = askFreeformModeApprovalCommentary
		}
		focus := askFocusReading
		m.ask.dedicated.freeformReturn = &focus
		m.ask.freeform = true
	}
	m.ask.dedicated.focus = askFocusAnswer
	document := m.layout().askDocument(m.layout().effectiveWidth())
	m.ask.dedicated.row = document.cursor.Row
	m.ask.dedicated.scroll = max(0, len(document.lines)-askDocumentCapacity(m.layout().effectiveHeight()))
}

func (c uiAskController) revealAskRow(document askDocument, capacity int) {
	presentation := c.model.ask.dedicated
	if presentation.row < presentation.scroll {
		presentation.scroll = presentation.row
	} else if presentation.row >= presentation.scroll+capacity {
		presentation.scroll = presentation.row - capacity + 1
	}
	presentation.scroll = min(max(0, presentation.scroll), max(0, len(document.lines)-capacity))
}
