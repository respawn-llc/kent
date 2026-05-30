package app

import "builder/shared/rollbacktarget"

func testActiveAsk(m *uiModel) *askEvent {
	if m == nil {
		return nil
	}
	return m.ask.current
}

func testSetActiveAsk(m *uiModel, event *askEvent) {
	if m == nil {
		return
	}
	m.ask.currentToken = nextNonZeroToken(m.ask.currentToken)
	m.ask.current = event
	if event != nil {
		m.setInputMode(uiInputModeAsk)
		return
	}
	m.restorePrimaryInputMode()
}

func testAskFreeform(m *uiModel) bool {
	if m == nil {
		return false
	}
	return m.ask.freeform
}

func testAskCursor(m *uiModel) int {
	if m == nil {
		return 0
	}
	return m.ask.cursor
}

func testAskInput(m *uiModel) string {
	if m == nil {
		return ""
	}
	return m.ask.input
}

func testSetAskInput(m *uiModel, input string) {
	if m == nil {
		return
	}
	m.ask.input = input
}

func testAskInputCursor(m *uiModel) int {
	if m == nil {
		return 0
	}
	return m.ask.inputCursor
}

func testSetAskInputCursor(m *uiModel, cursor int) {
	if m == nil {
		return
	}
	m.ask.inputCursor = cursor
}

func testAskQueue(m *uiModel) []askEvent {
	if m == nil {
		return nil
	}
	return m.ask.queue
}

func testProcessListOpen(m *uiModel) bool {
	if m == nil {
		return false
	}
	return m.processList.open
}

func testProcessListSurfaceActive(m *uiModel) bool {
	if m == nil {
		return false
	}
	return m.surface() == uiSurfaceProcessList
}

func testRollbackSelecting(m *uiModel) bool {
	if m == nil {
		return false
	}
	return m.rollback.isSelecting()
}

func testRollbackEditing(m *uiModel) bool {
	if m == nil {
		return false
	}
	return m.rollback.isEditing()
}

func testSetRollbackEditing(m *uiModel, selection int, selectedTranscriptEntry int) {
	if m == nil {
		return
	}
	m.rollback.phase = uiRollbackPhaseEditing
	m.rollback.selection = selection
	m.rollback.selectedTranscriptEntry = selectedTranscriptEntry
	m.rollback.selectedTargetID = rollbackTargetIDForTestSelection(selectedTranscriptEntry)
	m.setInputMode(uiInputModeRollbackEdit)
}

func rollbackTargetIDForTestSelection(selectedTranscriptEntry int) string {
	if selectedTranscriptEntry < 0 {
		return ""
	}
	return rollbacktarget.EncodeUserMessageIndex(selectedTranscriptEntry + 1)
}

func testRollbackSelection(m *uiModel) int {
	if m == nil {
		return 0
	}
	return m.rollback.selection
}

func testRollbackCandidates(m *uiModel) []rollbackCandidate {
	if m == nil {
		return nil
	}
	return m.rollback.candidates
}

func testRollbackSelectionSurfaceActive(m *uiModel) bool {
	if m == nil {
		return false
	}
	return m.surface() == uiSurfaceRollbackSelection
}
