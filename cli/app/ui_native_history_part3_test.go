package app

import (
	"core/cli/tui"
	"core/server/llm"
	"core/server/runtime"
	"core/shared/clientui"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestProjectedRuntimeAssistantFinalMatchesMarkdownProjectionAfterHeldSetextAndReference(t *testing.T) {
	m := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), nil)
	next, startupCmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(*uiModel)
	_ = collectCmdMessages(t, startupCmd)

	fullText := "Heading\n---\nThis is [link][id]\n\n[id]: https://example.com\n"
	var emitted strings.Builder
	for _, delta := range []string{"Heading\n", "---\nThis is [link][id]\n", "\n[id]: https://example.com\n"} {
		next, cmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
			projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: delta}),
		}})
		m = next.(*uiModel)
		emitted.WriteString(collectNativeHistoryFlushText(collectCmdMessages(t, cmd)))
	}

	next, finalCmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{
		projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, Message: llm.Message{Role: llm.RoleAssistant, Content: fullText, Phase: llm.MessagePhaseFinal}}),
	}})
	m = next.(*uiModel)
	emitted.WriteString(collectNativeHistoryFlushText(collectCmdMessages(t, finalCmd)))

	got := normalizedOutput(emitted.String())
	want := normalizedOutput(joinedPlainProjectionLines(tui.RenderAssistantMarkdownProjection(fullText, m.theme, m.termWidth)))
	if got != want {
		t.Fatalf("native streamed/final output mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestNativeHistoryFlushWaitsForTargetSequenceBeforeRearmingRuntimeEvents(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.pendingRuntimeEvents = []clientui.Event{{Kind: clientui.EventConversationUpdated}}
	m.waitRuntimeEventAfterFlushSequence = 2

	firstCmd := m.handleNativeHistoryFlush(nativeHistoryFlushMsg{Text: "first", Sequence: 1})
	if m.waitRuntimeEventAfterFlushSequence != 2 {
		t.Fatalf("expected runtime-event wait to remain armed for sequence 2, got %d", m.waitRuntimeEventAfterFlushSequence)
	}
	if got := len(m.pendingRuntimeEvents); got != 1 {
		t.Fatalf("expected pending runtime events preserved before target flush, got %d", got)
	}
	for _, msg := range collectCmdMessages(t, firstCmd) {
		if _, ok := msg.(runtimeEventBatchMsg); ok {
			t.Fatalf("did not expect runtime rearm before target flush, got %T", msg)
		}
	}

	secondCmd := m.handleNativeHistoryFlush(nativeHistoryFlushMsg{Text: "second", Sequence: 2})
	if secondCmd == nil {
		t.Fatal("expected target flush to rearm runtime events")
	}
	var rearmed runtimeEventBatchMsg
	foundRearm := false
	for _, msg := range collectCmdMessages(t, secondCmd) {
		batch, ok := msg.(runtimeEventBatchMsg)
		if !ok {
			continue
		}
		rearmed = batch
		foundRearm = true
	}
	if !foundRearm {
		t.Fatal("expected runtime event batch after target flush")
	}
	if got := len(rearmed.events); got != 1 {
		t.Fatalf("expected exactly one rearmed pending runtime event, got %d", got)
	}
	if got := rearmed.events[0].Kind; got != clientui.EventConversationUpdated {
		t.Fatalf("rearmed event kind = %q, want %q", got, clientui.EventConversationUpdated)
	}
	if m.waitRuntimeEventAfterFlushSequence != 0 {
		t.Fatalf("expected runtime-event wait cleared after target flush, got %d", m.waitRuntimeEventAfterFlushSequence)
	}
	if got := len(m.pendingRuntimeEvents); got != 0 {
		t.Fatalf("expected pending runtime events drained after target flush, got %d", got)
	}
}

func TestRuntimeBatchNativeFlushArmsFlushFenceBeforeRearmingRuntimeEvents(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.pendingRuntimeEvents = []clientui.Event{{Kind: clientui.EventConversationUpdated}}

	next, cmd := m.handleRuntimeEventBatch([]clientui.Event{{
		Kind:                       clientui.EventUserMessageFlushed,
		UserMessage:                "prompt",
		CommittedTranscriptChanged: true,
		CommittedEntryCount:        1,
		CommittedEntryStart:        0,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
	}})
	m = next

	if m.waitRuntimeEventAfterFlushSequence == 0 {
		t.Fatal("expected runtime-event wait to be fenced behind native flush sequence")
	}
	if m.waitRuntimeEventAfterFlushSequence != m.nativeFlushSequence {
		t.Fatalf("wait flush sequence = %d, want native sequence %d", m.waitRuntimeEventAfterFlushSequence, m.nativeFlushSequence)
	}
	foundFlush := false
	for _, msg := range collectCmdMessages(t, cmd) {
		switch msg.(type) {
		case nativeHistoryFlushMsg:
			foundFlush = true
		case runtimeEventBatchMsg:
			t.Fatalf("runtime event rearmed before native flush ack: %+v", msg)
		}
	}
	if !foundFlush {
		t.Fatal("expected runtime batch to emit native history flush")
	}
	if got := len(m.pendingRuntimeEvents); got != 1 {
		t.Fatalf("pending runtime events drained before flush ack: %d", got)
	}
}

func TestRuntimeBatchHydrationWithNativeFlushWaitsForFlushAndHydration(t *testing.T) {
	client := &refreshingRuntimeClient{transcripts: []clientui.TranscriptPage{{
		SessionID:    "session-1",
		Revision:     2,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
	}}}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.pendingRuntimeEvents = []clientui.Event{{Kind: clientui.EventLocalEntryAdded}}

	next, cmd := m.handleRuntimeEventBatch([]clientui.Event{
		{
			Kind:                       clientui.EventUserMessageFlushed,
			UserMessage:                "prompt",
			CommittedTranscriptChanged: true,
			TranscriptRevision:         1,
			CommittedEntryCount:        1,
			CommittedEntryStart:        0,
			CommittedEntryStartSet:     true,
			TranscriptEntries:          []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
		},
		{
			Kind:                       clientui.EventConversationUpdated,
			CommittedTranscriptChanged: true,
			TranscriptRevision:         2,
			CommittedEntryCount:        2,
		},
	})
	m = next

	if !m.waitRuntimeEventAfterHydration {
		t.Fatal("expected hydration fence to remain armed")
	}
	if m.waitRuntimeEventAfterFlushSequence == 0 {
		t.Fatal("expected native flush fence to remain armed")
	}
	foundFlush := false
	foundHydration := false
	for _, msg := range collectCmdMessages(t, cmd) {
		switch msg.(type) {
		case nativeHistoryFlushMsg:
			foundFlush = true
		case runtimeTranscriptRefreshedMsg:
			foundHydration = true
		case runtimeEventBatchMsg:
			t.Fatalf("runtime event rearmed before flush+hydration completed: %+v", msg)
		}
	}
	if !foundFlush {
		t.Fatal("expected native flush command")
	}
	if !foundHydration {
		t.Fatal("expected hydration command")
	}
	if got := len(m.pendingRuntimeEvents); got != 1 {
		t.Fatalf("pending runtime events drained before fences cleared: %d", got)
	}
}

func TestNativeHistorySnapshotDoesNotReplaySameSessionRewriteInOngoingMode(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	initial := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ before"}},
	}}
	m.nativeProjection = initial
	m.nativeRenderedProjection = initial
	m.nativeRenderedSnapshot = initial.Render(tui.TranscriptDivider)
	m.nativeProjection = tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", Lines: []string{"❮ after"}},
	}}

	cmd := m.emitCurrentNativeHistorySnapshot(false, nativeHistoryReplayPermitNone)
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect same-session rewrite to replay committed scrollback, got %+v", msg)
		}
	}
	if got := m.nativeRenderedSnapshot; got != m.nativeProjection.Render(tui.TranscriptDivider) {
		t.Fatalf("expected rendered snapshot updated without replay, got %q", got)
	}
}

