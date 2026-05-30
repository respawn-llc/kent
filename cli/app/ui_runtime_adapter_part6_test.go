package app

import (
	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/runtime"
	"builder/shared/clientui"
	"builder/shared/transcript"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestProjectedConversationUpdatedSkipsHydrationAfterImmediateUserFlushAppend(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedClosedUIModel(client)
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.setBusy(true)
	m.pendingInjected = queuedUserMessagesForTest("steered message")
	m.input = "steered message"
	m.lockedInjectText = "steered message"
	m.lockedInjectID = "queue-test-0"
	m.setInputSubmitLocked(true)
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "user", Text: "seed"}}
	m.transcriptRevision = 6
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "foreground done"})
	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantDelta, StepID: "step-1", AssistantDelta: "foreground done"})

	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                         clientui.EventUserMessageFlushed,
		StepID:                       "step-1",
		CommittedTranscriptChanged:   true,
		TranscriptRevision:           7,
		CommittedEntryCount:          2,
		UserMessage:                  "steered message",
		UserMessageBatchQueueItemIDs: []string{"queue-test-0"},
		TranscriptEntries:            []clientui.ChatEntry{{Role: "user", Text: "steered message"}},
	})
	if got := len(m.transcriptEntries); got != 2 {
		t.Fatalf("expected queued user flush to append immediately once committed tail is contiguous, got %d entries", got)
	}

	cmd := m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         7,
		CommittedEntryCount:        2,
	})
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect committed conversation_updated to hydrate after immediate user append, got %+v", msg)
		}
	}
	if got := len(m.deferredCommittedTail); got != 0 {
		t.Fatalf("expected queued user flush path to avoid deferred committed tail, got %d", got)
	}
}

func TestProjectedAssistantMessageMergesDeferredCommittedUserFlushWithoutHydration(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedClosedUIModel(client)
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.setBusy(true)
	m.pendingInjected = queuedUserMessagesForTest("steered message")
	m.input = "steered message"
	m.lockedInjectText = "steered message"
	m.lockedInjectID = "queue-test-0"
	m.setInputSubmitLocked(true)
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "seed"}}
	m.transcriptRevision = 6
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})
	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantDelta, StepID: "step-1", AssistantDelta: "foreground done"})

	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                         clientui.EventUserMessageFlushed,
		StepID:                       "step-1",
		CommittedTranscriptChanged:   true,
		TranscriptRevision:           7,
		CommittedEntryCount:          2,
		UserMessage:                  "steered message",
		UserMessageBatchQueueItemIDs: []string{"queue-test-0"},
		TranscriptEntries:            []clientui.ChatEntry{{Role: "user", Text: "steered message"}},
	})

	cmd := m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         8,
		CommittedEntryCount:        3,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "foreground done", Phase: string(llm.MessagePhaseFinal)}},
	})
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect hydration after assistant caught up with deferred user flush, got %+v", msgs)
		}
	}
	if got := len(m.deferredCommittedTail); got != 0 {
		t.Fatalf("expected deferred tail cleared after assistant catch-up, got %d", got)
	}
	if got := len(m.transcriptEntries); got != 3 {
		t.Fatalf("expected seed + deferred user + assistant, got %d entries", got)
	}
	if got := m.transcriptEntries[1].Text; got != "steered message" {
		t.Fatalf("second transcript entry = %q, want steered message", got)
	}
	if got := m.transcriptEntries[2].Text; got != "foreground done" {
		t.Fatalf("third transcript entry = %q, want foreground done", got)
	}
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected assistant commit to clear live stream after deferred merge, got %q", got)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected assistant delta flag cleared after deferred merge")
	}
}

