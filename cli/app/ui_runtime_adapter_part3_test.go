package app

import (
	"core/cli/tui"
	"core/server/llm"
	"core/server/runtime"
	"core/shared/clientui"
	"fmt"
	"reflect"
	"testing"
)

func TestApplyRuntimeTranscriptPageSkipsDuplicateDetailRefresh(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 12
	m.windowSizeKnown = true
	page := clientui.TranscriptPage{SessionID: "session-1", Offset: 300, TotalEntries: 500}
	for i := 0; i < 200; i++ {
		page.Entries = append(page.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("line %03d", 300+i)})
	}
	entries := transcriptEntriesFromPage(page)
	m.detailTranscript.replace(page)
	m.forwardToView(tui.SetConversationMsg{BaseOffset: page.Offset, TotalEntries: page.TotalEntries, Entries: entries})
	m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail, SkipDetailWarmup: true})
	m.layout().syncViewport()

	cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Offset: page.Offset, Limit: len(page.Entries)}, page, clientui.TranscriptRecoveryCauseNone)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected duplicate detail page refresh to be skipped, got %T", msg)
		}
	}
	if m.view.TranscriptBaseOffset() != page.Offset || m.view.TranscriptTotalEntries() != page.TotalEntries {
		t.Fatalf("detail transcript metadata changed unexpectedly: base=%d total=%d", m.view.TranscriptBaseOffset(), m.view.TranscriptTotalEntries())
	}
}

func TestApplyRuntimeTranscriptPageInDetailModeDoesNotRebuildNativeHistoryState(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	ongoingPage := clientui.TranscriptPage{SessionID: "session-1", Offset: 300, TotalEntries: 500}
	for i := 0; i < 200; i++ {
		ongoingPage.Entries = append(ongoingPage.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("tail %03d", 300+i)})
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, ongoingPage, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	baselineProjection := m.nativeProjection
	baselineRenderedProjection := m.nativeRenderedProjection
	baselineRenderedSnapshot := m.nativeRenderedSnapshot
	baselineFlushedEntryCount := m.nativeFlushedEntryCount

	m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail, SkipDetailWarmup: true})
	detailPage := clientui.TranscriptPage{SessionID: "session-1", Offset: 0, TotalEntries: 500}
	for i := 0; i < 250; i++ {
		detailPage.Entries = append(detailPage.Entries, clientui.ChatEntry{Role: "assistant", Text: fmt.Sprintf("history %03d", i)})
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Offset: 0, Limit: 250}, detailPage, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if !reflect.DeepEqual(m.nativeProjection, baselineProjection) {
		t.Fatal("detail transcript apply unexpectedly changed native projection state")
	}
	if !reflect.DeepEqual(m.nativeRenderedProjection, baselineRenderedProjection) {
		t.Fatal("detail transcript apply unexpectedly changed rendered native projection state")
	}
	if m.nativeRenderedSnapshot != baselineRenderedSnapshot {
		t.Fatalf("detail transcript apply changed rendered native snapshot: %q -> %q", baselineRenderedSnapshot, m.nativeRenderedSnapshot)
	}
	if m.nativeFlushedEntryCount != baselineFlushedEntryCount {
		t.Fatalf("detail transcript apply changed native flushed entry count: %d -> %d", baselineFlushedEntryCount, m.nativeFlushedEntryCount)
	}
}

func TestApplyRuntimeTranscriptPageInDetailModeAdvancesRevisionEvenWhenPageMatches(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	page := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed-0"},
			{Role: "assistant", Text: "seed-1"},
		},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Offset: 0, Limit: 2}, page, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail, SkipDetailWarmup: true})
	m.layout().syncViewport()

	updated := page
	updated.Revision = 11
	cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Offset: 0, Limit: 2}, updated, clientui.TranscriptRecoveryCauseNone)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected matching detail page refresh to be skipped, got %T", msg)
		}
	}
	if got := m.transcriptRevision; got != 11 {
		t.Fatalf("transcript revision after matching detail refresh = %d, want 11", got)
	}
}

