package app

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func completeProcessRefreshForTest(t *testing.T, m *uiModel) *uiModel {
	t.Helper()
	if m == nil || m.processClient == nil {
		t.Fatal("process client is required")
	}
	entries, err := m.processClient.ListProcesses(context.Background())
	next, _ := m.Update(processListRefreshDoneMsg{
		token:   m.processList.refreshToken,
		entries: entries,
		err:     err,
	})
	return next.(*uiModel)
}

func refreshProcessEntriesForTest(t *testing.T, m *uiModel) {
	t.Helper()
	if m == nil || m.processClient == nil {
		t.Fatal("process client is required")
	}
	entries, err := m.processClient.ListProcesses(context.Background())
	if err != nil {
		t.Fatalf("ListProcesses: %v", err)
	}
	m.applyProcessEntries(entries)
}

func applyProcessActionCommandForTest(t *testing.T, m *uiModel, cmd tea.Cmd) *uiModel {
	t.Helper()
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if done, ok := msg.(processActionDoneMsg); ok {
			next, _ := m.Update(done)
			return next.(*uiModel)
		}
	}
	t.Fatalf("expected process action completion, got %+v", msgs)
	return m
}
