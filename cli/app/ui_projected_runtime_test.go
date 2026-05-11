package app

import (
	"builder/cli/tui"
	"builder/server/llm"
	"builder/shared/clientui"
	"builder/shared/serverapi"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type sessionActivityTestSubscription struct {
	events chan clientui.Event
}

func (s *sessionActivityTestSubscription) Next(ctx context.Context) (clientui.Event, error) {
	select {
	case <-ctx.Done():
		return clientui.Event{}, ctx.Err()
	case evt := <-s.events:
		return evt, nil
	}
}

func (s *sessionActivityTestSubscription) Close() error { return nil }

var _ serverapi.SessionActivitySubscription = (*sessionActivityTestSubscription)(nil)

func TestWaitRuntimeEventReturnsProjectedMessage(t *testing.T) {
	ch := make(chan clientui.Event, 1)
	ch <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "hello"}
	cmd := waitRuntimeEvent(ch)
	msg, ok := cmd().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", cmd())
	}
	if len(msg.events) != 1 {
		t.Fatalf("events len = %d, want 1", len(msg.events))
	}
	if msg.events[0].Kind != clientui.EventAssistantDelta || msg.events[0].AssistantDelta != "hello" {
		t.Fatalf("unexpected projected event: %+v", msg.events[0])
	}
}

func TestWaitRuntimeEventDrainsQueuedBatch(t *testing.T) {
	ch := make(chan clientui.Event, 3)
	ch <- clientui.Event{Kind: clientui.EventRunStateChanged, RunState: &clientui.RunState{Lifecycle: clientui.RunningRunLifecycle(clientui.RunModeTurn)}}
	ch <- clientui.Event{Kind: clientui.EventRunStateChanged, RunState: &clientui.RunState{Lifecycle: clientui.IdleRunLifecycle()}}
	ch <- clientui.Event{Kind: clientui.EventRunStateChanged, RunState: &clientui.RunState{Lifecycle: clientui.RunningRunLifecycle(clientui.RunModeTurn)}}
	msg, ok := waitRuntimeEvent(ch)().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", waitRuntimeEvent(ch)())
	}
	if len(msg.events) != 3 {
		t.Fatalf("events len = %d, want 3", len(msg.events))
	}
}

func TestWaitRuntimeEventDoesNotCoalesceAssistantDeltas(t *testing.T) {
	ch := make(chan clientui.Event, 3)
	ch <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "hello"}
	ch <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: " world"}
	ch <- clientui.Event{Kind: clientui.EventRunStateChanged, RunState: &clientui.RunState{Lifecycle: clientui.RunningRunLifecycle(clientui.RunModeTurn)}}

	msg, ok := waitRuntimeEvent(ch)().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", waitRuntimeEvent(ch)())
	}
	if len(msg.events) != 1 {
		t.Fatalf("events len = %d, want 1", len(msg.events))
	}
	if msg.events[0].Kind != clientui.EventAssistantDelta || msg.events[0].AssistantDelta != "hello" {
		t.Fatalf("first streamed event = %+v, want first assistant delta", msg.events[0])
	}
	nextMsg, ok := waitRuntimeEvent(ch)().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected second runtimeEventBatchMsg, got %T", waitRuntimeEvent(ch)())
	}
	if len(nextMsg.events) != 1 {
		t.Fatalf("second events len = %d, want 1", len(nextMsg.events))
	}
	if nextMsg.events[0].Kind != clientui.EventAssistantDelta || nextMsg.events[0].AssistantDelta != " world" {
		t.Fatalf("second streamed event = %+v, want second assistant delta", nextMsg.events[0])
	}
}

func TestWaitRuntimeEventFencesTranscriptEventIntoCarry(t *testing.T) {
	ch := make(chan clientui.Event, 3)
	ch <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "hello"}
	ch <- clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		UserMessage:                "steer now",
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "steer now",
		}},
	}
	ch <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "after"}

	msg, ok := waitRuntimeEvent(ch)().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", waitRuntimeEvent(ch)())
	}
	if len(msg.events) != 1 {
		t.Fatalf("events len = %d, want 1", len(msg.events))
	}
	if msg.events[0].Kind != clientui.EventAssistantDelta {
		t.Fatalf("first event kind = %q, want assistant_delta", msg.events[0].Kind)
	}
	if msg.carry != nil {
		t.Fatalf("did not expect carry when assistant deltas fence batching, got %+v", *msg.carry)
	}
	nextMsg, ok := waitRuntimeEvent(ch)().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected second runtimeEventBatchMsg, got %T", waitRuntimeEvent(ch)())
	}
	if len(nextMsg.events) != 1 || nextMsg.events[0].Kind != clientui.EventUserMessageFlushed {
		t.Fatalf("expected transcript event to be delivered on next wait, got %+v", nextMsg.events)
	}
}

