package app

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"builder/shared/clientui"
	"builder/shared/serverapi"
)

func useFastStreamResubscribeDelays(t *testing.T) {
	t.Helper()
	originalSessionDelay := sessionActivityResubscribeDelay
	originalPromptDelay := promptActivityResubscribeDelay
	sessionActivityResubscribeDelay = time.Millisecond
	promptActivityResubscribeDelay = time.Millisecond
	t.Cleanup(func() {
		sessionActivityResubscribeDelay = originalSessionDelay
		promptActivityResubscribeDelay = originalPromptDelay
	})
}

func TestStartSessionActivityEventsResubscribesFromLastSequenceAfterStreamGap(t *testing.T) {
	useFastStreamResubscribeDelays(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{{evt: clientui.Event{Sequence: 41, Kind: clientui.EventAssistantDelta, AssistantDelta: "first"}}, {err: serverapi.ErrStreamGap}}}
	resubscribed := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{{evt: clientui.Event{Sequence: 42, Kind: clientui.EventRunStateChanged, RunState: &clientui.RunState{Lifecycle: clientui.RunningRunLifecycle(clientui.RunModeTurn)}}}}}
	remaining := []serverapi.SessionActivitySubscription{resubscribed}
	var requestedAfter uint64

	events, stop := startSessionActivityEvents(ctx, initial, func(_ context.Context, afterSequence uint64) (serverapi.SessionActivitySubscription, error) {
		requestedAfter = afterSequence
		if len(remaining) == 0 {
			return nil, context.Canceled
		}
		next := remaining[0]
		remaining = remaining[1:]
		return next, nil
	}, func() bool { return false }, nil)
	defer stop()

	first := waitSessionActivityEvent(t, events)
	if first.Kind != clientui.EventAssistantDelta || first.AssistantDelta != "first" {
		t.Fatalf("unexpected initial event: %+v", first)
	}

	second := waitSessionActivityEvent(t, events)
	if second.Kind != clientui.EventRunStateChanged || second.RunState == nil || !second.RunState.Lifecycle.IsRunning() {
		t.Fatalf("unexpected resubscribed event: %+v", second)
	}
	if requestedAfter != 41 {
		t.Fatalf("resubscribe cursor = %d, want 41", requestedAfter)
	}
}

func TestStartSessionActivityEventsEmitsExplicitGapWhenCursorReplayUnavailable(t *testing.T) {
	useFastStreamResubscribeDelays(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{{evt: clientui.Event{Sequence: 41, Kind: clientui.EventAssistantDelta, AssistantDelta: "first"}}, {err: serverapi.ErrStreamGap}}}
	events, stop := startSessionActivityEvents(ctx, initial, func(context.Context, uint64) (serverapi.SessionActivitySubscription, error) {
		return nil, serverapi.ErrStreamGap
	}, func() bool { return false }, nil)
	defer stop()

	first := waitSessionActivityEvent(t, events)
	if first.Kind != clientui.EventAssistantDelta || first.AssistantDelta != "first" {
		t.Fatalf("unexpected initial event: %+v", first)
	}
	gap := waitSessionActivityEvent(t, events)
	if gap.Kind != clientui.EventStreamGap {
		t.Fatalf("expected explicit stream-gap event, got %+v", gap)
	}
	if gap.RecoveryCause != clientui.TranscriptRecoveryCauseStreamGap {
		t.Fatalf("stream-gap recovery cause = %q, want %q", gap.RecoveryCause, clientui.TranscriptRecoveryCauseStreamGap)
	}
}

func TestStartSessionActivityEventsEmitsExplicitGapWhenInitialStreamDropsWithoutCursor(t *testing.T) {
	useFastStreamResubscribeDelays(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{{err: io.EOF}}}
	resubscribed := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{{evt: clientui.Event{Sequence: 1, Kind: clientui.EventRunStateChanged, RunState: &clientui.RunState{Lifecycle: clientui.RunningRunLifecycle(clientui.RunModeTurn)}}}}}
	var requestedAfter []uint64
	events, stop := startSessionActivityEvents(ctx, initial, func(_ context.Context, afterSequence uint64) (serverapi.SessionActivitySubscription, error) {
		requestedAfter = append(requestedAfter, afterSequence)
		return resubscribed, nil
	}, func() bool { return false }, nil)
	defer stop()

	gap := waitSessionActivityEvent(t, events)
	if gap.Kind != clientui.EventStreamGap {
		t.Fatalf("expected explicit stream-gap event after initial stream drop, got %+v", gap)
	}
	if gap.RecoveryCause != clientui.TranscriptRecoveryCauseStreamGap {
		t.Fatalf("stream-gap recovery cause = %q, want %q", gap.RecoveryCause, clientui.TranscriptRecoveryCauseStreamGap)
	}
	if len(requestedAfter) != 1 || requestedAfter[0] != 0 {
		t.Fatalf("resubscribe cursors = %+v, want [0]", requestedAfter)
	}
	live := waitSessionActivityEvent(t, events)
	if live.Kind != clientui.EventRunStateChanged || live.RunState == nil || !live.RunState.Lifecycle.IsRunning() {
		t.Fatalf("expected live event after recovery resubscribe, got %+v", live)
	}
}

