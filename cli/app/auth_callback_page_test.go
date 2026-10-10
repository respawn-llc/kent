package app

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAuthCallbackPageInvalidPasteShowsTransientErrorAndStaysOpen(t *testing.T) {
	m, err := newAuthCallbackPageModel(authCallbackPageData{Theme: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	m.complete = func(context.Context, string) error {
		return errors.New("oauth callback is missing code")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bad")})
	m = next.(*authCallbackPageModel)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*authCallbackPageModel)
	if cmd == nil {
		t.Fatal("expected completion command")
	}
	msg := cmd()
	next, _ = m.Update(msg)
	m = next.(*authCallbackPageModel)
	if m.result.CallbackInput != "" {
		t.Fatalf("expected invalid paste to stay on page, result=%+v", m.result)
	}
	if m.currentScreen.ErrorText == "" {
		t.Fatal("invalid callback failure was not surfaced")
	}
}

func TestAuthCallbackPageBrowserWaitErrorShowsErrorAndStaysOpen(t *testing.T) {
	m, err := newAuthCallbackPageModel(authCallbackPageData{Theme: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	next, cmd := m.Update(authCallbackPageBrowserDoneMsg{err: errors.New("listener timed out")})
	m = next.(*authCallbackPageModel)
	if cmd == nil {
		t.Fatal("expected transient error command")
	}
	if m.result.Err != nil || m.result.Canceled {
		t.Fatalf("expected browser wait failure to keep page open, result=%+v", m.result)
	}
	if m.currentScreen.ErrorText == "" {
		t.Fatal("callback wait failure was not surfaced")
	}
}

func TestAuthCallbackPageEscGoesBack(t *testing.T) {
	m, err := newAuthCallbackPageModel(authCallbackPageData{Theme: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(*authCallbackPageModel)
	if !errors.Is(m.result.Err, ErrAuthBack) {
		t.Fatalf("expected Esc to go back, got %+v", m.result)
	}
}