func TestApplyRuntimeTranscriptPageAcceptsSameRevisionEmptyOngoingWhenCommittedTranscriptAlreadyMatches(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "done"}}
	m.transcriptBaseOffset = 0
	m.transcriptTotalEntries = 1
	m.transcriptRevision = 10
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "done"})
	m.sawAssistantDelta = true

	page := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "done", Phase: string(llm.MessagePhaseFinal)}},
		Ongoing:      "",
	}
	cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, page, clientui.TranscriptRecoveryCauseNone)
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected same-revision authoritative page to clear stale live ongoing, got %q", got)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected same-revision authoritative page to clear assistant delta flag")
	}
	if cmd == nil {
		t.Fatal("expected native sync command after authoritative page apply")
	}
}

func TestApplyRuntimeTranscriptPageAcceptsSameRevisionEmptyOngoingWhenPageCommitsLiveAssistantBeforeCommittedSuffix(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "user", Text: "prompt"}}
	m.transcriptBaseOffset = 0
	m.transcriptTotalEntries = 3
	m.transcriptRevision = 10
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "done"})
	m.sawAssistantDelta = true

	page := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 3,
		Entries: []clientui.ChatEntry{
			{Role: "user", Text: "prompt"},
			{Role: "assistant", Text: "done", Phase: string(llm.MessagePhaseFinal)},
			{Role: "reviewer_status", Text: "Supervisor ran: no changes."},
		},
		Ongoing: "",
	}
	cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, page, clientui.TranscriptRecoveryCauseNone)
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected same-revision authoritative page to clear stale live ongoing when assistant is committed before suffix, got %q", got)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected same-revision authoritative page to clear assistant delta flag when assistant is committed before suffix")
	}
	if got, want := len(m.transcriptEntries), 3; got != want {
		t.Fatalf("transcript entry count = %d, want %d", got, want)
	}
	if got := m.transcriptEntries[1].Text; got != "done" {
		t.Fatalf("assistant text = %q, want done", got)
	}
	if got := m.transcriptEntries[2].Role; got != "reviewer_status" {
		t.Fatalf("suffix role = %q, want reviewer_status", got)
	}
	if cmd == nil {
		t.Fatal("expected native sync command after authoritative page apply")
	}
}

func TestApplyRuntimeTranscriptPageRejectsSameRevisionEmptyOngoingWhenOnlyOlderAssistantMatchesLiveText(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "done"}, {Role: "assistant", Text: "different"}}
	m.transcriptBaseOffset = 0
	m.transcriptTotalEntries = 2
	m.transcriptRevision = 10
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "done"})
	m.sawAssistantDelta = true

	page := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "done", Phase: string(llm.MessagePhaseFinal)},
			{Role: "assistant", Text: "different", Phase: string(llm.MessagePhaseFinal)},
		},
		Ongoing: "",
	}
	cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, page, clientui.TranscriptRecoveryCauseNone)
	if got := m.view.OngoingStreamingText(); got != "done" {
		t.Fatalf("expected same-revision authoritative page to preserve live ongoing when only older assistant matches, got %q", got)
	}
	if !m.sawAssistantDelta {
		t.Fatal("expected stale page rejection to preserve assistant delta flag")
	}
	if cmd != nil {
		t.Fatalf("expected no native sync command on stale ongoing clear rejection, got %T", cmd)
	}
}

func TestInvalidateTransientTranscriptStateClearsDeferredCommittedTail(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.deferredCommittedTail = []deferredProjectedTranscriptTail{{rangeStart: 1, rangeEnd: 2, revision: 7, entries: []clientui.ChatEntry{{Role: "user", Text: "queued"}}}}
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "done", Transient: true, Committed: true}}
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, Ongoing: "done"})

	m.invalidateTransientTranscriptState()

	if got := len(m.deferredCommittedTail); got != 0 {
		t.Fatalf("expected deferred committed tail cleared during transient state invalidation, got %d", got)
	}
}

