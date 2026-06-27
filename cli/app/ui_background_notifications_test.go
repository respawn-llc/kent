package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/runtime"
	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

type blockingProcessClient struct {
	listStarted chan struct{}
	release     chan struct{}
}

func (c *blockingProcessClient) ListProcesses(context.Context) ([]clientui.BackgroundProcess, error) {
	select {
	case c.listStarted <- struct{}{}:
	default:
	}
	<-c.release
	return nil, nil
}

func (c *blockingProcessClient) KillProcess(context.Context, string) error {
	return errors.New("not implemented")
}

func (c *blockingProcessClient) InlineOutput(context.Context, string, int) (string, string, error) {
	return "", "", errors.New("not implemented")
}

func TestBackgroundCompletionDoesNotBlockOnHiddenProcessRefresh(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 120
	m.termHeight = 30
	m.windowSizeKnown = true
	client := &blockingProcessClient{
		listStarted: make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
	m.processClient = client

	done := make(chan tea.Cmd, 1)
	go func() {
		done <- m.runtimeAdapter().applyProjectedRuntimeEvent(projectRuntimeEvent(runtime.Event{
			Kind: runtime.EventBackgroundUpdated,
			Background: &runtime.BackgroundShellEvent{
				Type:       "completed",
				ID:         "1000",
				State:      "completed",
				NoticeText: "Background shell 1000 completed.\nOutput:\nhello",
			},
		})).cmd
	}()

	var cmd tea.Cmd
	select {
	case cmd = <-done:
	case <-time.After(200 * time.Millisecond):
		close(client.release)
		<-done
		t.Fatal("background completion blocked while hidden process list refresh was in flight")
	}

	select {
	case <-client.listStarted:
		t.Fatal("did not expect hidden process list to synchronously refresh on background completion")
	default:
	}
	close(client.release)
	_ = collectCmdMessages(t, cmd)

	if m.transientStatus != "background shell 1000 completed" {
		t.Fatalf("transient status = %q, want background completion notice", m.transientStatus)
	}
	if len(m.transcriptEntries) != 1 {
		t.Fatalf("transcript entry count = %d, want 1", len(m.transcriptEntries))
	}
	if m.transcriptEntries[0].Text != "Background shell 1000 completed.\nOutput:\nhello" {
		t.Fatalf("unexpected transcript entry %+v", m.transcriptEntries[0])
	}
}