func TestWaitRuntimeEventPrefersRichCommittedEventOverBareCommittedConversationUpdate(t *testing.T) {
	ch := make(chan clientui.Event, 2)
	ch <- clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        1,
	}
	ch <- clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        1,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "final"}},
	}

	msg, ok := waitRuntimeEvent(ch)().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", waitRuntimeEvent(ch)())
	}
	if len(msg.events) != 1 {
		t.Fatalf("events len = %d, want 1", len(msg.events))
	}
	if msg.events[0].Kind != clientui.EventAssistantMessage {
		t.Fatalf("event kind = %q, want assistant_message", msg.events[0].Kind)
	}
	if msg.carry != nil {
		t.Fatalf("did not expect carry after rich committed event covered bare conversation update, got %+v", *msg.carry)
	}
}

func TestWaitRuntimeEventDoesNotSuppressCommittedConversationUpdateWhenNextEventAdvancesTailPastReplacement(t *testing.T) {
	ch := make(chan clientui.Event, 2)
	ch <- clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        3,
	}
	ch <- clientui.Event{
		Kind:                       clientui.EventLocalEntryAdded,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        4,
		CommittedEntryStart:        3,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "reviewer_status", Text: "Supervisor ran: no changes."}},
	}

	msg, ok := waitRuntimeEvent(ch)().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", waitRuntimeEvent(ch)())
	}
	if len(msg.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(msg.events))
	}
	if msg.events[0].Kind != clientui.EventConversationUpdated {
		t.Fatalf("event kind = %q, want conversation_updated", msg.events[0].Kind)
	}
	if msg.events[1].Kind != clientui.EventLocalEntryAdded {
		t.Fatalf("second event kind = %q, want local_entry_added", msg.events[1].Kind)
	}
	if msg.carry != nil {
		t.Fatalf("did not expect carry when same-step committed tail should batch with replacement update, got %+v", msg.carry)
	}
}

func TestPlainConversationUpdateDoesNotDelayCompactionNoticeAppend(t *testing.T) {
	m := newProjectedTestUIModel(&runtimeControlFakeClient{}, closedProjectedRuntimeEvents(), nil)
	m.startupCmds = nil
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "seed", Committed: true}}
	m.transcriptBaseOffset = 0
	m.transcriptTotalEntries = 1
	m.transcriptRevision = 10
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	next, cmd := m.Update(runtimeEventBatchMsg{events: []clientui.Event{{
		Kind:               clientui.EventConversationUpdated,
		StepID:             "step-1",
		TranscriptRevision: 11,
	}}})
	updated := next.(*uiModel)
	if updated.waitRuntimeEventAfterHydration {
		t.Fatal("did not expect plain conversation update to arm hydration fence")
	}
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect plain conversation update to trigger hydration, got %+v", msg)
		}
	}

	next, _ = updated.Update(runtimeEventBatchMsg{events: []clientui.Event{{
		Kind:                       clientui.EventLocalEntryAdded,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        2,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "compaction_notice",
			Text: "context compacted for the 1st time",
		}},
	}}})
	updated = next.(*uiModel)
	if got := len(updated.transcriptEntries); got != 2 {
		t.Fatalf("expected compaction notice appended immediately, got %+v", updated.transcriptEntries)
	}
	if got := updated.transcriptEntries[1].Role; got != "compaction_notice" {
		t.Fatalf("second transcript role = %q, want compaction_notice", got)
	}
}

