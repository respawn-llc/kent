package app

import (
	"bytes"
	"core/cli/tui"
	"core/server/runtime"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNativeProgramClearsResidualLivePadAfterStreamingCommit(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	program := startNativeProgram(t, model, out)
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 20})
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "line1\nline2"}))
	time.Sleep(30 * time.Millisecond)
	program.Send(tui.SetConversationMsg{Entries: []tui.TranscriptEntry{}, Ongoing: ""})
	time.Sleep(30 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	program.Wait(2 * time.Second)

	if model.nativeLiveRegionPad <= 0 {
		t.Fatalf("expected fresh conversation to restore native live region pad after streaming commit, got %d", model.nativeLiveRegionPad)
	}
	if model.nativeStreamingActive {
		t.Fatal("expected native streaming active flag cleared after commit")
	}
}
