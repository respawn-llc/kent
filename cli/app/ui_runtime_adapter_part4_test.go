package app

import (
	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/runtime"
	"builder/shared/clientui"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestApplyRuntimeTranscriptPageAcceptsEqualRevisionTailReplacementWhenAuthoritativePageCorrectsOverlap(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if cmd, mutated, needsHydration := m.runtimeAdapter().applyProjectedTranscriptEntries(clientui.Event{Kind: clientui.EventToolCallStarted, TranscriptEntries: []clientui.ChatEntry{{
		Role:       "tool_call",
		Text:       "pwd",
		ToolCallID: "stale-call",
		ToolCall:   &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"},
	}}}, false); cmd != nil || !mutated || needsHydration {
		t.Fatalf("expected live append without extra command, mutated=%t needsHydration=%t cmd=%v", mutated, needsHydration, cmd)
	}
	if !m.transcriptLiveDirty {
		t.Fatal("expected live append to mark transcript live-dirty")
	}

	corrected := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 3,
		Entries: []clientui.ChatEntry{
			{Role: "user", Text: "prompt"},
			{Role: "tool_call", Text: "pwd", ToolCallID: "call-1", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"}},
			{Role: "tool_result_ok", Text: "/tmp", ToolCallID: "call-1"},
		},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, corrected); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if got, want := len(m.transcriptEntries), 3; got != want {
		t.Fatalf("transcript entry count = %d, want %d", got, want)
	}
	if got := m.transcriptEntries[1].ToolCallID; got != "call-1" {
		t.Fatalf("corrected tool call id = %q, want call-1", got)
	}
	if got := m.transcriptEntries[2].ToolCallID; got != "call-1" {
		t.Fatalf("corrected tool result id = %q, want call-1", got)
	}
	if m.transcriptLiveDirty {
		t.Fatal("expected corrective equal-revision refresh to clear transcriptLiveDirty")
	}
	rawCommitted := renderStyledNativeProjection(m.nativeProjection, m.theme, m.termWidth)
	if plain := stripANSIPreserve(rawCommitted); !strings.Contains(plain, "$ pwd") {
		t.Fatalf("expected corrected shell row in committed native projection, got %q", plain)
	}
	assertContainsColoredShellSymbol(t, rawCommitted, "dark success", transcriptToolSuccessColorHex("dark"))
	assertNoColoredShellSymbol(t, rawCommitted, "dark pending", transcriptToolPendingColorHex("dark"))
}

func TestApplyRuntimeTranscriptPageAcceptsEqualRevisionReplacementWhenToolMetadataChanges(t *testing.T) {
	m := newProjectedStaticUIModel()

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "user", Text: "prompt"},
			{Role: "tool_call", Text: "run", ToolCallID: "call-1", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"}},
		},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	m.transcriptLiveDirty = true

	corrected := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "user", Text: "prompt"},
			{Role: "tool_call", Text: "run", ToolCallID: "call-1", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "ls"}},
		},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, corrected); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if got, want := len(m.transcriptEntries), 2; got != want {
		t.Fatalf("transcript entry count = %d, want %d", got, want)
	}
	if m.transcriptEntries[1].ToolCall == nil {
		t.Fatalf("expected corrected tool metadata, got nil")
	}
	if got := m.transcriptEntries[1].ToolCall.Command; got != "ls" {
		t.Fatalf("tool command = %q, want ls", got)
	}
	if m.transcriptLiveDirty {
		t.Fatal("expected equal-revision metadata correction to clear transcriptLiveDirty")
	}
}

func TestProjectedAssistantToolCallEntriesApplyAsCommittedInRuntimeMode(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	toolStarted := clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        2,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			Text:       "pwd",
			ToolCallID: "call-1",
			ToolCall:   &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"},
		}},
	}
	_ = collectCmdMessages(t, m.runtimeAdapter().handleProjectedRuntimeEvent(toolStarted))

	if got, want := len(m.transcriptEntries), 2; got != want {
		t.Fatalf("transcript entry count = %d, want %d", got, want)
	}
	if m.transcriptEntries[1].Transient || !m.transcriptEntries[1].Committed {
		t.Fatalf("expected runtime assistant tool call to apply as committed transcript state, got %+v", m.transcriptEntries[1])
	}
	if got := m.transcriptRevision; got != 11 {
		t.Fatalf("transcript revision = %d, want 11", got)
	}
}