func TestWaitRuntimeEventReturnsFencedFirstEventWithoutCoalescing(t *testing.T) {
	ch := make(chan clientui.Event, 2)
	ch <- clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		UserMessage:                "steer now",
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "user",
			Text: "steer now",
		}},
	}
	ch <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "after"}

	msg, ok := waitRuntimeEvent(ch)().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", waitRuntimeEvent(ch)())
	}
	if len(msg.events) != 1 {
		t.Fatalf("events len = %d, want 1", len(msg.events))
	}
	if msg.events[0].Kind != clientui.EventUserMessageFlushed {
		t.Fatalf("first event kind = %q, want user_message_flushed", msg.events[0].Kind)
	}
	if msg.carry != nil {
		t.Fatalf("did not expect carry when first event is already fenced, got %+v", *msg.carry)
	}
	if remaining := (<-ch); remaining.Kind != clientui.EventAssistantDelta || remaining.AssistantDelta != "after" {
		t.Fatalf("expected later delta to remain unread, got %+v", remaining)
	}
}

func TestWaitRuntimeEventCmdPrefersPendingCarryBeforeRuntimeChannel(t *testing.T) {
	runtimeEvents := make(chan clientui.Event, 1)
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "later"}
	m := newProjectedTestUIModel(nil, runtimeEvents, nil)
	m.pendingRuntimeEvents = []clientui.Event{{Kind: clientui.EventUserMessageFlushed, UserMessage: "steer now"}}

	first := m.waitRuntimeEventCmd()
	if first == nil {
		t.Fatal("expected wait command for pending carry event")
	}
	firstMsg, ok := first().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg for pending carry, got %T", first())
	}
	if len(firstMsg.events) != 1 || firstMsg.events[0].Kind != clientui.EventUserMessageFlushed {
		t.Fatalf("unexpected pending carry batch: %+v", firstMsg.events)
	}
	if len(m.pendingRuntimeEvents) != 0 {
		t.Fatalf("expected pending carry queue drained, got %+v", m.pendingRuntimeEvents)
	}

	second := m.waitRuntimeEventCmd()
	if second == nil {
		t.Fatal("expected wait command for runtime channel after carry drain")
	}
	secondMsg, ok := second().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg from runtime channel, got %T", second())
	}
	if len(secondMsg.events) != 1 || secondMsg.events[0].AssistantDelta != "later" {
		t.Fatalf("unexpected runtime channel batch after carry drain: %+v", secondMsg.events)
	}
}

func TestWaitRuntimeEventCmdStaysPausedWhileHydrationFenceIsArmed(t *testing.T) {
	runtimeEvents := make(chan clientui.Event, 1)
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "later"}
	m := newProjectedTestUIModel(nil, runtimeEvents, nil)
	m.waitRuntimeEventAfterHydration = true

	if cmd := m.waitRuntimeEventCmd(); cmd != nil {
		t.Fatalf("expected runtime wait to remain paused while hydration fence is armed, got %T", cmd())
	}
	if len(runtimeEvents) != 1 {
		t.Fatalf("expected runtime event to remain unread while hydration fence is armed, remaining=%d", len(runtimeEvents))
	}

	m.waitRuntimeEventAfterHydration = false
	cmd := m.waitRuntimeEventCmd()
	if cmd == nil {
		t.Fatal("expected runtime wait after hydration fence clears")
	}
	msg, ok := cmd().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg after hydration fence clears, got %T", cmd())
	}
	if len(msg.events) != 1 || msg.events[0].AssistantDelta != "later" {
		t.Fatalf("unexpected runtime event after hydration fence clears: %+v", msg.events)
	}
}

