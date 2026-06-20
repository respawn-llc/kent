package app

import (
	"strings"
	"testing"

	"core/cli/tui"
	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDetailModeIgnoresHiddenMainInputKeys(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 90
	m.termHeight = 12
	m.windowSizeKnown = true
	m.input = "draft"
	m.inputCursor = -1
	m.layout().syncViewport()

	for i := 0; i < 12; i++ {
		m.forwardToView(tui.AppendTranscriptMsg{Role: "assistant", Text: strings.Repeat("line ", i+1)})
	}
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode, got %q", m.view.Mode())
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.input != "draft" {
		t.Fatalf("expected hidden main input unchanged in detail mode, got %q", m.input)
	}
}

func TestAskEventDefersWhileRollbackEditIsActive(t *testing.T) {
	reply := make(chan askReply, 1)
	m := newProjectedStaticUIModel(WithUIInitialTranscript([]UITranscriptEntry{
		{Role: "user", Text: "u1"},
		{Role: "assistant", Text: "a1"},
		{Role: "user", Text: "u2"},
	}))

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.rollback.isSelecting() || m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected rollback selection in detail mode, rollback=%t mode=%q", m.rollback.isSelecting(), m.view.Mode())
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.rollback.isEditing() || m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected rollback edit in ongoing mode, editing=%t mode=%q", m.rollback.isEditing(), m.view.Mode())
	}
	original := m.input

	m = updateUIModel(t, m, askEventMsg{event: askEvent{req: clientui.PendingPromptEvent{Question: "Proceed?", Suggestions: []string{"Yes", "No"}}, reply: reply}})
	if got := m.inputMode(); got != uiInputModeRollbackEdit {
		t.Fatalf("expected rollback edit to keep focus while ask is pending, got %q", got)
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" patched")})
	if m.input != original+" patched" {
		t.Fatalf("expected rollback edit input to keep accepting keys, got %q", m.input)
	}

	select {
	case got := <-reply:
		t.Fatalf("did not expect ask answered while rollback edit was active: %+v", got)
	default:
	}

	m.input = ""
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.rollback.isSelecting() || m.rollback.isEditing() || m.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected esc to return to rollback selection, rollback=%t editing=%t mode=%q", m.rollback.isSelecting(), m.rollback.isEditing(), m.view.Mode())
	}
	if got := m.inputMode(); got != uiInputModeRollbackSelection {
		t.Fatalf("expected rollback selection to keep focus while ask is pending, got %q", got)
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.rollback.isSelecting() || m.rollback.isEditing() || m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected rollback flow canceled back to ongoing, rollback=%t editing=%t mode=%q", m.rollback.isSelecting(), m.rollback.isEditing(), m.view.Mode())
	}
	if got := m.inputMode(); got != uiInputModeAsk {
		t.Fatalf("expected ask to become interactive after exiting rollback flow, got %q", got)
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	resp := <-reply
	if resp.response.SelectedOptionNumber != 1 {
		t.Fatalf("expected first option selected by default, got %+v", resp.response)
	}
}