func TestRuntimeAuthoritativeHydrateDoesNotRepairCommittedToolPathWhenLiveProjectionMatches(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	_ = collectCmdMessages(t, m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        2,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_call",
			Text:       "pwd",
			ToolCallID: "call-1",
			ToolCall:   &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"},
		}},
	}))
	_ = collectCmdMessages(t, m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventToolCallCompleted,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         12,
		CommittedEntryCount:        3,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:       "tool_result_ok",
			Text:       "/tmp",
			ToolCallID: "call-1",
		}},
	}))

	cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     12,
		Offset:       0,
		TotalEntries: 3,
		Entries: []clientui.ChatEntry{
			{Role: "user", Text: "prompt"},
			{Role: "tool_call", Text: "pwd", ToolCallID: "call-1", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"}},
			{Role: "tool_result_ok", Text: "/tmp", ToolCallID: "call-1"},
		},
	})
	if cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if m.transientStatus != "" {
		t.Fatalf("did not expect authoritative hydrate warning when live committed tool path already matches, got status=%q", m.transientStatus)
	}
	if got, want := len(m.transcriptEntries), 3; got != want {
		t.Fatalf("transcript entry count = %d, want %d", got, want)
	}
	if !m.transcriptEntries[1].Committed || !m.transcriptEntries[2].Committed {
		t.Fatalf("expected tool path entries to remain committed for ordering after hydrate, got %+v", m.transcriptEntries)
	}
}

func TestRuntimeAuthoritativeHydrateDoesNotRepairCommittedReviewerStatusPathWhenLiveProjectionMatches(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	_ = collectCmdMessages(t, m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventLocalEntryAdded,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        2,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "reviewer_status",
			Text: "Supervisor ran and applied 2 suggestions.",
		}},
	}))

	if m.transcriptEntries[1].Transient || !m.transcriptEntries[1].Committed {
		t.Fatalf("expected reviewer status to apply as committed transcript state, got %+v", m.transcriptEntries[1])
	}

	cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed"},
			{Role: "reviewer_status", Text: "Supervisor ran and applied 2 suggestions."},
		},
	})
	if cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if m.transientStatus != "" {
		t.Fatalf("did not expect authoritative hydrate warning when reviewer status path already matches, got status=%q", m.transientStatus)
	}
	if got, want := len(m.transcriptEntries), 2; got != want {
		t.Fatalf("transcript entry count = %d, want %d", got, want)
	}
	if !m.transcriptEntries[1].Committed {
		t.Fatalf("expected reviewer status to remain committed for ordering after hydrate, got %+v", m.transcriptEntries[1])
	}
}

func TestApplyRuntimeTranscriptPageAcceptsEqualRevisionTailReplacementWhenOngoingErrorChanged(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	m.transcriptLiveDirty = true

	runtimeOnly := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
		OngoingError: "background continuation failed",
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, runtimeOnly); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if got := m.view.OngoingErrorText(); got != "background continuation failed" {
		t.Fatalf("ongoing error text = %q, want background continuation failed", got)
	}
	if m.transcriptLiveDirty {
		t.Fatal("expected accepted equal-revision ongoing-error refresh to clear transcriptLiveDirty")
	}
}

func TestApplyRuntimeTranscriptPageAcceptsEqualRevisionTailReplacementWhenOngoingErrorCleared(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
		OngoingError: "background continuation failed",
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	m.transcriptLiveDirty = true

	cleared := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
		OngoingError: "",
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, cleared); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if got := m.view.OngoingErrorText(); got != "" {
		t.Fatalf("ongoing error text = %q, want empty", got)
	}
	if m.transcriptLiveDirty {
		t.Fatal("expected accepted equal-revision ongoing-error clear to clear transcriptLiveDirty")
	}
	if got := len(m.transcriptEntries); got != 1 {
		t.Fatalf("transcript entry count = %d, want 1", got)
	}
}

func TestApplyRuntimeTranscriptPageRejectsEqualRevisionShiftedTailReplacement(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed-0"},
			{Role: "assistant", Text: "seed-1"},
		},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	m.transcriptLiveDirty = true

	shifted := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       1,
		TotalEntries: 2,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed-1"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, shifted); cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected shifted equal-revision page to be ignored, got %T", msg)
		}
	}

	if got := m.transcriptBaseOffset; got != 0 {
		t.Fatalf("transcript base offset = %d, want 0", got)
	}
	if got := len(m.transcriptEntries); got != 2 {
		t.Fatalf("transcript entry count = %d, want 2", got)
	}
	if got := m.transcriptEntries[0].Text; got != "seed-0" {
		t.Fatalf("first transcript entry = %q, want seed-0", got)
	}
	if !m.transcriptLiveDirty {
		t.Fatal("expected rejected shifted equal-revision page to preserve transcriptLiveDirty")
	}
}

