package app

import (
	"encoding/json"
	"testing"

	"core/cli/tui"
	"core/server/session"
	"core/shared/rollbacktarget"
)

func userMessageSeqAt(t *testing.T, store *session.Store, n int) int64 {
	t.Helper()
	window, err := store.ReadRecentEvents(10_000)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	visible := 0
	for _, evt := range window.Events {
		if evt.Kind != "message" {
			continue
		}
		var msg struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(evt.Payload, &msg); err != nil {
			continue
		}
		if msg.Role == "user" {
			visible++
			if visible == n {
				return evt.Seq
			}
		}
	}
	t.Fatalf("user message %d not found among %d events", n, len(window.Events))
	return 0
}

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
	return rollbacktarget.EncodeUserMessageSeq(int64(selectedTranscriptEntry + 1))
}

func seedTestRollbackTargets(m *uiModel) {
	if m == nil {
		return
	}
	seeded := false
	seed := func(entries []tui.TranscriptEntry, base int) {
		for i := range entries {
			if entries[i].Role == tui.TranscriptRoleUser && entries[i].RollbackTargetID == "" {
				entries[i].RollbackTargetID = rollbackTargetIDForTestSelection(base + i)
				seeded = true
			}
		}
	}
	seed(m.transcriptEntries, m.transcriptBaseOffset)
	seed(m.detailTranscript.entries, m.detailTranscript.offset)
	if seeded {
		m.refreshRollbackCandidates()
	}
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