func TestStartSessionActivityEventsResubscribeStaysIsolatedAcrossStreams(t *testing.T) {
	useFastStreamResubscribeDelays(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initialA := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{{evt: clientui.Event{Sequence: 1, Kind: clientui.EventAssistantDelta, AssistantDelta: "a-first"}}, {err: serverapi.ErrStreamGap}}}
	resubA := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{{evt: clientui.Event{Sequence: 2, Kind: clientui.EventRunStateChanged, StepID: "step-a", RunState: &clientui.RunState{Lifecycle: clientui.RunningRunLifecycle(clientui.RunModeTurn)}}}}}
	remainingA := []serverapi.SessionActivitySubscription{resubA}

	initialB := &stubSessionActivitySubscription{steps: []stubSessionActivityStep{{evt: clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "b-first"}}}}

	eventsA, stopA := startSessionActivityEvents(ctx, initialA, func(context.Context, uint64) (serverapi.SessionActivitySubscription, error) {
		if len(remainingA) == 0 {
			return nil, context.Canceled
		}
		next := remainingA[0]
		remainingA = remainingA[1:]
		return next, nil
	}, func() bool { return false }, nil)
	defer stopA()
	eventsB, stopB := startSessionActivityEvents(ctx, initialB, func(context.Context, uint64) (serverapi.SessionActivitySubscription, error) {
		return nil, context.Canceled
	}, func() bool { return false }, nil)
	defer stopB()

	firstA := waitSessionActivityEvent(t, eventsA)
	if firstA.AssistantDelta != "a-first" {
		t.Fatalf("unexpected initial event for stream A: %+v", firstA)
	}
	firstB := waitSessionActivityEvent(t, eventsB)
	if firstB.AssistantDelta != "b-first" {
		t.Fatalf("unexpected initial event for stream B: %+v", firstB)
	}

	secondA := waitSessionActivityEvent(t, eventsA)
	if secondA.Kind != clientui.EventRunStateChanged || secondA.StepID != "step-a" {
		t.Fatalf("unexpected post-resubscribe event for stream A: %+v", secondA)
	}

	select {
	case evt := <-eventsB:
		t.Fatalf("unexpected cross-stream event on stream B: %+v", evt)
	default:
	}
}

func TestStartPendingPromptEventsResubscribesWithoutDuplicatingPendingPrompt(t *testing.T) {
	useFastStreamResubscribeDelays(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}}, {err: serverapi.ErrStreamGap}}}
	resubscribed := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}}, {evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-2", SessionID: "session-1", Question: "Second?"}}}}
	remaining := []serverapi.PromptActivitySubscription{resubscribed}
	var notified []clientui.PendingPromptEvent

	events, stop := startPendingPromptEvents(ctx, initial, func(context.Context, uint64) (serverapi.PromptActivitySubscription, error) {
		if len(remaining) == 0 {
			return nil, context.Canceled
		}
		next := remaining[0]
		remaining = remaining[1:]
		return next, nil
	}, stubPromptControlClient{}, staticControllerLeaseManager("lease-test-controller"), func(req clientui.PendingPromptEvent) {
		notified = append(notified, req)
	})
	defer stop()

	first := waitPromptEvent(t, events)
	if first.req.PromptID != "ask-1" || first.req.Question != "First?" {
		t.Fatalf("unexpected first prompt event: %+v", first.req)
	}

	second := waitPromptEvent(t, events)
	if second.req.PromptID != "ask-2" || second.req.Question != "Second?" {
		t.Fatalf("unexpected second prompt event: %+v", second.req)
	}
	if len(notified) != 2 || notified[0].PromptID != "ask-1" || notified[1].PromptID != "ask-2" {
		t.Fatalf("unexpected prompt notifications after resubscribe: %+v", notified)
	}

	select {
	case duplicate := <-events:
		t.Fatalf("unexpected duplicate pending prompt after resubscribe: %+v", duplicate.req)
	default:
	}
}

