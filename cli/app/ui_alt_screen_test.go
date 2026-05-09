package app

import (
	"strings"
	"testing"

	"builder/cli/tui"
	"builder/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func TestToggleTranscriptModeUsesFixedDetailAltScreen(t *testing.T) {
	m := newProjectedStaticUIModel()

	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("mode=%q want ongoing", m.view.Mode())
	}
	if m.altScreenActive {
		t.Fatal("expected initial alt-screen inactive")
	}

	cmd := m.toggleTranscriptMode()
	if cmd == nil {
		t.Fatal("expected alt-screen command when toggling into detail")
	}
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("mode=%q want detail", m.view.Mode())
	}
	if !m.altScreenActive {
		t.Fatal("expected alt-screen active when entering detail")
	}

	cmd = m.toggleTranscriptMode()
	if cmd == nil {
		t.Fatal("expected alt-screen command when toggling out of detail")
	}
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("mode=%q want ongoing", m.view.Mode())
	}
	if m.altScreenActive {
		t.Fatal("expected alt-screen inactive after leaving detail")
	}
}

func TestNativeReplayCmdForModeTransitionPreservesAppendOnlyWhenScreenNotReplaced(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 80
	initial := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{{Role: "assistant", Lines: []string{"before"}}}}
	updated := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{{Role: "assistant", Lines: []string{"before"}}, {Role: "assistant", Lines: []string{"after"}}}}
	m.nativeProjection = updated
	m.nativeRenderedProjection = initial
	m.nativeRenderedSnapshot = initial.Render(tui.TranscriptDivider)

	cmd := m.nativeReplayCmdForModeTransition(tui.ModeDetail, tui.ModeOngoing, true)
	if cmd == nil {
		t.Fatal("expected append-only replay command")
	}
	msgs := collectCmdMessages(t, cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected append-only replay without clear-screen, got %d message(s)", len(msgs))
	}
	flush, ok := msgs[0].(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", msgs[0])
	}
	if got := stripANSIText(flush.Text); got != "after" {
		t.Fatalf("expected append-only replay of deferred delta, got %q", got)
	}
}

func TestFullscreenSurfaceOpenClosePolicy(t *testing.T) {
	tests := []struct {
		name         string
		wantSurface  uiSurface
		wantOpenMode tui.Mode
		open         func(t *testing.T, m *uiModel) tea.Cmd
		close        func(m *uiModel) tea.Cmd
	}{
		{
			name:         "status",
			wantSurface:  uiSurfaceStatus,
			wantOpenMode: tui.ModeOngoing,
			open: func(_ *testing.T, m *uiModel) tea.Cmd {
				m.openStatusOverlay()
				return m.pushStatusOverlayIfNeeded()
			},
			close: func(m *uiModel) tea.Cmd {
				cmd := m.popStatusOverlayIfNeeded()
				m.closeStatusOverlay()
				return cmd
			},
		},
		{
			name:         "process",
			wantSurface:  uiSurfaceProcessList,
			wantOpenMode: tui.ModeOngoing,
			open: func(_ *testing.T, m *uiModel) tea.Cmd {
				m.openProcessList()
				return m.pushProcessOverlayIfNeeded()
			},
			close: func(m *uiModel) tea.Cmd {
				cmd := m.popProcessOverlayIfNeeded()
				m.closeProcessList()
				return cmd
			},
		},
		{
			name:         "goal",
			wantSurface:  uiSurfaceGoal,
			wantOpenMode: tui.ModeOngoing,
			open: func(_ *testing.T, m *uiModel) tea.Cmd {
				m.openGoalOverlay(nil, nil)
				return m.pushGoalOverlayIfNeeded()
			},
			close: func(m *uiModel) tea.Cmd {
				cmd := m.popGoalOverlayIfNeeded()
				m.closeGoalOverlay()
				return cmd
			},
		},
		{
			name:         "worktree",
			wantSurface:  uiSurfaceWorktree,
			wantOpenMode: tui.ModeOngoing,
			open: func(_ *testing.T, m *uiModel) tea.Cmd {
				m.openWorktreeOverlay(uiWorktreeOpenIntent{})
				return m.pushWorktreeOverlayIfNeeded()
			},
			close: func(m *uiModel) tea.Cmd {
				cmd := m.popWorktreeOverlayIfNeeded()
				m.closeWorktreeOverlay()
				return cmd
			},
		},
		{
			name:         "rollback",
			wantSurface:  uiSurfaceRollbackSelection,
			wantOpenMode: tui.ModeDetail,
			open: func(t *testing.T, m *uiModel) tea.Cmd {
				m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt"}}
				m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, TotalEntries: 1})
				if !m.startRollbackSelectionMode() {
					t.Fatal("expected rollback selection to start")
				}
				return m.pushRollbackOverlayIfNeeded()
			},
			close: func(m *uiModel) tea.Cmd {
				cmd := m.popRollbackOverlayIfNeeded()
				m.stopRollbackSelectionMode()
				return cmd
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newProjectedStaticUIModel()
			m.windowSizeKnown = true
			m.termWidth = 80
			m.termHeight = 24

			openCmd := tt.open(t, m)
			if openCmd == nil {
				t.Fatal("expected open command")
			}
			if got := m.surface(); got != tt.wantSurface {
				t.Fatalf("surface=%q want %q", got, tt.wantSurface)
			}
			if got := m.view.Mode(); got != tt.wantOpenMode {
				t.Fatalf("mode=%q want %q", got, tt.wantOpenMode)
			}
			if !m.altScreenActive {
				t.Fatal("expected alt-screen active after open")
			}

			closeCmd := tt.close(m)
			if closeCmd == nil {
				t.Fatal("expected close command")
			}
			if got := m.surface(); got != uiSurfaceOngoingTranscript {
				t.Fatalf("surface=%q want ongoing", got)
			}
			if got := m.view.Mode(); got != tui.ModeOngoing {
				t.Fatalf("mode=%q want ongoing", got)
			}
			if m.altScreenActive {
				t.Fatal("expected alt-screen inactive after close")
			}
		})
	}
}