func TestConversationUpdateHydrationFencesLaterRuntimeEvents(t *testing.T) {
	client := &refreshingRuntimeClient{
		transcripts: []clientui.TranscriptPage{{
			SessionID:    "session-1",
			TotalEntries: 1,
			Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "hydrated"}},
		}},
	}
	runtimeEvents := make(chan clientui.Event, 1)
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "later"}
	m := newProjectedTestUIModel(client, runtimeEvents, nil)
	m.startupCmds = nil
	m.sawAssistantDelta = true
	m.reasoningLiveDirty = true
	m.forwardToView(tui.SetConversationMsg{Ongoing: "stale stream"})

	next, cmd := m.Update(runtimeEventMsg{event: clientui.Event{Kind: clientui.EventConversationUpdated, CommittedTranscriptChanged: true}})
	updated := next.(*uiModel)
	if !updated.waitRuntimeEventAfterHydration {
		t.Fatal("expected conversation update to arm hydration fence")
	}
	if updated.sawAssistantDelta {
		t.Fatal("expected conversation update committed-advance sync to clear assistant delta state before hydration")
	}
	if updated.reasoningLiveDirty {
		t.Fatal("expected conversation update committed-advance sync to clear reasoning live state before hydration")
	}
	if got := updated.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected conversation update committed-advance sync to clear stale ongoing text before hydration, got %q", got)
	}
	if len(runtimeEvents) != 1 {
		t.Fatalf("expected later runtime event to remain unread until hydration completes, remaining=%d", len(runtimeEvents))
	}
	msgs := collectCmdMessages(t, cmd)
	var refresh runtimeTranscriptRefreshedMsg
	refreshFound := false
	for _, msg := range msgs {
		switch typed := msg.(type) {
		case runtimeTranscriptRefreshedMsg:
			refresh = typed
			refreshFound = true
		case runtimeEventBatchMsg:
			t.Fatalf("did not expect runtime stream to resume before hydration completes, got %+v", typed)
		}
	}
	if !refreshFound {
		t.Fatalf("expected authoritative transcript refresh for conversation update, got %+v", msgs)
	}

	next, followCmd := updated.Update(refresh)
	updated = next.(*uiModel)
	if updated.waitRuntimeEventAfterHydration {
		t.Fatal("expected hydration fence cleared after transcript refresh applies")
	}
	followMsgs := collectCmdMessages(t, followCmd)
	resumed := false
	for _, msg := range followMsgs {
		batch, ok := msg.(runtimeEventBatchMsg)
		if !ok {
			continue
		}
		if len(batch.events) == 1 && batch.events[0].AssistantDelta == "later" {
			resumed = true
		}
	}
	if !resumed {
		t.Fatalf("expected runtime stream to resume after hydration, got %+v", followMsgs)
	}
}

func TestStreamGapInvalidatesTransientStateBeforeHydrationFence(t *testing.T) {
	client := &refreshingRuntimeClient{
		transcripts: []clientui.TranscriptPage{{
			SessionID:    "session-1",
			TotalEntries: 1,
			Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "hydrated after gap"}},
		}},
	}
	runtimeEvents := make(chan clientui.Event, 1)
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "later"}
	m := newProjectedTestUIModel(client, runtimeEvents, nil)
	m.startupCmds = nil
	m.sawAssistantDelta = true
	m.reasoningLiveDirty = true
	m.forwardToView(tui.SetConversationMsg{Ongoing: "stale stream"})

	next, cmd := m.Update(runtimeEventMsg{event: clientui.Event{Kind: clientui.EventStreamGap, RecoveryCause: clientui.TranscriptRecoveryCauseStreamGap}})
	updated := next.(*uiModel)
	if !updated.waitRuntimeEventAfterHydration {
		t.Fatal("expected stream gap to arm hydration fence after transient state invalidation")
	}
	if updated.sawAssistantDelta {
		t.Fatal("expected stream gap to clear assistant delta state before hydration")
	}
	if updated.reasoningLiveDirty {
		t.Fatal("expected stream gap to clear reasoning live state before hydration")
	}
	if got := updated.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected stream gap to clear stale ongoing text before hydration, got %q", got)
	}
	if len(runtimeEvents) != 1 {
		t.Fatalf("expected later runtime event to remain unread until hydration completes, remaining=%d", len(runtimeEvents))
	}
	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if batch, ok := msg.(runtimeEventBatchMsg); ok {
			t.Fatalf("did not expect runtime stream to resume before stream-gap hydration completes, got %+v", batch)
		}
	}
}

