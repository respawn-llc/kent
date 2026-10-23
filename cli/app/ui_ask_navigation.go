package app

import tea "github.com/charmbracelet/bubbletea"

func (c uiAskController) handleDedicatedNavigation(msg tea.KeyMsg) bool {
	m := c.model
	presentation := m.ask.dedicated
	if presentation == nil || m.surface() != uiSurfaceAsk {
		return false
	}
	document := m.layout().askDocument(m.layout().effectiveWidth())
	capacity := askDocumentCapacity(m.layout().effectiveHeight())
	bottom := max(0, len(document.lines)-capacity)
	switch msg.Type {
	case tea.KeyPgUp, tea.KeyPgDown:
		presentation.focus = askFocusReading
		delta := capacity
		if msg.Type == tea.KeyPgUp {
			delta = -delta
		}
		presentation.scroll = min(bottom, max(0, presentation.scroll+delta))
		return true
	case tea.KeyUp, tea.KeyDown:
		delta := 1
		if msg.Type == tea.KeyUp {
			delta = -1
		}
		if presentation.focus == askFocusReading {
			if delta > 0 && presentation.scroll == bottom {
				presentation.focus = askFocusAnswer
				m.ask.freeform = askOptionCount(m.ask.current.prompt) == 0
				m.ask.cursor = 0
				c.chooseFocusedApproval()
				document = m.layout().askDocument(m.layout().effectiveWidth())
				c.revealFocusedAnswer(document, capacity)
			} else {
				presentation.scroll = min(bottom, max(0, presentation.scroll+delta))
			}
			return true
		}
		if m.ask.freeform {
			if m.moveAskCursorVertical(delta) {
				c.revealFocusedAnswer(m.layout().askDocument(m.layout().effectiveWidth()), capacity)
			} else {
				presentation.focus = askFocusReading
				presentation.scroll = min(bottom, max(0, presentation.scroll+delta))
			}
			return true
		}
		next := m.ask.cursor + delta
		if next >= 0 && next < askOptionCount(m.ask.current.prompt) {
			m.ask.cursor = next
			c.chooseFocusedApproval()
			c.revealFocusedAnswer(document, capacity)
		} else {
			presentation.focus = askFocusReading
			presentation.scroll = min(bottom, max(0, presentation.scroll+delta))
		}
		return true
	}
	return false
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
		!transcriptPromptIsApproval(m.ask.current.prompt))
}

func (m *uiModel) focusDedicatedAskEditor() {
	if m.ask.dedicated == nil || !m.askEditorAcceptsInput() {
		return
	}
	if !m.ask.freeform {
		focus := askFocusReading
		m.ask.dedicated.freeformReturn = &focus
		m.ask.freeform = true
	}
	m.ask.dedicated.focus = askFocusAnswer
	document := m.layout().askDocument(m.layout().effectiveWidth())
	m.ask.dedicated.scroll = max(0, len(document.lines)-askDocumentCapacity(m.layout().effectiveHeight()))
}

func (c uiAskController) revealFocusedAnswer(document askDocument, capacity int) {
	m := c.model
	var target askDocumentRange
	if m.ask.freeform {
		if document.editor == nil {
			return
		}
		target = *document.editor
		if document.cursor.Visible {
			target = askDocumentRange{start: document.cursor.Row, end: document.cursor.Row + 1}
		}
	} else {
		var exists bool
		target, exists = document.options[m.ask.cursor]
		if !exists {
			return
		}
	}
	presentation := m.ask.dedicated
	if target.start < presentation.scroll {
		presentation.scroll = target.start
	} else if target.end > presentation.scroll+capacity {
		presentation.scroll = max(target.start, target.end-capacity)
	}
	presentation.scroll = min(presentation.scroll, max(0, len(document.lines)-capacity))
}