func TestApplyRuntimeTranscriptPageAcceptsNewerRevisionTailReplacementAfterLiveAppend(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	if cmd, mutated, needsHydration := m.runtimeAdapter().applyProjectedTranscriptEntries(clientui.Event{Kind: clientui.EventAssistantMessage, TranscriptEntries: []clientui.ChatEntry{{Role: "assistant", Text: "live append"}}}, false); cmd != nil || !mutated || needsHydration {
		t.Fatalf("expected live append without extra command, mutated=%t needsHydration=%t cmd=%v", mutated, needsHydration, cmd)
	}

	fresh := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed"},
			{Role: "assistant", Text: "live append"},
		},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, fresh); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	if got := m.transcriptRevision; got != 11 {
		t.Fatalf("transcript revision = %d, want 11", got)
	}
	if m.transcriptLiveDirty {
		t.Fatal("expected fresh authoritative page to clear live-dirty state")
	}
	if got, want := len(m.transcriptEntries), 2; got != want {
		t.Fatalf("transcript entry count = %d, want %d", got, want)
	}
	if got := m.transcriptEntries[1].Text; got != "live append" {
		t.Fatalf("second transcript entry = %q, want live append", got)
	}
}

func TestApplyProjectedTranscriptEntriesUsesTailOffsetWhileViewingOlderDetailPage(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	ongoingTail := clientui.TranscriptPage{SessionID: "session-1", Offset: 300, TotalEntries: 500}
	for i := 0; i < 200; i++ {
		ongoingTail.Entries = append(ongoingTail.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("tail %03d", 300+i)})
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, ongoingTail); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail, SkipDetailWarmup: true})
	olderDetailPage := clientui.TranscriptPage{SessionID: "session-1", Offset: 0, TotalEntries: 500}
	for i := 0; i < 250; i++ {
		olderDetailPage.Entries = append(olderDetailPage.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("history %03d", i)})
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{Offset: 0, Limit: 250}, olderDetailPage); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if m.view.TranscriptBaseOffset() != 0 {
		t.Fatalf("expected detail view to remain on older page, got base=%d", m.view.TranscriptBaseOffset())
	}
	if got := m.transcriptBaseOffset; got != ongoingTail.Offset {
		t.Fatalf("live tail base offset = %d, want %d", got, ongoingTail.Offset)
	}

	appended := []clientui.ChatEntry{{Role: "assistant", Text: "tail 500"}, {Role: "assistant", Text: "tail 501"}}
	if cmd, mutated, needsHydration := m.runtimeAdapter().applyProjectedTranscriptEntries(clientui.Event{Kind: clientui.EventAssistantMessage, TranscriptEntries: appended}, false); cmd != nil || !mutated || needsHydration {
		t.Fatalf("expected projected append to mutate without extra command, mutated=%t needsHydration=%t cmd=%v", mutated, needsHydration, cmd)
	}

	if got, want := len(m.transcriptEntries), 202; got != want {
		t.Fatalf("live tail entry count = %d, want %d", got, want)
	}
	if got := m.transcriptEntries[len(m.transcriptEntries)-2].Text; got != "tail 500" {
		t.Fatalf("expected first appended tail entry at live tail end, got %q", got)
	}
	if got := m.transcriptEntries[len(m.transcriptEntries)-1].Text; got != "tail 501" {
		t.Fatalf("expected second appended tail entry at live tail end, got %q", got)
	}
	if got, want := m.transcriptTotalEntries, 502; got != want {
		t.Fatalf("live tail total entries = %d, want %d", got, want)
	}
	if got, want := m.detailTranscript.totalEntries, 502; got != want {
		t.Fatalf("detail transcript total entries = %d, want %d", got, want)
	}
	if got, want := m.detailTranscript.offset, 500; got != want {
		t.Fatalf("detail transcript offset = %d, want %d", got, want)
	}
	if got, want := len(m.detailTranscript.entries), 2; got != want {
		t.Fatalf("detail transcript entry count = %d, want %d", got, want)
	}
	if got := m.detailTranscript.entries[0].Text; got != "tail 500" {
		t.Fatalf("expected first appended detail transcript entry at live tail offset, got %q", got)
	}
	if got := m.detailTranscript.entries[1].Text; got != "tail 501" {
		t.Fatalf("expected second appended detail transcript entry at live tail offset, got %q", got)
	}
	if got := m.view.TranscriptBaseOffset(); got != 0 {
		t.Fatalf("view base offset changed unexpectedly after live append: %d", got)
	}
}