func TestHydratingClientAndLiveClientConvergeWithoutDuplicateCommittedRows(t *testing.T) {
	baseline := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
	}
	authoritative := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		TotalEntries: 2,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed"},
			{Role: "assistant", Text: "final", Phase: string(llm.MessagePhaseFinal)},
		},
	}
	committedFinal := clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        2,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "final", Phase: string(llm.MessagePhaseFinal)}},
	}

	hydratingEvents := make(chan clientui.Event, 1)
	hydratingClient := &runtimeControlFakeClient{transcript: authoritative}
	hydrating := newProjectedTestUIModel(hydratingClient, hydratingEvents, nil)
	hydrating.startupCmds = nil
	if cmd := hydrating.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	live := newProjectedTestUIModel(&runtimeControlFakeClient{}, closedProjectedRuntimeEvents(), nil)
	live.startupCmds = nil
	if cmd := live.runtimeAdapter().applyRuntimeTranscriptPage(clientui.TranscriptPageRequest{}, baseline); cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	hydrating.waitRuntimeEventAfterHydration = true
	refreshMsg, ok := hydrating.requestRuntimeCommittedConversationSync()().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatal("expected committed conversation sync to return runtimeTranscriptRefreshedMsg")
	}
	hydratingEvents <- committedFinal
	close(hydratingEvents)
	liveCmd := live.runtimeAdapter().handleProjectedRuntimeEvent(committedFinal)
	for _, msg := range collectCmdMessages(t, liveCmd) {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect live client committed event to require hydration, got %+v", msg)
		}
	}

	next, followCmd := hydrating.Update(refreshMsg)
	hydrating = next.(*uiModel)
	followMsgs := collectCmdMessages(t, followCmd)
	var resumed runtimeEventBatchMsg
	resumedFound := false
	for _, msg := range followMsgs {
		batch, ok := msg.(runtimeEventBatchMsg)
		if !ok {
			continue
		}
		resumed = batch
		resumedFound = true
	}
	if !resumedFound {
		t.Fatalf("expected hydrating client to resume queued live events after refresh, got %+v", followMsgs)
	}

	next, finalCmd := hydrating.Update(resumed)
	hydrating = next.(*uiModel)
	_ = finalCmd

	hydratingCommitted := stripANSIAndTrimRight(hydrating.view.OngoingCommittedSnapshot())
	liveCommitted := stripANSIAndTrimRight(live.view.OngoingCommittedSnapshot())
	if got := len(hydrating.transcriptEntries); got != 2 {
		t.Fatalf("hydrating client transcript entry count = %d, want 2", got)
	}
	if got := len(live.transcriptEntries); got != 2 {
		t.Fatalf("live client transcript entry count = %d, want 2", got)
	}
	if strings.Count(hydratingCommitted, "final") != 1 {
		t.Fatalf("expected hydrating client committed snapshot to contain final exactly once, got %q", hydratingCommitted)
	}
	if strings.Count(liveCommitted, "final") != 1 {
		t.Fatalf("expected live client committed snapshot to contain final exactly once, got %q", liveCommitted)
	}
	if hydratingCommitted != liveCommitted {
		t.Fatalf("expected hydrating and live clients to converge to same committed snapshot, hydrating=%q live=%q", hydratingCommitted, liveCommitted)
	}
}

func TestHydrationRetryErrorReleasesRuntimeEventFenceWhileRetryIsScheduled(t *testing.T) {
	previousRetryDelay := uiRuntimeHydrationRetryDelay
	uiRuntimeHydrationRetryDelay = time.Millisecond
	t.Cleanup(func() {
		uiRuntimeHydrationRetryDelay = previousRetryDelay
	})

	client := &refreshingRuntimeClient{}
	runtimeEvents := make(chan clientui.Event, 1)
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "later"}
	m := newProjectedTestUIModel(client, runtimeEvents, nil)
	m.startupCmds = nil
	m.runtimeTranscriptBusy = true
	m.runtimeTranscriptToken = 7
	m.waitRuntimeEventAfterHydration = true

	next, cmd := m.Update(runtimeTranscriptRefreshedMsg{token: 7, err: errors.New("temporary refresh failure")})
	updated := next.(*uiModel)
	if updated.waitRuntimeEventAfterHydration {
		t.Fatal("expected hydration retry path to release runtime event fence after failure")
	}
	if updated.runtimeTranscriptBusy {
		t.Fatal("expected hydration retry path to clear in-flight busy flag after failure")
	}
	msgs := collectCmdMessages(t, cmd)
	retryFound := false
	resumed := false
	for _, msg := range msgs {
		switch typed := msg.(type) {
		case runtimeTranscriptRetryMsg:
			retryFound = true
		case runtimeEventBatchMsg:
			if len(typed.events) == 1 && typed.events[0].AssistantDelta == "later" {
				resumed = true
			}
		}
	}
	if !retryFound {
		t.Fatalf("expected hydration retry to remain scheduled after failure, got %+v", msgs)
	}
	if !resumed {
		t.Fatalf("expected runtime event consumption to resume while retry is pending, got %+v", msgs)
	}
	if len(runtimeEvents) != 0 {
		t.Fatalf("expected resumed runtime wait to consume pending event, remaining=%d", len(runtimeEvents))
	}
}