func TestProjectedAssistantMessageReplacesNonTailCommittedRangeWithoutHydration(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedClosedUIModel(client)
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptEntries = []tui.TranscriptEntry{
		{Role: "assistant", Text: "seed"},
		{Role: "assistant", Text: "stale final", Phase: llm.MessagePhaseFinal},
		{Role: "reviewer_status", Text: "Supervisor ran: no changes."},
	}
	m.transcriptRevision = 10
	m.transcriptTotalEntries = len(m.transcriptEntries)
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: len(m.transcriptEntries), Entries: m.transcriptEntries})
	m.syncViewport()

	cmd := m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        3,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "reviewed final",
			Phase: string(llm.MessagePhaseFinal),
		}},
	})
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect non-tail committed assistant replacement to trigger hydration, got %+v", msg)
		}
	}
	if got := len(m.transcriptEntries); got != 3 {
		t.Fatalf("transcript entry count = %d, want 3", got)
	}
	if got := m.transcriptEntries[1].Text; got != "reviewed final" {
		t.Fatalf("replaced assistant text = %q, want reviewed final", got)
	}
	if got := m.transcriptEntries[2].Role; got != "reviewer_status" {
		t.Fatalf("suffix role = %q, want reviewer_status", got)
	}
	committed := stripANSIAndTrimRight(m.view.OngoingCommittedSnapshot())
	if !containsInOrder(committed, "seed", "reviewed final", "Supervisor ran: no changes.") {
		t.Fatalf("expected committed ongoing surface to keep reviewer suffix after assistant replacement, got %q", committed)
	}
}

func TestProjectedCommittedGapClearsDeferredCommittedTailBeforeHydration(t *testing.T) {
	client := &runtimeControlFakeClient{transcript: clientui.TranscriptPage{SessionID: "session-1"}}
	m := newProjectedClosedUIModel(client)
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "seed"}}
	m.transcriptRevision = 7
	m.transcriptTotalEntries = 2
	m.deferredCommittedTail = []deferredProjectedTranscriptTail{{
		rangeStart: 1,
		rangeEnd:   2,
		revision:   7,
		entries:    []clientui.ChatEntry{{Role: "user", Text: "queued user"}},
	}}
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 1, Entries: m.transcriptEntries, Ongoing: "foreground done"})
	m.sawAssistantDelta = true

	cmd := m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         8,
		CommittedEntryCount:        4,
		CommittedEntryStart:        3,
		CommittedEntryStartSet:     true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "authoritative tail",
			Phase: string(llm.MessagePhaseFinal),
		}},
	})
	msgs := collectCmdMessages(t, cmd)
	refreshFound := false
	for _, msg := range msgs {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			refreshFound = true
		}
	}
	if !refreshFound {
		t.Fatalf("expected committed gap to request hydration, got %+v", msgs)
	}
	if got := len(m.deferredCommittedTail); got != 0 {
		t.Fatalf("expected committed continuity loss to clear deferred committed tail before hydration, got %d", got)
	}
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected continuity recovery to clear stale ongoing assistant text, got %q", got)
	}
}

func TestProjectedUserMessageFlushedDoesNotDeferAfterCommittedAssistantToolProgress(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedClosedUIModel(client)
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.setBusy(true)
	m.sawAssistantDelta = true
	m.transcriptEntries = []tui.TranscriptEntry{
		{Role: "user", Text: "run task"},
		{Role: "assistant", Text: "working"},
		{Role: "tool_call", Text: "sleep 1", ToolCallID: "call-1", ToolCall: &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "sleep 1"}},
		{Role: "tool_result_ok", Text: "/tmp", ToolCallID: "call-1"},
	}
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "working"})

	cmd := m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		UserMessage:                "steered message",
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "steered message",
		}},
	})
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect transcript refresh after flushed user message, got %+v", msgs)
		}
	}
	if got := len(m.transcriptEntries); got != 5 {
		t.Fatalf("expected queued user flush to append immediately after committed tool progress, got %d entries", got)
	}
	if got := m.transcriptEntries[4].Text; got != "steered message" {
		t.Fatalf("transcript entry text = %q, want steered message", got)
	}
}

func TestProjectedUserMessageFlushedDoesNotDeferWhenUIIsIdleDespiteStaleLiveAssistantState(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedClosedUIModel(client)
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.setBusy(false)
	m.sawAssistantDelta = true
	m.forwardToView(tui.SetConversationMsg{Ongoing: "stale assistant"})

	cmd := m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		UserMessage:                "steered message",
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "steered message",
		}},
	})
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect transcript refresh after idle flushed user message, got %+v", msgs)
		}
	}
	if got := len(m.transcriptEntries); got != 1 {
		t.Fatalf("expected idle flushed user message to append immediately, got %d entries", got)
	}
	if got := m.transcriptEntries[0].Text; got != "steered message" {
		t.Fatalf("transcript entry text = %q, want steered message", got)
	}
}