func TestApplyRuntimeTranscriptPageRejectsStaleAuthoritativePageWhileDeferredCommittedTailExists(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedClosedUIModel(client)
	m.sessionID = "session-1"
	m.setBusy(true)
	m.sawAssistantDelta = true
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 0, Entries: nil, Ongoing: "stale assistant"})

	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         7,
		CommittedEntryCount:        1,
		UserMessage:                "steered message",
		TranscriptEntries:          []clientui.ChatEntry{{Role: "user", Text: "steered message"}},
	}, true).cmd
	if got := len(m.deferredCommittedTail); got != 1 {
		t.Fatalf("expected deferred committed user tail before stale hydrate, got %d", got)
	}

	cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     6,
		Offset:       0,
		TotalEntries: 0,
		Entries:      nil,
	}, clientui.TranscriptRecoveryCauseNone)
	if cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	if got := len(m.deferredCommittedTail); got != 1 {
		t.Fatalf("expected stale authoritative page to preserve deferred committed tail, got %d", got)
	}

	cmd = m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         8,
		CommittedEntryCount:        2,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "done",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}, true).cmd
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect assistant commit after stale hydrate rejection to require hydration, got %+v", msg)
		}
	}
	if got := len(m.transcriptEntries); got != 2 {
		t.Fatalf("expected deferred user tail to merge with assistant commit after stale hydrate rejection, got %d entries", got)
	}
	if got := m.transcriptEntries[0].Text; got != "steered message" {
		t.Fatalf("first transcript entry = %q, want steered message", got)
	}
	if got := m.transcriptEntries[1].Text; got != "done" {
		t.Fatalf("second transcript entry = %q, want done", got)
	}
}

func TestApplyRuntimeTranscriptPageRejectsOlderRevisionTailReplacement(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true

	current := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "newer"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, current, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	older := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "older"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, older, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected older-revision page to be ignored, got %T", msg)
		}
	}
	if got := m.transcriptRevision; got != 11 {
		t.Fatalf("transcript revision = %d, want 11", got)
	}
	if got, want := len(m.transcriptEntries), 1; got != want {
		t.Fatalf("transcript entry count = %d, want %d", got, want)
	}
	if got := m.transcriptEntries[0].Text; got != "newer" {
		t.Fatalf("transcript entry = %q, want newer", got)
	}
}

func TestApplyRuntimeTranscriptPageRejectsEqualRevisionTailReplacementThatClearsLiveOngoing(t *testing.T) {
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
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, baseline, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "working"})
	if got := m.view.OngoingStreamingText(); got != "working" {
		t.Fatalf("ongoing streaming text = %q, want working", got)
	}

	stale := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, stale, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected stale equal-revision page to be ignored, got %T", msg)
		}
	}
	if got := m.view.OngoingStreamingText(); got != "working" {
		t.Fatalf("expected live ongoing stream preserved, got %q", got)
	}
}

func TestApplyRuntimeTranscriptPagePreservesLiveOngoingForEqualRevisionDetailPage(t *testing.T) {
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
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, baseline, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "working"})
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 1, Entries: m.transcriptEntries, Ongoing: m.view.OngoingStreamingText(), OngoingError: "boom"})
	m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail, SkipDetailWarmup: true})

	staleDetail := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{Offset: 0, Limit: 1}, staleDetail, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	if got := m.view.OngoingStreamingText(); got != "working" {
		t.Fatalf("expected live ongoing stream preserved for detail page, got %q", got)
	}
	if got := m.view.OngoingErrorText(); got != "boom" {
		t.Fatalf("expected live ongoing error preserved for detail page, got %q", got)
	}
	if got := m.detailTranscript.ongoing; got != "working" {
		t.Fatalf("expected detail transcript to preserve live ongoing stream, got %q", got)
	}
}

