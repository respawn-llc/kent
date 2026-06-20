package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAuthCallbackPageEscCancels(t *testing.T) {
	m := newAuthCallbackPageModel(authCallbackPageData{Theme: "dark"})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(*authCallbackPageModel)
	if !m.result.Canceled {
		t.Fatalf("expected Esc to cancel, got %+v", m.result)
	}
}