func TestStartupSeedsFromRuntimeClientTranscriptAccessorBeforeBoundedSync(t *testing.T) {
	client := &startupTranscriptRuntimeClient{
		view:     clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1", SessionName: "incident triage"}},
		page:     clientui.TranscriptPage{SessionID: "session-1", Offset: 10, TotalEntries: 15, Entries: []clientui.ChatEntry{{Role: "assistant", Text: "cached tail"}}},
		loadPage: clientui.TranscriptPage{SessionID: "session-1", Offset: 14, TotalEntries: 15, Entries: []clientui.ChatEntry{{Role: "assistant", Text: "authoritative tail"}}},
	}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())

	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	updated := next.(*uiModel)
	if startupCmd == nil {
		t.Fatal("expected startup transcript hydration command")
	}
	if client.transcriptCalls != 1 {
		t.Fatalf("expected startup to seed from RuntimeClient.Transcript(), got %d calls", client.transcriptCalls)
	}
	if got := stripANSIAndTrimRight(updated.view.OngoingSnapshot()); !strings.Contains(got, "cached tail") {
		t.Fatalf("expected cached transcript tail visible before bounded sync, got %q", got)
	}
	if updated.sessionName != "incident triage" {
		t.Fatalf("session name = %q, want incident triage", updated.sessionName)
	}
	if got := len(client.loadRequests); got != 0 {
		t.Fatalf("expected no bounded transcript load before startup cmd executes, got %d", got)
	}
	flushMsg, ok := startupCmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected startup window-size update to replay native history, got %T", startupCmd())
	}
	if !strings.Contains(stripANSIAndTrimRight(flushMsg.Text), "cached tail") {
		t.Fatalf("expected startup native replay to include cached tail, got %q", stripANSIAndTrimRight(flushMsg.Text))
	}
	refreshed, ok := startupCmdMessage[runtimeTranscriptRefreshedMsg](updated.startupCmds)
	if !ok {
		t.Fatalf("expected queued startup sync to return runtimeTranscriptRefreshedMsg, got %d command(s)", len(updated.startupCmds))
	}
	if refreshed.syncCause != runtimeTranscriptSyncCauseBootstrap {
		t.Fatalf("startup bounded sync cause = %q, want %q", refreshed.syncCause, runtimeTranscriptSyncCauseBootstrap)
	}
	if refreshed.req.Window != clientui.TranscriptWindowOngoingTail {
		t.Fatalf("startup transcript request window = %q, want ongoing_tail", refreshed.req.Window)
	}
	if got, want := len(client.loadRequests), 1; got != want {
		t.Fatalf("load request count = %d, want %d", got, want)
	}
	if client.loadRequests[0].Window != clientui.TranscriptWindowOngoingTail {
		t.Fatalf("startup load request window = %q, want ongoing_tail", client.loadRequests[0].Window)
	}

	next, followUp := updated.Update(refreshed)
	if followUp != nil {
		_ = collectCmdMessages(t, followUp)
	}
	afterHydrate := next.(*uiModel)
	if got := stripANSIAndTrimRight(afterHydrate.view.OngoingSnapshot()); !strings.Contains(got, "authoritative tail") || strings.Contains(got, "cached tail") {
		t.Fatalf("expected authoritative startup hydrate without cached seed, got %q", got)
	}
}

func TestAssistantDeltaAppendsStreamingText(t *testing.T) {
	m := newProjectedStaticUIModel()

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "hello"})
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: " world"})

	if got := m.view.OngoingStreamingText(); got != "hello world" {
		t.Fatalf("expected concatenated streaming text, got %q", got)
	}
}