func TestDeferredNativeReplayFlushesAutomaticallyOnDetailExit(t *testing.T) {
	policies := []string{"fixed-detail-alt-screen"}
	for _, policy := range policies {
		t.Run(policy, func(t *testing.T) {
			m := newProjectedStaticUIModel(
				WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
			)

			next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
			m = next.(*uiModel)
			if startupCmd == nil {
				t.Fatal("expected startup replay command")
			}
			_ = collectCmdMessages(t, startupCmd)

			next, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
			m = next.(*uiModel)
			if m.view.Mode() != tui.ModeDetail {
				t.Fatalf("expected detail mode, got %q", m.view.Mode())
			}
			_ = collectCmdMessages(t, enterCmd)

			cmd := m.runtimeAdapter().applyChatSnapshot(runtime.ChatSnapshot{
				Entries: []runtime.ChatEntry{{Role: "assistant", Text: "seed"}, {Role: "user", Text: "steered later"}},
			})
			if cmd != nil {
				t.Fatalf("expected replay to stay deferred while detail is active, got %T", cmd())
			}

			next, leaveCmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
			m = next.(*uiModel)
			if m.view.Mode() != tui.ModeOngoing {
				t.Fatalf("expected ongoing mode, got %q", m.view.Mode())
			}
			msgs := collectCmdMessages(t, leaveCmd)
			flushCount := 0
			foundLater := false
			for _, msg := range msgs {
				flush, ok := msg.(nativeHistoryFlushMsg)
				if !ok {
					continue
				}
				flushCount++
				if strings.Contains(stripANSIPreserve(flush.Text), "steered later") {
					foundLater = true
				}
			}
			if flushCount == 0 {
				t.Fatalf("expected native replay flush on detail exit, got messages=%v native_projection=%+v native_rendered_projection=%+v native_snapshot=%q transcript=%+v", msgs, m.nativeProjection, m.nativeRenderedProjection, m.nativeRenderedSnapshot, m.transcriptEntries)
			}
			if !foundLater {
				t.Fatalf("expected exit replay to include deferred transcript update, got messages=%v", msgs)
			}
		})
	}
}

func TestBackgroundUpdatedUsesTransientStatusLifecycle(t *testing.T) {
	m := newProjectedStaticUIModel()

	cmd := m.runtimeAdapter().handleRuntimeEvent(runtime.Event{
		Kind: runtime.EventBackgroundUpdated,
		Background: &runtime.BackgroundShellEvent{
			Type:  "completed",
			ID:    "1000",
			State: "completed",
		},
	})
	if cmd == nil {
		t.Fatal("expected transient status clear command")
	}
	if got := strings.TrimSpace(m.transientStatus); got != "background shell 1000 completed" {
		t.Fatalf("unexpected transient status %q", got)
	}
	if m.transientStatusKind != uiStatusNoticeSuccess {
		t.Fatalf("expected success notice kind, got %d", m.transientStatusKind)
	}
	clearMsg, ok := cmd().(clearTransientStatusMsg)
	if !ok {
		t.Fatalf("expected clearTransientStatusMsg, got %T", cmd())
	}
	next, _ := m.Update(clearMsg)
	updated := next.(*uiModel)
	if updated.transientStatus != "" {
		t.Fatalf("expected transient status to clear, got %q", updated.transientStatus)
	}
	if updated.transientStatusKind != uiStatusNoticeNeutral {
		t.Fatalf("expected transient status kind reset, got %d", updated.transientStatusKind)
	}
}

func TestBackgroundUpdatedWhileBusyUsesCompletionStatus(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.setBusy(true)

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{
		Kind: runtime.EventBackgroundUpdated,
		Background: &runtime.BackgroundShellEvent{
			Type:  "completed",
			ID:    "1000",
			State: "completed",
		},
	})

	if got := strings.TrimSpace(m.transientStatus); got != "background shell 1000 completed" {
		t.Fatalf("unexpected transient status %q", got)
	}
}

