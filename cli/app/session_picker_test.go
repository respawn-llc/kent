package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"builder/shared/clientui"
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

func TestSessionPickerHeaderRendersStatusReportBox(t *testing.T) {
	m := newSessionPickerModel(nil, "dark", sessionPickerHeaderInfo{
		Version:    "1.2.3",
		CWD:        "~/Developer/builder-cli",
		Branch:     "feature/session-picker",
		Model:      "gpt-5 high",
		OwnsServer: true,
	})
	m.width = 120

	plain := stripANSIAndTrimRight(m.renderHeader())
	for _, want := range []string{
		"┌",
		"Builder v1.2.3",
		"~/Developer/builder-cli · feature/session-picker · gpt-5 high",
		"Server owned by this terminal",
		"└",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected header to contain %q, got %q", want, plain)
		}
	}
}

func TestSessionPickerHeaderReflowsMainInfoWhenNarrow(t *testing.T) {
	m := newSessionPickerModel(nil, "dark", sessionPickerHeaderInfo{
		Version:       "1.2.3",
		CWD:           "~/repo",
		Branch:        "main",
		Model:         "gpt-5.1-ultra high",
		ServerAddress: "127.0.0.1:53082",
	})
	m.width = 35

	plain := stripANSIAndTrimRight(m.renderHeader())
	if strings.Contains(plain, "~/repo · main · gpt-5.1-ultra high") {
		t.Fatalf("expected narrow header to reflow main info, got %q", plain)
	}
	for _, want := range []string{
		"Builder v1.2.3",
		"~/repo",
		"main",
		"gpt-5.1-ultra high",
		"Connected to 127.0.0.1:53082",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected narrow header to contain %q, got %q", want, plain)
		}
	}
}

func TestSessionPickerHeaderRendersRemoteServerStatus(t *testing.T) {
	withTrueColor(t)
	m := newSessionPickerModel(nil, "dark", sessionPickerHeaderInfo{
		Version:       "1.2.3",
		CWD:           "~/repo",
		Model:         "gpt-5 high",
		ServerAddress: "127.0.0.1:53082",
	})
	m.width = 80

	raw := m.renderHeader()
	successEscape := strings.Replace(foregroundTrueColorEscape(statusGreenColor().Dark.TrueColor), "\x1b[", "\x1b[1;", 1)
	if !strings.Contains(raw, successEscape+"Connected to 127.0.0.1:53082") {
		t.Fatalf("expected remote server line to use success color, got %q", raw)
	}
	if plain := stripANSIAndTrimRight(raw); !strings.Contains(plain, "Connected to 127.0.0.1:53082") {
		t.Fatalf("expected remote server copy, got %q", plain)
	}
}

func TestSessionPickerHeaderRendersMissingRemoteAddressFallback(t *testing.T) {
	m := newSessionPickerModel(nil, "dark", sessionPickerHeaderInfo{
		Version: "1.2.3",
		CWD:     "~/repo",
		Model:   "gpt-5 high",
	})
	m.width = 80

	plain := stripANSIAndTrimRight(m.renderHeader())
	if !strings.Contains(plain, "Connected to Server") {
		t.Fatalf("expected remote server fallback copy, got %q", plain)
	}
}

func TestSessionPickerHeaderTinyWidthKeepsRowsVisible(t *testing.T) {
	m := newSessionPickerModel([]clientui.SessionSummary{{SessionID: "s-1", UpdatedAt: time.Now()}}, "dark", sessionPickerHeaderInfo{
		Version:       "1.2.3",
		CWD:           "~/very/long/path/to/repo",
		Branch:        "feature/very-long-branch",
		Model:         "gpt-5.1-ultra high",
		ServerAddress: "127.0.0.1:53082",
	})
	m.width = 8
	m.height = 8

	if got := m.visibleLineBudget(); got < 1 {
		t.Fatalf("visible line budget = %d, want at least 1", got)
	}
	view := stripANSIAndTrimRight(m.View())
	if !strings.Contains(view, "◈") {
		t.Fatalf("expected at least selected row visible at tiny width, got %q", view)
	}
}