func TestProjectedRuntimeEventUpdateStreamsAssistantDelta(t *testing.T) {
	m := NewProjectedUIModel(nil, make(chan clientui.Event), make(chan askEvent)).(*uiModel)
	next, _ := m.Update(runtimeEventMsg{event: clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "hello"}})
	updated := next.(*uiModel)
	if got := updated.view.OngoingStreamingText(); got != "hello" {
		t.Fatalf("expected projected delta to reach view, got %q", got)
	}
}

func TestProjectedRuntimeEventSequentialAssistantDeltasStayIncremental(t *testing.T) {
	runtimeEvents := make(chan clientui.Event, 2)
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "hello"}
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: " world"}
	m := newProjectedTestUIModel(nil, runtimeEvents, nil)
	m.startupCmds = nil

	first := m.waitRuntimeEventCmd()
	if first == nil {
		t.Fatal("expected first runtime wait command")
	}
	firstMsg, ok := first().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected first runtimeEventBatchMsg, got %T", first())
	}
	if len(firstMsg.events) != 1 || firstMsg.events[0].AssistantDelta != "hello" {
		t.Fatalf("unexpected first batch: %+v", firstMsg.events)
	}
	next, _ := m.Update(firstMsg)
	updated := next.(*uiModel)
	if got := updated.view.OngoingStreamingText(); got != "hello" {
		t.Fatalf("stream after first delta = %q, want hello", got)
	}

	second := updated.waitRuntimeEventCmd()
	if second == nil {
		t.Fatal("expected second runtime wait command")
	}
	secondMsg, ok := second().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected second runtimeEventBatchMsg, got %T", second())
	}
	if len(secondMsg.events) != 1 || secondMsg.events[0].AssistantDelta != " world" {
		t.Fatalf("unexpected second batch: %+v", secondMsg.events)
	}
	next, _ = updated.Update(secondMsg)
	updated = next.(*uiModel)
	if got := updated.view.OngoingStreamingText(); got != "hello world" {
		t.Fatalf("stream after second delta = %q, want hello world", got)
	}
}

func TestRuntimeModelSkipsBareCommittedConversationUpdateWhenRichCommittedEventImmediatelyFollows(t *testing.T) {
	runtimeEvents := make(chan clientui.Event, 2)
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        1,
	}
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        1,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "final"}},
	}
	close(runtimeEvents)
	m := newProjectedTestUIModel(&runtimeControlFakeClient{}, runtimeEvents, nil)
	m.startupCmds = nil

	first := m.waitRuntimeEventCmd()
	if first == nil {
		t.Fatal("expected runtime wait command")
	}
	msg, ok := first().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", first())
	}
	next, cmd := m.Update(msg)
	updated := next.(*uiModel)
	for _, follow := range collectCmdMessages(t, cmd) {
		if _, ok := follow.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect hydration when rich committed event already covered bare committed conversation update, got %+v", follow)
		}
	}
	if updated.runtimeTranscriptBusy {
		t.Fatal("did not expect runtime transcript sync to start")
	}
	if got := len(updated.transcriptEntries); got != 1 {
		t.Fatalf("transcript entry count = %d, want 1", got)
	}
	if got := updated.transcriptEntries[0].Text; got != "final" {
		t.Fatalf("assistant transcript text = %q, want final", got)
	}
}