func TestBackgroundUpdatedWithSuppressedNoticeSkipsTransientStatus(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.transientStatus = "existing"

	cmd := m.runtimeAdapter().handleRuntimeEvent(runtime.Event{
		Kind: runtime.EventBackgroundUpdated,
		Background: &runtime.BackgroundShellEvent{
			Type:             "completed",
			ID:               "1000",
			State:            "completed",
			NoticeSuppressed: true,
		},
	})

	if cmd != nil {
		t.Fatalf("did not expect transient status command when notice is suppressed, got %T", cmd())
	}
	if m.transientStatus != "existing" {
		t.Fatalf("expected transient status unchanged, got %q", m.transientStatus)
	}
}

func TestDeferredNativeReplayFlushesBackgroundNoticeOnDetailExit(t *testing.T) {
	policies := []string{"fixed-detail-alt-screen"}
	for _, policy := range policies {
		t.Run(policy, func(t *testing.T) {
			m := newProjectedStaticUIModel(
				WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
			)

			next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
			m = next.(*uiModel)
			if startupCmd == nil {
				t.Fatal("expected startup replay command")
			}
			_ = collectCmdMessages(t, startupCmd)

			next, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
			m = next.(*uiModel)
			if m.view.Mode() != tui.ModeDetail {
				t.Fatalf("expected detail mode, got %q", m.view.Mode())
			}
			_ = collectCmdMessages(t, enterCmd)

			cmd := m.runtimeAdapter().applyChatSnapshot(runtime.ChatSnapshot{
				Entries: []runtime.ChatEntry{
					{Role: "assistant", Text: "seed"},
					{Role: "system", Text: "Background shell 1000 completed.\nExit code: 0\nOutput:\ndone", OngoingText: "Background shell 1000 completed (exit 0)"},
				},
			})
			if cmd != nil {
				t.Fatalf("expected replay to stay deferred while detail is active, got %T", cmd())
			}

			next, leaveCmd := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
			m = next.(*uiModel)
			if m.view.Mode() != tui.ModeOngoing {
				t.Fatalf("expected ongoing mode, got %q", m.view.Mode())
			}
			msgs := collectCmdMessages(t, leaveCmd)
			flushCount := 0
			foundNotice := false
			for _, msg := range msgs {
				flush, ok := msg.(nativeHistoryFlushMsg)
				if !ok {
					continue
				}
				flushCount++
				plain := stripANSIPreserve(flush.Text)
				if strings.Contains(plain, "Background shell 1000 completed (exit 0)") {
					foundNotice = true
				}
			}
			if flushCount == 0 {
				t.Fatalf("expected native replay flush on detail exit, got messages=%v native_projection=%+v native_rendered_projection=%+v native_snapshot=%q transcript=%+v", msgs, m.nativeProjection, m.nativeRenderedProjection, m.nativeRenderedSnapshot, m.transcriptEntries)
			}
			if !foundNotice {
				t.Fatalf("expected exit replay to include deferred background notice, got messages=%v", msgs)
			}
		})
	}
}

func TestRunStateChangedTransitionsRunningStateToIdleWhenTurnEnds(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.activity = uiActivityRunning

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventRunStateChanged, RunState: &runtime.RunState{Lifecycle: runtime.IdleRunLifecycle()}})

	if m.activity != uiActivityIdle {
		t.Fatalf("expected idle activity after turn end, got %v", m.activity)
	}
}

func TestUserRequestedKilledBackgroundUsesSuccessNotice(t *testing.T) {
	m := newProjectedStaticUIModel()

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{
		Kind: runtime.EventBackgroundUpdated,
		Background: &runtime.BackgroundShellEvent{
			Type:              "killed",
			ID:                "1001",
			State:             "killed",
			UserRequestedKill: true,
		},
	})
	if m.transientStatusKind != uiStatusNoticeSuccess {
		t.Fatalf("expected success notice kind, got %d", m.transientStatusKind)
	}
}