func TestNativeHistorySnapshotDoesNotAppendSuffixWhenVisibleRewriteTouchesRenderedFrontier(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.windowSizeKnown = true
	previous := tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", EntryIndex: 0, EntryEnd: 0, Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", EntryIndex: 1, EntryEnd: 1, Lines: []string{"❮ before"}},
	}}
	m.nativeProjection = tui.TranscriptProjection{Blocks: []tui.TranscriptProjectionBlock{
		{Role: "user", DividerGroup: "user", EntryIndex: 0, EntryEnd: 0, Lines: []string{"❯ commit/push"}},
		{Role: "assistant", DividerGroup: "assistant", EntryIndex: 1, EntryEnd: 1, Lines: []string{"❮ after"}},
		{Role: "compaction_notice", DividerGroup: "notice", EntryIndex: 3, EntryEnd: 3, Lines: []string{"context compacted for the 1st time"}},
	}}
	m.nativeRenderedProjection = previous
	m.nativeRenderedSnapshot = previous.Render(tui.TranscriptDivider)

	cmd := m.emitCurrentNativeHistorySnapshot(false, nativeHistoryReplayPermitNone)
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(nativeHistoryFlushMsg); ok {
			t.Fatalf("did not expect visible rewrite to append stale suffix after divergence, got %+v", msg)
		}
	}
}