func TestOngoingOverlaySurfacesDoNotEnableAlternateScroll(t *testing.T) {
	tests := []struct {
		name    string
		surface uiSurface
	}{
		{name: "status", surface: uiSurfaceStatus},
		{name: "goal", surface: uiSurfaceGoal},
		{name: "worktree", surface: uiSurfaceWorktree},
		{name: "process", surface: uiSurfaceProcessList},
	}

	originalWriteTerminalSequence := writeTerminalSequence
	defer func() { writeTerminalSequence = originalWriteTerminalSequence }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sequenceLog := strings.Builder{}
			writeTerminalSequence = func(sequence string) {
				sequenceLog.WriteString(sequence)
			}

			m := newProjectedStaticUIModel()
			m.windowSizeKnown = true
			m.termWidth = 80
			m.termHeight = 24
			if got := m.view.Mode(); got != tui.ModeOngoing {
				t.Fatalf("mode=%q want ongoing", got)
			}

			openCmd := m.activateSurface(tt.surface)
			if openCmd == nil {
				t.Fatal("expected open command")
			}
			_ = collectCmdMessages(t, openCmd)
			closeCmd := m.restoreTranscriptSurface()
			if closeCmd == nil {
				t.Fatal("expected close command")
			}
			_ = collectCmdMessages(t, closeCmd)

			if got := sequenceLog.String(); strings.Contains(got, "\x1b[?1007h") || strings.Contains(got, "\x1b[?1007l") {
				t.Fatalf("ongoing overlay emitted alternate-scroll sequence: %q", got)
			}
			if got := m.view.Mode(); got != tui.ModeOngoing {
				t.Fatalf("mode=%q want ongoing", got)
			}
			if m.altScreenActive {
				t.Fatal("expected alt-screen inactive after restore")
			}
		})
	}
}

func TestDetailOverlaySurfacesPreserveAlternateScroll(t *testing.T) {
	tests := []struct {
		name    string
		surface uiSurface
	}{
		{name: "status", surface: uiSurfaceStatus},
		{name: "goal", surface: uiSurfaceGoal},
		{name: "worktree", surface: uiSurfaceWorktree},
		{name: "process", surface: uiSurfaceProcessList},
	}

	originalWriteTerminalSequence := writeTerminalSequence
	defer func() { writeTerminalSequence = originalWriteTerminalSequence }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sequenceLog := strings.Builder{}
			writeTerminalSequence = func(sequence string) {
				sequenceLog.WriteString(sequence)
			}

			m := newProjectedStaticUIModel()
			m.windowSizeKnown = true
			m.termWidth = 80
			m.termHeight = 24
			enterDetailCmd := m.toggleTranscriptModeWithNativeReplay(false)
			if enterDetailCmd == nil {
				t.Fatal("expected detail-mode open command")
			}
			_ = collectCmdMessages(t, enterDetailCmd)
			if got := m.view.Mode(); got != tui.ModeDetail {
				t.Fatalf("mode=%q want detail", got)
			}
			if !m.altScreenActive {
				t.Fatal("expected alt-screen active after entering detail")
			}

			openCmd := m.activateSurface(tt.surface)
			if openCmd != nil {
				_ = collectCmdMessages(t, openCmd)
			}
			restoreCmd := m.restoreTranscriptSurface()
			if restoreCmd != nil {
				_ = collectCmdMessages(t, restoreCmd)
			}

			if got := sequenceLog.String(); strings.Count(got, "\x1b[?1007h") != 1 || strings.Contains(got, "\x1b[?1007l") {
				t.Fatalf("detail overlay did not preserve alternate scroll, got sequences %q", got)
			}
			if got := m.view.Mode(); got != tui.ModeDetail {
				t.Fatalf("mode=%q want detail", got)
			}
			if !m.altScreenActive {
				t.Fatal("expected alt-screen active after restore")
			}
		})
	}
}