func TestAssistantCommentaryCommitPlusDeltaDoesNotSplitOngoingView(t *testing.T) {
	m := newProjectedStaticUIModel()

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{
		Kind:                       runtime.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         1,
		CommittedEntryCount:        1,
		Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: "Decision: keep Builder tool name patch; expose custom tool with Lark grammar.",
			Phase:   llm.MessagePhaseCommentary,
		},
	})
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{
		Kind:           runtime.EventAssistantDelta,
		StepID:         "step-1",
		AssistantDelta: " Internally normalize custom calls into existing executor input.",
	})

	view := stripANSIPreserve(m.view.OngoingSnapshot())
	if strings.Contains(view, tui.TranscriptDivider) {
		t.Fatalf("expected projected commentary commit plus live assistant delta without divider, got %q", view)
	}
	if !containsInOrder(view, "Decision:", "executor input") {
		t.Fatalf("expected projected commentary commit and live delta in order, got %q", view)
	}
}

func TestAssistantDeltaSkipsNoopFinalToken(t *testing.T) {
	m := newProjectedStaticUIModel()

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: uiNoopFinalToken})

	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected noop final token to stay out of streaming text, got %q", got)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected sawAssistantDelta to remain false for noop final token")
	}
}

func TestAssistantDeltaResetClearsStreamingText(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.forwardToView(tui.SetConversationMsg{Ongoing: "partial"})

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDeltaReset})

	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected reset to clear streaming text, got %q", got)
	}
}

func TestAssistantDeltaDoesNotSuppressNewStepThatMatchesPreviousAssistantText(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "Done", Phase: llm.MessagePhaseFinal}}
	m.lastCommittedAssistantStepID = "step-1"

	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantDelta, StepID: "step-2", AssistantDelta: "Done"})

	if got := m.view.OngoingStreamingText(); got != "Done" {
		t.Fatalf("expected matching assistant delta from a new step to stream, got %q", got)
	}
	if !m.sawAssistantDelta {
		t.Fatal("expected matching assistant delta from a new step to preserve assistant delta flag")
	}
}

func TestAssistantDeltaSuppressesLateMatchingDeltaFromCommittedStep(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "Done", Phase: llm.MessagePhaseFinal}}
	m.lastCommittedAssistantStepID = "step-1"

	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantDelta, StepID: "step-1", AssistantDelta: "Done"})

	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected matching assistant delta from the committed step to stay suppressed, got %q", got)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected matching assistant delta from the committed step to keep assistant delta flag cleared")
	}
}

func TestProjectedAssistantMessageClearsStreamingTextOnCommit(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.setBusy(true)
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "partial"})
	if got := m.view.OngoingStreamingText(); got != "partial" {
		t.Fatalf("expected assistant delta in live stream, got %q", got)
	}

	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "assistant",
			Text: "partial",
		}},
	})

	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected committed assistant message to clear live stream, got %q", got)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected committed assistant message to clear assistant delta flag")
	}
}

func TestProjectedAssistantMessageDoesNotClearStreamingTextWhenCommitIsSkipped(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.setBusy(true)
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "older"}}
	m.transcriptRevision = 5
	m.transcriptTotalEntries = len(m.transcriptEntries)
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "newer live"})

	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         5,
		CommittedEntryCount:        1,
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "assistant",
			Text: "older",
		}},
	})

	if got := m.view.OngoingStreamingText(); got != "newer live" {
		t.Fatalf("expected skipped assistant commit to preserve live stream, got %q", got)
	}
	if !m.sawAssistantDelta {
		t.Fatal("expected skipped assistant commit to preserve assistant delta flag")
	}
}

func TestProjectedAssistantMessageClearsStreamingTextWhenSkippedCommitMatchesLiveStream(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.setBusy(true)
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "final"}}
	m.transcriptRevision = 5
	m.transcriptTotalEntries = len(m.transcriptEntries)
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "final"})

	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         5,
		CommittedEntryCount:        1,
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "assistant",
			Text: "final",
		}},
	})

	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected skipped assistant commit matching live stream to clear it, got %q", got)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected skipped matching assistant commit to clear assistant delta flag")
	}
}

func TestApplyRuntimeTranscriptPagePreservesNonEmptyAuthoritativeOngoingEvenWhenTextMatchesCommittedAssistant(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.sawAssistantDelta = true
	m.forwardToView(tui.SetConversationMsg{Ongoing: "final"})

	page := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     3,
		TotalEntries: 1,
		Entries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "final",
			Phase: string(llm.MessagePhaseFinal),
		}},
		Ongoing: "final",
	}

	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, page); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	if got := m.view.OngoingStreamingText(); got != "final" {
		t.Fatalf("expected authoritative non-empty ongoing preserved, got %q", got)
	}
	if !m.sawAssistantDelta {
		t.Fatal("expected authoritative non-empty ongoing to preserve assistant delta flag")
	}
	if got := len(m.transcriptEntries); got != 1 || m.transcriptEntries[0].Text != "final" {
		t.Fatalf("expected committed assistant entry preserved after authoritative ongoing apply, got %+v", m.transcriptEntries)
	}
}