func TestRuntimeModelHiddenCommittedSkipDoesNotTriggerFollowUpCommittedConversationHydrate(t *testing.T) {
	client := &runtimeControlFakeClient{transcript: clientui.TranscriptPage{SessionID: "session-1"}}
	runtimeEvents := make(chan clientui.Event, 2)
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventLocalEntryAdded,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         13,
		CommittedEntryCount:        8,
		CommittedEntryStart:        4,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "cache_warning", Text: "hidden-prefix-only"}},
	}
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         13,
		CommittedEntryCount:        8,
	}
	close(runtimeEvents)

	m := newProjectedTestUIModel(client, runtimeEvents, nil)
	m.startupCmds = nil
	m.transcriptEntries = []tui.TranscriptEntry{
		{Role: "assistant", Text: "visible-a", Phase: llm.MessagePhaseFinal},
		{Role: "reviewer_status", Text: "visible-b"},
	}
	m.transcriptBaseOffset = 5
	m.transcriptTotalEntries = 7
	m.transcriptRevision = 12
	m.forwardToView(tui.SetConversationMsg{BaseOffset: m.transcriptBaseOffset, TotalEntries: m.transcriptTotalEntries, Entries: m.transcriptEntries})

	first := m.waitRuntimeEventCmd()
	if first == nil {
		t.Fatal("expected runtime wait command for hidden committed event")
	}
	firstMsg, ok := first().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", first())
	}
	next, cmd := m.Update(firstMsg)
	updated := next.(*uiModel)
	followMsgs := collectCmdMessages(t, cmd)
	var secondMsg runtimeEventBatchMsg
	secondFound := false
	for _, follow := range followMsgs {
		if _, ok := follow.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect hidden committed event to trigger hydration, got %+v", follow)
		}
		typed, ok := follow.(runtimeEventBatchMsg)
		if ok {
			secondMsg = typed
			secondFound = true
		}
	}
	if updated.runtimeTranscriptBusy {
		t.Fatal("did not expect hidden committed event to start transcript hydration")
	}
	if !secondFound {
		t.Fatalf("expected follow-up committed conversation update to resume immediately, got %+v", followMsgs)
	}
	next, cmd = updated.Update(secondMsg)
	updated = next.(*uiModel)
	for _, follow := range collectCmdMessages(t, cmd) {
		if _, ok := follow.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect matching committed conversation_updated after hidden skip to trigger hydration, got %+v", follow)
		}
	}
	if updated.runtimeTranscriptBusy {
		t.Fatal("did not expect follow-up committed conversation_updated to start hydration after hidden skip")
	}
	if got := updated.transcriptRevision; got != 13 {
		t.Fatalf("transcript revision = %d, want 13", got)
	}
	if got := updated.transcriptTotalEntries; got != 8 {
		t.Fatalf("transcript total entries = %d, want 8", got)
	}
	if got := len(updated.transcriptEntries); got != 2 {
		t.Fatalf("visible transcript entry count = %d, want 2", got)
	}
}

func TestRuntimeModelReplacementAndSameStepTailConvergeWithoutDuplicateRows(t *testing.T) {
	client := &runtimeControlFakeClient{transcript: clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     11,
		TotalEntries: 4,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "replacement summary"},
			{Role: "reviewer_status", Text: "Supervisor ran: no changes."},
		},
	}}
	runtimeEvents := make(chan clientui.Event, 2)
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        3,
	}
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventLocalEntryAdded,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         11,
		CommittedEntryCount:        4,
		CommittedEntryStart:        3,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "reviewer_status", Text: "Supervisor ran: no changes."}},
	}
	close(runtimeEvents)
	m := newProjectedTestUIModel(client, runtimeEvents, nil)
	m.startupCmds = nil
	m.transcriptEntries = []tui.TranscriptEntry{{Role: "assistant", Text: "stale before replace"}}
	m.transcriptTotalEntries = 1
	m.transcriptRevision = 10
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries})

	first := m.waitRuntimeEventCmd()
	if first == nil {
		t.Fatal("expected runtime wait command")
	}
	msg, ok := first().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", first())
	}
	if got := len(msg.events); got != 2 {
		t.Fatalf("expected replacement update and same-step tail in one batch, got %+v", msg.events)
	}
	next, cmd := m.Update(msg)
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected hydration command for committed replacement update")
	}
	refresh, ok := cmd().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", cmd())
	}
	next, follow := updated.Update(refresh)
	updated = next.(*uiModel)
	pending := collectCmdMessages(t, follow)
	for len(pending) > 0 {
		msg := pending[0]
		pending = pending[1:]
		refreshMsg, ok := msg.(runtimeTranscriptRefreshedMsg)
		if !ok {
			continue
		}
		next, nextCmd := updated.Update(refreshMsg)
		updated = next.(*uiModel)
		pending = append(pending, collectCmdMessages(t, nextCmd)...)
	}
	if got := len(updated.transcriptEntries); got != 2 {
		t.Fatalf("transcript entry count = %d, want 2", got)
	}
	if got := updated.transcriptEntries[0].Text; got != "replacement summary" {
		t.Fatalf("first transcript entry = %q, want replacement summary", got)
	}
	if got := updated.transcriptEntries[1].Text; got != "Supervisor ran: no changes." {
		t.Fatalf("second transcript entry = %q, want reviewer status", got)
	}
}