func TestReturningFromFullscreenSurfaceReplaysNativeOngoingDelta(t *testing.T) {
	tests := []struct {
		name    string
		surface uiSurface
		restore func(*uiModel) tea.Cmd
	}{
		{name: "status", surface: uiSurfaceStatus, restore: func(m *uiModel) tea.Cmd { return m.restoreTranscriptSurface() }},
		{name: "process", surface: uiSurfaceProcessList, restore: func(m *uiModel) tea.Cmd { return m.restoreTranscriptSurface() }},
		{name: "goal", surface: uiSurfaceGoal, restore: func(m *uiModel) tea.Cmd { return m.restoreTranscriptSurface() }},
		{name: "worktree", surface: uiSurfaceWorktree, restore: func(m *uiModel) tea.Cmd { return m.restoreTranscriptSurface() }},
		{
			name:    "rollback",
			surface: uiSurfaceRollbackSelection,
			restore: func(m *uiModel) tea.Cmd {
				m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail, SkipDetailWarmup: true})
				m.activeSurface = uiSurfaceRollbackSelection
				m.rollback.restoreTranscriptMode = tui.ModeOngoing
				return m.popRollbackOverlayWithNativeReplay(true)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newProjectedStaticUIModel()
			m.windowSizeKnown = true
			m.termWidth = 80
			initial := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{{Role: "assistant", Lines: []string{"before"}}}}
			updated := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{{Role: "assistant", Lines: []string{"before"}}, {Role: "assistant", Lines: []string{"after"}}}}
			m.nativeProjection = updated
			m.nativeRenderedProjection = initial
			m.nativeRenderedSnapshot = initial.Render(tui.TranscriptDivider)
			m.activeSurface = tt.surface
			m.altScreenActive = true

			cmd := tt.restore(m)
			if cmd == nil {
				t.Fatal("expected restore command")
			}
			assertNativeFlushText(t, collectCmdMessages(t, cmd), "after")
			if m.surface() != uiSurfaceOngoingTranscript {
				t.Fatalf("surface=%q want ongoing", m.surface())
			}
		})
	}
}

func assertNativeFlushText(t *testing.T, msgs []tea.Msg, want string) {
	t.Helper()
	var flush nativeHistoryFlushMsg
	foundFlush := false
	for _, msg := range msgs {
		if typed, ok := msg.(nativeHistoryFlushMsg); ok {
			flush = typed
			foundFlush = true
		}
	}
	if !foundFlush {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", msgs)
	}
	if got := stripANSIText(flush.Text); got != want {
		t.Fatalf("expected native replay %q, got %q", want, got)
	}
}

func TestReturningFromDetailSyncsCommittedSuffixTailIntoOngoingView(t *testing.T) {
	m := newProjectedTestUIModel(&runtimeControlFakeClient{}, closedProjectedRuntimeEvents(), closedAskEvents())
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, TotalEntries: 1})

	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("mode=%q want detail", m.view.Mode())
	}

	_ = m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            2,
		CommittedEntryCount: 2,
		StartEntryCount:     1,
		NextEntryCount:      2,
		Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "answer"}},
	})

	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("mode=%q want ongoing", m.view.Mode())
	}
	if got := stripANSIAndTrimRight(m.view.OngoingSnapshot()); !containsAny(got, "answer") {
		t.Fatalf("expected committed suffix in ongoing tail after detail restore, got %q", got)
	}
}

func TestReturningFromDetailPreservesLiveTransientTailInOngoingView(t *testing.T) {
	m := newProjectedTestUIModel(&runtimeControlFakeClient{}, closedProjectedRuntimeEvents(), closedAskEvents())
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, TotalEntries: 1})

	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("mode=%q want detail", m.view.Mode())
	}

	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{
		Role:      tui.TranscriptRoleToolCall,
		Text:      "echo live",
		Transient: true,
	})
	m.transcriptTotalEntries = 2

	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("mode=%q want ongoing", m.view.Mode())
	}
	if got := stripANSIAndTrimRight(m.view.OngoingSnapshot()); !strings.Contains(got, "echo live") {
		t.Fatalf("expected live transient tail after detail restore, got %q", got)
	}
}

func TestReturningFromDetailPreservesTransientTailAndNextLiveAppendInOngoingView(t *testing.T) {
	m := newProjectedTestUIModel(&runtimeControlFakeClient{}, closedProjectedRuntimeEvents(), closedAskEvents())
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, TotalEntries: 1})

	_ = m.toggleTranscriptModeWithNativeReplay(false)
	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{
		Role:      tui.TranscriptRoleToolCall,
		Text:      "echo live",
		Transient: true,
	})
	m.transcriptTotalEntries = 2

	_ = m.toggleTranscriptModeWithNativeReplay(false)
	if got := stripANSIAndTrimRight(m.view.OngoingSnapshot()); !strings.Contains(got, "echo live") {
		t.Fatalf("expected transient tail after detail restore, got %q", got)
	}

	m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{
		Role:      tui.TranscriptRoleAssistant,
		Text:      "post-restore live append",
		Transient: true,
	})
	m.transcriptTotalEntries = 3
	m.syncOngoingTailViewFromRuntimeState()

	got := stripANSIAndTrimRight(m.view.OngoingSnapshot())
	if !strings.Contains(got, "echo live") || !strings.Contains(got, "post-restore live append") {
		t.Fatalf("expected transient tail and next live append after detail restore, got %q", got)
	}
}