func TestApplyRuntimeTranscriptPageAllowsEqualRevisionToClearDuplicateCommittedAssistantOngoing(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptRevision = 3
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "final", Phase: llm.MessagePhaseFinal}}
	m.transcriptTotalEntries = len(m.transcriptEntries)
	m.sawAssistantDelta = true
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "final"})

	page := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     3,
		TotalEntries: 1,
		Entries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "final",
			Phase: string(llm.MessagePhaseFinal),
		}},
		Ongoing: "",
	}

	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, page); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected equal-revision authoritative page to clear duplicate ongoing, got %q", got)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected equal-revision duplicate ongoing clear to reset assistant delta flag")
	}
}

func TestApplyRuntimeTranscriptPagePreservesAuthoritativeNonEmptyOngoingOverStaleLiveDuplicate(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptRevision = 3
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "final", Phase: llm.MessagePhaseFinal}}
	m.transcriptTotalEntries = len(m.transcriptEntries)
	m.sawAssistantDelta = true
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "final"})

	page := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     4,
		TotalEntries: 1,
		Entries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "final",
			Phase: string(llm.MessagePhaseFinal),
		}},
		Ongoing: "final continuation",
	}

	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, page); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	if got := m.view.OngoingStreamingText(); got != "final continuation" {
		t.Fatalf("expected authoritative non-empty ongoing preserved, got %q", got)
	}
	if !m.sawAssistantDelta {
		t.Fatal("expected authoritative non-empty ongoing to preserve assistant delta flag")
	}
}

func TestReasoningDeltaUpdatesDetailTranscriptLive(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.forwardToView(tui.SetViewportSizeMsg{Lines: 20, Width: 80})
	m.forwardToView(tui.AppendTranscriptMsg{Role: "user", Text: "u"})
	m.forwardToView(tui.ToggleModeMsg{})

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventReasoningDelta, ReasoningDelta: &llm.ReasoningSummaryDelta{Key: "rs_1:summary:0", Role: "reasoning", Text: "Plan summary"}})

	if detail := stripANSIAndTrimRight(m.view.View()); !strings.Contains(detail, "Plan summary") {
		t.Fatalf("expected live reasoning summary in detail view, got %q", detail)
	}
	if detail := stripANSIAndTrimRight(m.view.View()); strings.Contains(detail, "Preparing patch") {
		t.Fatalf("expected separate status field ignored for detail view, got %q", detail)
	}
}

func TestReasoningDeltaResetClearsLiveReasoningTranscript(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.forwardToView(tui.SetViewportSizeMsg{Lines: 20, Width: 80})
	m.forwardToView(tui.AppendTranscriptMsg{Role: "user", Text: "u"})
	m.forwardToView(tui.ToggleModeMsg{})
	m.forwardToView(tui.UpsertStreamingReasoningMsg{Key: "rs_1:summary:0", Role: "reasoning", Text: "Plan summary"})

	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventReasoningDeltaReset})

	if detail := stripANSIAndTrimRight(m.view.View()); strings.Contains(detail, "Plan summary") {
		t.Fatalf("expected live reasoning summary cleared after reset, got %q", detail)
	}
}

func TestApplyRuntimeTranscriptPageRejectsEqualRevisionReasoningClear(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.forwardToView(tui.SetViewportSizeMsg{Lines: 20, Width: 80})
	m.forwardToView(tui.AppendTranscriptMsg{Role: "user", Text: "u"})
	m.forwardToView(tui.ToggleModeMsg{})

	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "user", Text: "u"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventReasoningDelta, ReasoningDelta: &llm.ReasoningSummaryDelta{Key: "rs_1:summary:0", Role: "reasoning", Text: "Plan summary"}})
	if detail := stripANSIAndTrimRight(m.view.View()); !strings.Contains(detail, "Plan summary") {
		t.Fatalf("expected live reasoning visible before stale page apply, got %q", detail)
	}

	stale := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "user", Text: "u"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, stale); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	if detail := stripANSIAndTrimRight(m.view.View()); !strings.Contains(detail, "Plan summary") {
		t.Fatalf("expected stale equal-revision page to preserve live reasoning, got %q", detail)
	}
}
