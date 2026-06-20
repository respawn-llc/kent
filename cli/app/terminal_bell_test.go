package app

import (
	"strings"
	"sync"
	"testing"

	"core/shared/clientui"
)

type countRinger struct {
	mu    sync.Mutex
	count int
	last  string
}

func (r *countRinger) Notify(message string) {
	r.mu.Lock()
	r.count++
	r.last = message
	r.mu.Unlock()
}

func (r *countRinger) Bell() {
	r.mu.Lock()
	r.count++
	r.last = terminalBell
	r.mu.Unlock()
}

func (r *countRinger) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

func (r *countRinger) Last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

func newUnfocusedBellHooks(ringer *countRinger) *bellHooks {
	return newBellHooks(ringer, nil, func() bool { return false })
}

func TestUIResolvedAskEventDoesNotNotifyBellHook(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)
	m := newProjectedStaticUIModel(WithUIAskNotificationHook(hooks))

	next, _ := m.Update(askEventMsg{event: askEvent{resolvedPromptID: "ask-1"}})
	_ = next.(*uiModel)

	if got := ringer.Count(); got != 0 {
		t.Fatalf("resolved ask notification count = %d, want 0", got)
	}
}

func TestBellHooksSuppressTurnCompletionWhileFocused(t *testing.T) {
	ringer := &countRinger{}
	hooks := newBellHooks(ringer, nil, func() bool { return true })

	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantMessage, StepID: "step-1", TranscriptEntries: []clientui.ChatEntry{{Role: "assistant", Text: "done"}}})
	hooks.OnTurnQueueDrained()

	if got := ringer.Count(); got != 0 {
		t.Fatalf("ring count while focused = %d, want 0", got)
	}
}

func TestBellHooksRingOnToolHeavyTurnEndWithUnknownFocus(t *testing.T) {
	ringer := &countRinger{}
	focus := newTerminalFocusState()
	hooks := newBellHooks(ringer, nil, focus.FocusedForAttention)

	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantMessage, StepID: "step-1", TranscriptEntries: []clientui.ChatEntry{{Role: "assistant", Text: "done"}}})
	hooks.OnTurnQueueDrained()

	if got := ringer.Count(); got != 1 {
		t.Fatalf("ring count with unknown focus = %d, want 1", got)
	}
}

func TestBellHooksNoopAssistantDeltaClearsPendingTurnCompletion(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)

	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantMessage, StepID: "step-1", TranscriptEntries: []clientui.ChatEntry{{Role: "assistant", Text: "working"}}})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantDelta, StepID: "step-2", AssistantDelta: uiNoopFinalToken})
	hooks.OnTurnQueueDrained()

	if got := ringer.Count(); got != 0 {
		t.Fatalf("ring count = %d after NO_OP delta drain, want 0", got)
	}
}

func TestBellHooksNoopAssistantMessageClearsPendingTurnCompletion(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)

	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantMessage, StepID: "step-1", TranscriptEntries: []clientui.ChatEntry{{Role: "assistant", Text: "working"}}})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantMessage, StepID: "step-2", TranscriptEntries: []clientui.ChatEntry{{Role: "assistant", Text: "NO_OP"}}})
	hooks.OnTurnQueueDrained()

	if got := ringer.Count(); got != 0 {
		t.Fatalf("ring count = %d after NO_OP assistant message drain, want 0", got)
	}
}

func TestFormatAssistantPreview(t *testing.T) {
	if got := formatAssistantPreview("\n  hello\tworld  ", 80); got != "hello world" {
		t.Fatalf("preview = %q, want %q", got, "hello world")
	}

	if got := formatAssistantPreview("", 80); got != "" {
		t.Fatalf("preview = %q, want empty", got)
	}

	if got := formatAssistantPreview("abcdef", 4); got != "abc…" {
		t.Fatalf("preview = %q, want %q", got, "abc…")
	}

	long := strings.Repeat("a", terminalNotificationPreviewLimit+5)
	want := strings.Repeat("a", terminalNotificationPreviewLimit-1) + "…"
	if got := formatAssistantPreview(long, terminalNotificationPreviewLimit); got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}

	if got := formatAssistantPreview("ab\x1bcd\a ef", 80); got != "abcd ef" {
		t.Fatalf("preview = %q, want %q", got, "abcd ef")
	}
}

func TestBellHooksIgnoresMismatchedTurnEndStep(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)

	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantMessage, StepID: "step-2"})
	hooks.OnTurnQueueDrained()

	if got := ringer.Count(); got != 0 {
		t.Fatalf("ring count = %d, want 0", got)
	}
}

func TestBellHooksClearPendingTurnCompletionAfterAbort(t *testing.T) {
	ringer := &countRinger{}
	hooks := newUnfocusedBellHooks(ringer)

	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventToolCallStarted, StepID: "step-1"})
	hooks.OnProjectedRuntimeEvent(clientui.Event{Kind: clientui.EventAssistantMessage, StepID: "step-1", TranscriptEntries: []clientui.ChatEntry{{Role: "assistant", Text: "first"}}})
	hooks.OnTurnQueueAborted()
	hooks.OnTurnQueueDrained()

	if got := ringer.Count(); got != 0 {
		t.Fatalf("ring count = %d after aborted queue, want 0", got)
	}
}

func TestBellHooksSuppressUserCompactionCompletionWhileFocused(t *testing.T) {
	ringer := &countRinger{}
	hooks := newBellHooks(ringer, nil, func() bool { return true })

	hooks.OnUserCompactionCompleted(true)

	if got := ringer.Count(); got != 0 {
		t.Fatalf("ring count while focused = %d, want 0", got)
	}
}
