package app

import (
	"fmt"
	"testing"
	"time"

	"core/cli/app/internal/status"
	"core/server/auth"
	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSessionPickerScrollsAndSelects(t *testing.T) {
	now := time.Date(2026, time.February, 8, 12, 0, 0, 0, time.UTC)
	summaries := make([]clientui.SessionSummary, 0, 20)
	for i := 0; i < 20; i++ {
		summaries = append(summaries, clientui.SessionSummary{
			SessionID: fmt.Sprintf("s-%02d", i),
			UpdatedAt: now.Add(-time.Duration(i) * time.Minute),
		})
	}

	m := newSessionPickerModel(summaries, "dark", sessionPickerHeaderInfo{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(*sessionPickerModel)
	for i := 0; i < 16; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(*sessionPickerModel)
	}

	if m.cursor != 16 {
		t.Fatalf("cursor=%d want 16", m.cursor)
	}
	if m.offset == 0 {
		t.Fatalf("offset should advance for scroll")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*sessionPickerModel)
	if m.result.Session == nil {
		t.Fatal("expected selected session")
	}
	if m.result.Session.SessionID != summaries[15].SessionID {
		t.Fatalf("selected=%s want %s", m.result.Session.SessionID, summaries[15].SessionID)
	}
}

func TestSessionPickerEnterDefaultsToCreateNew(t *testing.T) {
	m := newSessionPickerModel(nil, "dark", sessionPickerHeaderInfo{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*sessionPickerModel)
	if !m.result.CreateNew {
		t.Fatal("expected default selection to create a new session")
	}
}

func TestSessionPickerNewHotkeyAndCancel(t *testing.T) {
	m := newSessionPickerModel(nil, "dark", sessionPickerHeaderInfo{})

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = next.(*sessionPickerModel)
	if !m.result.CreateNew {
		t.Fatal("expected create-new result")
	}

	m = newSessionPickerModel(nil, "dark", sessionPickerHeaderInfo{})
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = next.(*sessionPickerModel)
	if !m.result.Canceled {
		t.Fatal("expected canceled result")
	}
}

func TestSessionPickerIgnoresMouseSGRRunes(t *testing.T) {
	m := newSessionPickerModel(nil, "dark", sessionPickerHeaderInfo{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<64;81;40M[<65;80;39M")})
	m = next.(*sessionPickerModel)
	if m.result.CreateNew || m.result.Canceled || m.result.Session != nil {
		t.Fatalf("expected mouse sgr runes ignored, got result=%+v", m.result)
	}
}

func TestSessionPickerAuthLabelExamples(t *testing.T) {
	tests := []struct {
		name string
		info uiStatusAuthInfo
		want string
	}{
		{name: "no auth", info: uiStatusAuthInfo{Summary: "No Auth", Visible: true, Method: auth.MethodNone}, want: "No auth"},
		{name: "api key", info: uiStatusAuthInfo{Summary: "API Key ...1234", Visible: true, Method: auth.MethodAPIKey, Provider: "openai"}, want: "OpenAI API Key"},
		{name: "subscription", info: uiStatusAuthInfo{Summary: "user@example.com", Visible: true, Method: auth.MethodOAuth, Provider: "chatgpt-codex"}, want: "OpenAI Subscription"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.AuthDisplayLabel(tt.info); got != tt.want {
				t.Fatalf("auth label = %q, want %q", got, tt.want)
			}
		})
	}
}
