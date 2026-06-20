package app

import (
	"bytes"
	"core/cli/tui"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNativePSOverlayEscBalancesAltScreenAndAlternateScroll(t *testing.T) {
	var seqMu sync.Mutex
	var terminalSequences []string
	originalWriteTerminalSequence := writeTerminalSequence
	writeTerminalSequence = func(sequence string) {
		seqMu.Lock()
		terminalSequences = append(terminalSequences, sequence)
		seqMu.Unlock()
	}
	defer func() {
		writeTerminalSequence = originalWriteTerminalSequence
	}()
	sequenceLogSnapshot := func() string {
		seqMu.Lock()
		defer seqMu.Unlock()
		return strings.Join(terminalSequences, "")
	}

	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
	)
	model.input = "/ps"

	program := startNativeProgram(t, model, out)

	time.Sleep(40 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	time.Sleep(20 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	// Gate on the mutex-guarded alternate-scroll enable sequence (not live model state) so the
	// overlay is open before it is closed, without racing the program goroutine's model writes.
	waitForTestCondition(t, 2*time.Second, "/ps overlay to enable alternate-scroll", func() bool {
		return strings.Contains(sequenceLogSnapshot(), "\x1b[?1007h")
	})
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForTestCondition(t, 2*time.Second, "/ps overlay to close and disable alternate-scroll", func() bool {
		return !model.processList.open && model.surface() != uiSurfaceProcessList && model.view.Mode() == tui.ModeOngoing &&
			strings.Contains(sequenceLogSnapshot(), "\x1b[?1007l")
	})
	program.QuitAndWait(2 * time.Second)

	raw := out.String()
	enterAlt := strings.Count(raw, "\x1b[?1049h")
	exitAlt := strings.Count(raw, "\x1b[?1049l")
	if enterAlt != exitAlt {
		t.Fatalf("expected balanced /ps alt-screen enter/exit sequences, enter=%d exit=%d", enterAlt, exitAlt)
	}
	if enterAlt == 0 {
		t.Fatal("expected /ps overlay in native mode to enter alt-screen under auto policy")
	}
	sequenceLog := sequenceLogSnapshot()
	enableAltScroll := strings.Count(sequenceLog, "\x1b[?1007h")
	disableAltScroll := strings.Count(sequenceLog, "\x1b[?1007l")
	if enableAltScroll != 1 || disableAltScroll != 1 {
		t.Fatalf("expected /ps overlay to pair alternate-scroll enable/disable, enable=%d disable=%d log=%q", enableAltScroll, disableAltScroll, sequenceLog)
	}
}

func TestNativePSOverlayUsesFixedAltScreen(t *testing.T) {
	var seqMu sync.Mutex
	var terminalSequences []string
	originalWriteTerminalSequence := writeTerminalSequence
	writeTerminalSequence = func(sequence string) {
		seqMu.Lock()
		terminalSequences = append(terminalSequences, sequence)
		seqMu.Unlock()
	}
	defer func() {
		writeTerminalSequence = originalWriteTerminalSequence
	}()
	sequenceLogSnapshot := func() string {
		seqMu.Lock()
		defer seqMu.Unlock()
		return strings.Join(terminalSequences, "")
	}

	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
	)
	model.input = "/ps"

	program := startNativeProgram(t, model, out)

	time.Sleep(40 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	time.Sleep(20 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	// Gate on the mutex-guarded alternate-scroll enable sequence (not live model state) so the
	// overlay is open before it is closed, without racing the program goroutine's model writes.
	waitForTestCondition(t, 2*time.Second, "/ps overlay to enable alternate-scroll", func() bool {
		return strings.Contains(sequenceLogSnapshot(), "\x1b[?1007h")
	})
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForTestCondition(t, 2*time.Second, "/ps overlay to disable alternate-scroll", func() bool {
		return strings.Contains(sequenceLogSnapshot(), "\x1b[?1007l")
	})
	program.QuitAndWait(2 * time.Second)

	raw := out.String()
	enterAlt := strings.Count(raw, "\x1b[?1049h")
	exitAlt := strings.Count(raw, "\x1b[?1049l")
	if enterAlt == 0 || enterAlt != exitAlt {
		t.Fatalf("expected balanced /ps alt-screen enter/exit sequences, enter=%d exit=%d raw=%q", enterAlt, exitAlt, raw)
	}
	sequenceLog := sequenceLogSnapshot()
	enableAltScroll := strings.Count(sequenceLog, "\x1b[?1007h")
	disableAltScroll := strings.Count(sequenceLog, "\x1b[?1007l")
	if enableAltScroll != 1 || disableAltScroll != 1 {
		t.Fatalf("expected /ps overlay to pair alternate-scroll enable/disable, enable=%d disable=%d log=%q", enableAltScroll, disableAltScroll, sequenceLog)
	}
}