func TestRuntimeModelRefreshesOngoingErrorOnDedicatedUpdateEvent(t *testing.T) {
	client := &runtimeControlFakeClient{transcript: clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     10,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
		OngoingError: "background continuation failed",
	}}
	runtimeEvents := make(chan clientui.Event, 1)
	runtimeEvents <- clientui.Event{Kind: clientui.EventOngoingErrorUpdated, StepID: "step-1"}
	close(runtimeEvents)
	m := newProjectedTestUIModel(client, runtimeEvents, nil)
	m.startupCmds = nil

	first := m.waitRuntimeEventCmd()
	if first == nil {
		t.Fatal("expected runtime wait command")
	}
	msg, ok := first().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", first())
	}
	next, cmd := m.Update(msg)
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected refresh command for ongoing error update")
	}
	refresh, ok := cmd().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", cmd())
	}
	next, _ = updated.Update(refresh)
	updated = next.(*uiModel)
	if got := updated.view.OngoingErrorText(); got != "background continuation failed" {
		t.Fatalf("ongoing error text = %q, want background continuation failed", got)
	}
}

func TestRuntimeModelOngoingErrorUpdatedSetsAndClearsBannerLifecycle(t *testing.T) {
	client := &refreshingRuntimeClient{
		transcripts: []clientui.TranscriptPage{
			{
				SessionID:    "session-1",
				Revision:     10,
				TotalEntries: 1,
				Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
				OngoingError: "background continuation failed",
			},
			{
				SessionID:    "session-1",
				Revision:     10,
				TotalEntries: 1,
				Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
				OngoingError: "",
			},
		},
	}
	runtimeEvents := make(chan clientui.Event, 2)
	runtimeEvents <- clientui.Event{Kind: clientui.EventOngoingErrorUpdated, StepID: "step-1"}
	runtimeEvents <- clientui.Event{Kind: clientui.EventOngoingErrorUpdated, StepID: "step-1"}
	close(runtimeEvents)
	m := newProjectedTestUIModel(client, runtimeEvents, nil)
	m.startupCmds = nil
	m.termWidth = 100
	m.termHeight = 20
	m.windowSizeKnown = true
	m.forwardToView(tui.SetViewportSizeMsg{Lines: 20, Width: 100})

	first := m.waitRuntimeEventCmd()
	if first == nil {
		t.Fatal("expected runtime wait command for ongoing error set")
	}
	firstMsg, ok := first().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", first())
	}
	next, cmd := m.Update(firstMsg)
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected refresh command for ongoing error set")
	}
	refresh, ok := cmd().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", cmd())
	}
	next, _ = updated.Update(refresh)
	updated = next.(*uiModel)
	if got := updated.view.OngoingErrorText(); got != "background continuation failed" {
		t.Fatalf("ongoing error text after set = %q, want background continuation failed", got)
	}

	second := updated.waitRuntimeEventCmd()
	if second == nil {
		t.Fatal("expected runtime wait command for ongoing error clear")
	}
	secondMsg, ok := second().(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtimeEventBatchMsg, got %T", second())
	}
	next, cmd = updated.Update(secondMsg)
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected refresh command for ongoing error clear")
	}
	refresh, ok = cmd().(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", cmd())
	}
	next, _ = updated.Update(refresh)
	updated = next.(*uiModel)
	if got := updated.view.OngoingErrorText(); got != "" {
		t.Fatalf("ongoing error text after clear = %q, want empty", got)
	}
}