func TestStartPendingPromptEventsResubscribeEmitsResolutionForPromptMissingFromSnapshot(t *testing.T) {
	useFastStreamResubscribeDelays(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{{evt: clientui.PendingPromptEvent{Sequence: 1, Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}}, {err: serverapi.ErrStreamGap}}}
	resubscribed := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{
		{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-2", SessionID: "session-1", Question: "Second?"}},
		{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventSnapshot, SessionID: "session-1"}},
	}}
	events, stop := startPendingPromptEvents(ctx, initial, func(_ context.Context, afterSequence uint64) (serverapi.PromptActivitySubscription, error) {
		if afterSequence > 0 {
			return nil, serverapi.ErrStreamGap
		}
		return resubscribed, nil
	}, stubPromptControlClient{}, staticControllerLeaseManager("lease-test-controller"))
	defer stop()

	first := waitPromptEvent(t, events)
	if first.req.PromptID != "ask-1" {
		t.Fatalf("unexpected first prompt event: %+v", first.req)
	}
	resolved := waitPromptEvent(t, events)
	if !resolved.isResolution() || resolved.promptID() != "ask-1" {
		t.Fatalf("expected resolution event for ask-1 after resubscribe, got %+v", resolved)
	}
	second := waitPromptEvent(t, events)
	if second.req.PromptID != "ask-2" || second.req.Question != "Second?" {
		t.Fatalf("unexpected second prompt event: %+v", second.req)
	}
}

func TestStartPendingPromptEventsRetriesResubscribeWhenSnapshotStreamFails(t *testing.T) {
	useFastStreamResubscribeDelays(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{{evt: clientui.PendingPromptEvent{Sequence: 1, Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}}, {err: serverapi.ErrStreamGap}}}
	secondResubscribe := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{
		{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-2", SessionID: "session-1", Question: "Second?"}},
		{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventSnapshot, SessionID: "session-1"}},
	}}
	snapshotCalls := 0

	events, stop := startPendingPromptEvents(ctx, initial, func(_ context.Context, afterSequence uint64) (serverapi.PromptActivitySubscription, error) {
		if afterSequence > 0 {
			return nil, serverapi.ErrStreamGap
		}
		snapshotCalls++
		if snapshotCalls == 1 {
			return nil, errors.New("snapshot stream unavailable")
		}
		return secondResubscribe, nil
	}, stubPromptControlClient{}, staticControllerLeaseManager("lease-test-controller"))
	defer stop()

	first := waitPromptEvent(t, events)
	if first.req.PromptID != "ask-1" {
		t.Fatalf("unexpected first prompt event: %+v", first.req)
	}
	resolved := waitPromptEventWithin(t, events, 2*time.Second)
	if !resolved.isResolution() || resolved.promptID() != "ask-1" {
		t.Fatalf("expected resolution event for ask-1 after successful retry, got %+v", resolved)
	}
	second := waitPromptEventWithin(t, events, 2*time.Second)
	if second.req.PromptID != "ask-2" || second.req.Question != "Second?" {
		t.Fatalf("unexpected second prompt event: %+v", second.req)
	}
	if snapshotCalls != 2 {
		t.Fatalf("snapshot calls = %d, want 2", snapshotCalls)
	}
}

func TestPendingPromptEventRequeuesWhenAnswerRPCFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}}}}
	control := &retryingPromptControlClient{askErr: errors.New("transport down")}

	events, stop := startPendingPromptEvents(ctx, initial, func(context.Context, uint64) (serverapi.PromptActivitySubscription, error) {
		return nil, context.Canceled
	}, control, staticControllerLeaseManager("lease-test-controller"))
	defer stop()

	first := waitPromptEvent(t, events)
	if first.req.PromptID != "ask-1" {
		t.Fatalf("unexpected first prompt id: %q", first.req.PromptID)
	}
	first.reply <- askReply{response: clientui.PromptAnswer{PromptID: first.req.PromptID, Answer: "handled"}}

	retried := waitPromptEvent(t, events)
	if retried.req.PromptID != "ask-1" || retried.req.Question != "First?" {
		t.Fatalf("unexpected retried prompt event: %+v", retried.req)
	}
	if got := control.askCallCount(); got != 1 {
		t.Fatalf("AnswerAsk call count = %d, want 1", got)
	}
	if retried.reply == nil {
		t.Fatal("retried prompt reply channel is nil")
	}
	if retried.reply == first.reply {
		t.Fatal("retried prompt should use a fresh reply channel")
	}
	close(retried.reply)
	stop()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected prompt channel to close after stop")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt channel to close")
	}
}

func TestPendingPromptEventRetryAfterStopDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}}}}
	control := &retryingPromptControlClient{askErr: errors.New("transport down")}

	events, stop := startPendingPromptEvents(ctx, initial, func(context.Context, uint64) (serverapi.PromptActivitySubscription, error) {
		return nil, context.Canceled
	}, control, staticControllerLeaseManager("lease-test-controller"))

	first := waitPromptEvent(t, events)
	stop()
	first.reply <- askReply{response: clientui.PromptAnswer{PromptID: first.req.PromptID, Answer: "handled"}}
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected prompt channel to close after stop")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt channel to close")
	}
}

func TestStartPendingPromptEventsEmitsResolutionEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{
		{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}},
		{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventResolved, PromptID: "ask-1", SessionID: "session-1"}},
	}}

	events, stop := startPendingPromptEvents(ctx, initial, func(context.Context, uint64) (serverapi.PromptActivitySubscription, error) {
		return nil, context.Canceled
	}, stubPromptControlClient{}, staticControllerLeaseManager("lease-test-controller"))
	defer stop()

	first := waitPromptEvent(t, events)
	if first.req.PromptID != "ask-1" {
		t.Fatalf("unexpected first prompt event: %+v", first.req)
	}
	resolved := waitPromptEvent(t, events)
	if !resolved.isResolution() || resolved.promptID() != "ask-1" {
		t.Fatalf("expected resolution event for ask-1, got %+v", resolved)
	}
}

func TestPendingPromptEventDoesNotRequeueOnTerminalAnswerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}}}}
	control := &retryingPromptControlClient{askErr: serverapi.ErrPromptAlreadyResolved}

	events, stop := startPendingPromptEvents(ctx, initial, func(context.Context, uint64) (serverapi.PromptActivitySubscription, error) {
		return nil, context.Canceled
	}, control, staticControllerLeaseManager("lease-test-controller"))
	defer stop()

	first := waitPromptEvent(t, events)
	first.reply <- askReply{response: clientui.PromptAnswer{PromptID: first.req.PromptID, Answer: "handled"}}
	waitForPromptAskCallCount(t, control, 1)
	select {
	case retried := <-events:
		t.Fatalf("did not expect retry after terminal prompt error: %+v", retried.req)
	default:
	}
	if got := control.askCallCount(); got != 1 {
		t.Fatalf("AnswerAsk call count = %d, want 1", got)
	}
}

func TestPendingPromptEventDoesNotRequeueAfterPromptAlreadyResolvedLocally(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{
		{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}},
		{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventResolved, PromptID: "ask-1", SessionID: "session-1"}},
	}}
	control := &retryingPromptControlClient{askErr: errors.New("transport down")}

	events, stop := startPendingPromptEvents(ctx, initial, func(context.Context, uint64) (serverapi.PromptActivitySubscription, error) {
		return nil, context.Canceled
	}, control, staticControllerLeaseManager("lease-test-controller"))
	defer stop()

	first := waitPromptEvent(t, events)
	if first.req.PromptID != "ask-1" {
		t.Fatalf("unexpected first prompt event: %+v", first.req)
	}
	first.reply <- askReply{response: clientui.PromptAnswer{PromptID: first.req.PromptID, Answer: "handled"}}

	resolved := waitPromptEvent(t, events)
	if !resolved.isResolution() || resolved.promptID() != "ask-1" {
		t.Fatalf("expected prompt resolution event, got %+v", resolved)
	}
	waitForPromptAskCallCount(t, control, 1)
	select {
	case retried := <-events:
		t.Fatalf("did not expect stale retry after local resolution: %+v", retried.req)
	default:
	}
	if got := control.askCallCount(); got != 1 {
		t.Fatalf("AnswerAsk call count = %d, want 1", got)
	}
}