func TestRuntimeTranscriptRefreshPreservesLiveOngoingForEqualRevisionDetailPage(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedClosedUIModel(client)
	m.startupCmds = nil
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
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, baseline, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "working"})
	m.forwardToView(tui.SetConversationMsg{BaseOffset: 0, TotalEntries: 1, Entries: m.transcriptEntries, Ongoing: m.view.OngoingStreamingText(), OngoingError: "boom"})
	m.forwardToView(tui.SetModeMsg{Mode: tui.ModeDetail, SkipDetailWarmup: true})
	m.runtimeTranscriptBusy = true
	m.runtimeTranscriptToken = 7

	staleDetail := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}
	next, cmd := m.Update(runtimeTranscriptRefreshedMsg{
		token:      7,
		req:        clientui.TranscriptPageRequest{Offset: 0, Limit: 1},
		transcript: staleDetail,
	})
	updated := next.(*uiModel)
	_ = collectCmdMessages(t, cmd)
	if got := updated.view.OngoingStreamingText(); got != "working" {
		t.Fatalf("expected hydrated detail page to preserve live ongoing stream, got %q mode=%q revision=%d detail_loaded=%t detail_ongoing=%q detail_error=%q", got, updated.view.Mode(), updated.transcriptRevision, updated.detailTranscript.loaded, updated.detailTranscript.ongoing, updated.detailTranscript.ongoingError)
	}
	if got := updated.view.OngoingErrorText(); got != "boom" {
		t.Fatalf("expected hydrated detail page to preserve live ongoing error, got %q", got)
	}
	if got := updated.detailTranscript.ongoing; got != "working" {
		t.Fatalf("expected hydrated detail transcript to preserve live ongoing stream, got %q", got)
	}
}

func TestApplyRuntimeTranscriptPageAcceptsNewerRevisionTailReplacementThatClearsLiveOngoing(t *testing.T) {
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
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, baseline, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	_ = m.runtimeAdapter().handleRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "working"})

	fresh := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed"},
			{Role: "assistant", Text: "done", Phase: string(llm.MessagePhaseFinal)},
		},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, fresh, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected fresh authoritative page to clear live ongoing, got %q", got)
	}
	if got := m.transcriptRevision; got != 11 {
		t.Fatalf("transcript revision = %d, want 11", got)
	}
}

func TestProjectedAssistantMessageAdvancesTranscriptRevisionForReplayDedupe(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "seed"}}
	m.transcriptBaseOffset = 0
	m.transcriptTotalEntries = 1
	m.transcriptRevision = 10
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	evt := clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        2,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "live append",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}
	if cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(evt, true).cmd; cmd == nil {
		t.Fatal("expected native replay command for projected assistant message")
	}
	if got := m.transcriptRevision; got != 11 {
		t.Fatalf("transcript revision after live append = %d, want 11", got)
	}
	if got := len(m.transcriptEntries); got != 2 {
		t.Fatalf("transcript entry count after live append = %d, want 2", got)
	}

	if cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(evt, true).cmd; cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected replayed assistant message to be skipped, got %T", msg)
		}
	}
	if got := len(m.transcriptEntries); got != 2 {
		t.Fatalf("expected replayed assistant message to stay deduped, got %d entries", got)
	}
}

func TestApplyRuntimeTranscriptPageAcceptsEqualRevisionTailReplacementWhenRuntimeOnlyEntryChanged(t *testing.T) {
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
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, baseline, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}
	m.transcriptLiveDirty = true

	runtimeOnly := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		Offset:       0,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed"},
			{Role: "error", Text: "background continuation failed: boom"},
		},
	}
	if cmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, runtimeOnly, clientui.TranscriptRecoveryCauseNone); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	if got := len(m.transcriptEntries); got != 2 {
		t.Fatalf("transcript entry count = %d, want 2", got)
	}
	if got := m.transcriptEntries[1].Text; got != "background continuation failed: boom" {
		t.Fatalf("runtime-only entry text = %q, want background continuation failed: boom", got)
	}
	if m.transcriptLiveDirty {
		t.Fatal("expected accepted equal-revision tail refresh to clear transcriptLiveDirty")
	}
}