func TestPendingPromptEventRetryUsesLatestControllerLease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := &stubPromptActivitySubscription{steps: []stubPromptActivityStep{{evt: clientui.PendingPromptEvent{Type: clientui.PendingPromptEventPending, PromptID: "ask-1", SessionID: "session-1", Question: "First?"}}}}
	control := &retryingPromptControlClient{askErrors: []error{serverapi.ErrInvalidControllerLease, nil}}
	leaseManager := newControllerLeaseManager("lease-old")
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		return "lease-new", nil
	})

	events, stop := startPendingPromptEvents(ctx, initial, func(context.Context, uint64) (serverapi.PromptActivitySubscription, error) {
		return nil, context.Canceled
	}, control, leaseManager)
	defer stop()

	first := waitPromptEvent(t, events)
	first.reply <- askReply{response: clientui.PromptAnswer{PromptID: first.req.PromptID, Answer: "handled"}}

	waitForPromptAskCallCount(t, control, 2)
	if leases := control.askLeaseIDs(); len(leases) != 2 || leases[0] != "lease-old" || leases[1] != "lease-new" {
		t.Fatalf("ask lease ids = %+v, want [lease-old lease-new]", leases)
	}
	select {
	case evt := <-events:
		t.Fatalf("did not expect prompt requeue after successful lease recovery: %+v", evt.req)
	default:
	}
}

type stubSessionActivityStep struct {
	evt clientui.Event
	err error
}

type stubSessionActivitySubscription struct {
	steps  []stubSessionActivityStep
	closed bool
}

func (s *stubSessionActivitySubscription) Next(ctx context.Context) (clientui.Event, error) {
	if len(s.steps) == 0 {
		<-ctx.Done()
		return clientui.Event{}, ctx.Err()
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if step.err != nil {
		return clientui.Event{}, step.err
	}
	return step.evt, nil
}

func (s *stubSessionActivitySubscription) Close() error {
	s.closed = true
	return nil
}

type stubPromptActivityStep struct {
	evt clientui.PendingPromptEvent
	err error
}

type stubPromptActivitySubscription struct {
	steps  []stubPromptActivityStep
	closed bool
}

func (s *stubPromptActivitySubscription) Next(ctx context.Context) (clientui.PendingPromptEvent, error) {
	if len(s.steps) == 0 {
		<-ctx.Done()
		return clientui.PendingPromptEvent{}, ctx.Err()
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if step.err != nil {
		return clientui.PendingPromptEvent{}, step.err
	}
	return step.evt, nil
}

func (s *stubPromptActivitySubscription) Close() error {
	s.closed = true
	return nil
}

type stubPromptControlClient struct{}

func (stubPromptControlClient) AnswerAsk(context.Context, serverapi.AskAnswerRequest) error {
	return nil
}

func (stubPromptControlClient) AnswerApproval(context.Context, serverapi.ApprovalAnswerRequest) error {
	return nil
}

type retryingPromptControlClient struct {
	mu                 sync.Mutex
	askErr             error
	askErrors          []error
	approvalErr        error
	askCalls           int
	askLeaseIDValues   []string
	approvalCallCountV int
}

func (c *retryingPromptControlClient) askCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.askCalls
}

func (c *retryingPromptControlClient) askLeaseIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.askLeaseIDValues...)
}

func (c *retryingPromptControlClient) AnswerAsk(_ context.Context, req serverapi.AskAnswerRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.askCalls++
	c.askLeaseIDValues = append(c.askLeaseIDValues, req.ControllerLeaseID)
	if len(c.askErrors) > 0 {
		err := c.askErrors[0]
		c.askErrors = c.askErrors[1:]
		return err
	}
	return c.askErr
}

func (c *retryingPromptControlClient) AnswerApproval(context.Context, serverapi.ApprovalAnswerRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.approvalCallCountV++
	return c.approvalErr
}

func waitSessionActivityEvent(t *testing.T, events <-chan clientui.Event) clientui.Event {
	t.Helper()
	select {
	case evt := <-events:
		return evt
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session activity event")
		return clientui.Event{}
	}
}

func staticControllerLeaseManager(leaseID string) *controllerLeaseManager {
	return newControllerLeaseManager(leaseID)
}

func waitPromptEvent(t *testing.T, events <-chan askEvent) askEvent {
	return waitPromptEventWithin(t, events, time.Second)
}

func waitPromptEventWithin(t *testing.T, events <-chan askEvent, timeout time.Duration) askEvent {
	t.Helper()
	select {
	case evt := <-events:
		return evt
	case <-time.After(timeout):
		t.Fatal("timed out waiting for prompt event")
		return askEvent{}
	}
}

func waitForPromptAskCallCount(t *testing.T, control *retryingPromptControlClient, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := control.askCallCount(); got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("AnswerAsk call count = %d, want %d", control.askCallCount(), want)
}

var _ serverapi.SessionActivitySubscription = (*stubSessionActivitySubscription)(nil)
var _ serverapi.PromptActivitySubscription = (*stubPromptActivitySubscription)(nil)

func TestStubSubscriptionsSatisfyInterfaces(t *testing.T) {
	if errors.Is(nil, context.Canceled) {
		t.Fatal("unreachable")
	}
}
