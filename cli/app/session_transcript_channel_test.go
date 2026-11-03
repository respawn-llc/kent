package app

import (
	"context"
	"core/cli/tui/ongoing"
	"core/shared/client"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"
	"errors"
	"io"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestTranscriptProtocolMismatchSurfacesOnceWithoutRetry(t *testing.T) {
	mismatch := &client.ProtocolVersionMismatchError{}
	stream := startSessionTranscriptEvents(t.Context(), "session-1",
		newTranscriptHistoryReader(t),
		func(context.Context, *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
			return nil, mismatch
		}, nil)
	defer stream.Stop()
	expectTranscriptHistory(t, stream.Events)
	event := nextTranscriptEvent(t, stream.Events)
	if event.Kind != ongoingTranscriptEventFailure || !errors.Is(event.Err, mismatch) {
		t.Fatalf("unexpected compatibility failure: %+v", event)
	}
	select {
	case _, open := <-stream.Events:
		if open {
			t.Fatal("protocol mismatch retried the subscription")
		}
	case <-time.After(time.Second):
		t.Fatal("incompatible subscription did not stop")
	}
}

func TestStartSessionTranscriptEventsWaitsForExplicitRehydrationAfterLoss(t *testing.T) {
	subscriber := &recordingTranscriptSubscriber{
		subs: []*scriptedTranscriptSubscription{
			{
				messages: []*transcriptpb.Message{ongoingHydrationMessage(1)},
				err:      serverapi.ErrStreamGap},
			{messages: []*transcriptpb.Message{ongoingHydrationMessage(1)}}}}
	stream := startSessionTranscriptEvents(context.Background(), "session-1", newTranscriptHistoryReader(t), subscriber.SubscribeSessionTranscript)
	defer stream.Stop()
	expectTranscriptHistory(t, stream.Events)

	first := nextTranscriptEvent(t, stream.Events)
	if first.Kind != ongoingTranscriptEventMessage || reflect.TypeOf(first.Message.Event.Payload) != reflect.TypeFor[*transcriptpb.Event_Hydration]() {
		t.Fatalf("first event = %+v, want hydration message", first)
	}
	loss := nextTranscriptEvent(t, stream.Events)
	if loss.Kind != ongoingTranscriptEventLoss || !errors.Is(loss.Err, serverapi.ErrStreamGap) {
		t.Fatalf("loss event = %+v, want stream gap loss", loss)
	}
	select {
	case event, ok := <-stream.Events:
		t.Fatalf("unexpected event before explicit rehydration request: ok=%v event=%+v", ok, event)
	case <-time.After(25 * time.Millisecond):
	}

	stream.RequestRehydration(ongoing.RehydrateReasonSequenceGap)
	if cutover := nextTranscriptEvent(t, stream.Events); cutover.Kind != ongoingTranscriptEventCutover {
		t.Fatalf("expected recovery cutover, got %+v", cutover)
	}
	second := nextTranscriptEvent(t, stream.Events)
	if second.Kind != ongoingTranscriptEventMessage || reflect.TypeOf(second.Message.Event.Payload) != reflect.TypeFor[*transcriptpb.Event_Hydration]() {
		t.Fatalf("second event = %+v, want reopened hydration message", second)
	}
	if got, want := subscriber.sessionIDs, []string{"session-1", "session-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subscribe session IDs = %v, want %v", got, want)
	}
}

func TestStartSessionTranscriptEventsLocalCancelClosesChannel(t *testing.T) {
	subscriber := &recordingTranscriptSubscriber{subs: []*scriptedTranscriptSubscription{{}}}
	stream := startSessionTranscriptEvents(context.Background(), "session-1", newTranscriptHistoryReader(t), subscriber.SubscribeSessionTranscript)
	expectTranscriptHistory(t, stream.Events)

	stream.Stop()

	select {
	case _, ok := <-stream.Events:
		if ok {
			t.Fatal("expected transcript events channel to close after local cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transcript events channel close")
	}
}

func TestStartSessionTranscriptEventsReopensOnLocalRehydrationRequest(t *testing.T) {
	subscriber := &recordingTranscriptSubscriber{
		subs: []*scriptedTranscriptSubscription{
			{messages: []*transcriptpb.Message{ongoingHydrationMessage(1)}},
			{messages: []*transcriptpb.Message{ongoingHydrationMessage(1)}}}}
	stream := startSessionTranscriptEvents(context.Background(), "session-1", newTranscriptHistoryReader(t), subscriber.SubscribeSessionTranscript)
	defer stream.Stop()
	expectTranscriptHistory(t, stream.Events)

	first := nextTranscriptEvent(t, stream.Events)
	if first.Kind != ongoingTranscriptEventMessage || first.Message.Sequence != 1 {
		t.Fatalf("first event = %+v, want hydration", first)
	}

	stream.RequestRehydration(ongoing.RehydrateReasonResize)

	cutover := nextTranscriptEvent(t, stream.Events)
	if cutover.Kind != ongoingTranscriptEventCutover || cutover.Reason == nil || *cutover.Reason != ongoing.RehydrateReasonResize {
		t.Fatalf("expected ordered resize cutover before hydration: %+v", cutover)
	}

	second := nextTranscriptEvent(t, stream.Events)
	if second.Kind != ongoingTranscriptEventMessage || second.Message.Sequence != 1 {
		t.Fatalf("second event = %+v, want reopened hydration", second)
	}
	if got, want := len(subscriber.sessionIDs), 2; got != want {
		t.Fatalf("subscribe count = %d, want %d", got, want)
	}
}

func TestStartSessionTranscriptEventsSurfacesSubscriptionOpenFailure(t *testing.T) {
	subscribeErr := errors.New("canonical hydration is invalid")
	stream := startSessionTranscriptEvents(
		context.Background(),
		"session-1",
		newTranscriptHistoryReader(t),
		func(context.Context, *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
			return nil, subscribeErr
		},
	)
	defer stream.Stop()
	expectTranscriptHistory(t, stream.Events)

	failure := nextTranscriptEvent(t, stream.Events)
	if failure.Kind != ongoingTranscriptEventFailure || !errors.Is(failure.Err, subscribeErr) {
		t.Fatalf("subscription failure event = %+v, want terminal open failure", failure)
	}
	select {
	case _, ok := <-stream.Events:
		if ok {
			t.Fatal("transcript event stream remained open after subscription failure")
		}
	case <-time.After(time.Second):
		t.Fatal("transcript event stream did not close after subscription failure")
	}
}

func TestStartSessionTranscriptEventsRetriesTransportFailureUntilServerReturns(t *testing.T) {
	initial := &scriptedTranscriptSubscription{
		messages: []*transcriptpb.Message{ongoingHydrationMessage(1)},
		err:      &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}}
	recovered := &scriptedTranscriptSubscription{
		messages: []*transcriptpb.Message{ongoingHydrationMessage(1)}}
	attempt := 0
	var historyReads atomic.Int32
	readHistory := newTranscriptHistoryReader(t)
	connectionFailure := errors.Join(serverapi.ErrStreamFailed, io.EOF)
	stream := startSessionTranscriptEvents(
		context.Background(),
		"session-1",
		func(ctx context.Context, req *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
			historyReads.Add(1)
			return readHistory(ctx, req)
		},
		func(context.Context, *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
			attempt++
			switch attempt {
			case 1:
				return initial, nil
			case 2:
				return nil, connectionFailure
			default:
				return recovered, nil
			}
		})
	defer stream.Stop()
	expectTranscriptHistory(t, stream.Events)

	if event := nextTranscriptEvent(t, stream.Events); event.Kind != ongoingTranscriptEventMessage {
		t.Fatalf("initial event = %+v, want hydration", event)
	}
	if event := nextTranscriptEvent(t, stream.Events); event.Kind != ongoingTranscriptEventLoss {
		t.Fatalf("disconnect event = %+v, want subscription loss", event)
	}

	stream.RequestRehydration(ongoing.RehydrateReasonSequenceGap)
	if cutover := nextTranscriptEvent(t, stream.Events); cutover.Kind != ongoingTranscriptEventCutover {
		t.Fatalf("expected recovery cutover, got %+v", cutover)
	}
	failure := nextTranscriptEvent(t, stream.Events)
	if failure.Kind != ongoingTranscriptEventFailure || !errors.Is(failure.Err, connectionFailure) {
		t.Fatalf("reopen failure = %+v, want transport failure", failure)
	}
	hydration := nextTranscriptEvent(t, stream.Events)
	if hydration.Kind != ongoingTranscriptEventMessage || reflect.TypeOf(hydration.Message.Event.Payload) != reflect.TypeFor[*transcriptpb.Event_Hydration]() {
		t.Fatalf("recovered event = %+v, want hydration", hydration)
	}
	if attempt != 3 {
		t.Fatalf("subscription attempts = %d, want 3", attempt)
	}
	if got := historyReads.Load(); got != 1 {
		t.Fatalf("history reads = %d, want one initial read across reconnect", got)
	}
}

func TestStartSessionTranscriptEventsDeliversHistoryWhileSubscriptionWaits(t *testing.T) {
	subscribing := make(chan struct{})
	stream := startSessionTranscriptEvents(t.Context(), "session-1", newTranscriptHistoryReader(t),
		func(ctx context.Context, _ *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
			close(subscribing)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	defer stream.Stop()
	select {
	case <-subscribing:
	case <-time.After(time.Second):
		t.Fatal("subscription did not start")
	}
	expectTranscriptHistory(t, stream.Events)
}

func TestStartSessionTranscriptEventsReadsHistoryBeforeSubscribing(t *testing.T) {
	historyStarted := make(chan struct{})
	releaseHistory := make(chan struct{})
	subscribing := make(chan struct{})
	reader := newTranscriptHistoryReader(t)
	stream := startSessionTranscriptEvents(t.Context(), "session-1",
		func(ctx context.Context, req *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
			close(historyStarted)
			select {
			case <-releaseHistory:
				return reader(ctx, req)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		func(ctx context.Context, _ *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
			close(subscribing)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	defer stream.Stop()
	select {
	case <-historyStarted:
	case <-time.After(time.Second):
		t.Fatal("history read did not start")
	}
	select {
	case <-subscribing:
		t.Fatal("subscription started before history read completed")
	default:
	}
	close(releaseHistory)
	expectTranscriptHistory(t, stream.Events)
	select {
	case <-subscribing:
	case <-time.After(time.Second):
		t.Fatal("subscription did not start after history read")
	}
}

func TestStartSessionTranscriptEventsHistoryFailureStillSubscribes(t *testing.T) {
	readErr := errors.New("history unavailable")
	for _, test := range []struct {
		name     string
		response *transcriptpb.PageSuccess
		err      error
	}{
		{name: "read failure", err: readErr},
		{name: "read timeout", err: context.DeadlineExceeded},
		{name: "wrong session", response: &transcriptpb.PageSuccess{Transcript: &transcriptpb.Page{SessionId: "other-session"}}},
		{name: "missing page", response: &transcriptpb.PageSuccess{}},
		{name: "missing response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := startSessionTranscriptEvents(t.Context(), "session-1",
				func(context.Context, *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
					return test.response, test.err
				},
				func(context.Context, *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
					return &scriptedTranscriptSubscription{messages: []*transcriptpb.Message{ongoingHydrationMessage(1)}}, nil
				})
			defer stream.Stop()
			history := nextTranscriptEvent(t, stream.Events)
			if history.Kind != ongoingTranscriptEventHistory || history.Err == nil || history.Page != nil {
				t.Fatalf("expected history failure, got %+v", history)
			}
			if test.err != nil && !errors.Is(history.Err, test.err) {
				t.Fatalf("history error = %v, want %v", history.Err, test.err)
			}
			if event := nextTranscriptEvent(t, stream.Events); event.Kind != ongoingTranscriptEventMessage {
				t.Fatalf("expected live subscription hydration after failed history, got %+v", event)
			}
		})
	}
}

func TestStartSessionTranscriptEventsCancelStopsHistoryWithoutSubscribing(t *testing.T) {
	historyStarted := make(chan struct{})
	var subscriptions atomic.Int32
	stream := startSessionTranscriptEvents(t.Context(), "session-1",
		func(ctx context.Context, _ *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
			close(historyStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		func(context.Context, *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
			subscriptions.Add(1)
			return nil, errors.New("unexpected subscription")
		})
	defer stream.Stop()
	select {
	case <-historyStarted:
	case <-time.After(time.Second):
		t.Fatal("history read did not start")
	}
	stream.Stop()
	select {
	case event, open := <-stream.Events:
		if open {
			t.Fatalf("unexpected event after cancellation: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close event stream")
	}
	if subscriptions.Load() != 0 {
		t.Fatal("subscription started after history read was canceled")
	}
}

func newTranscriptHistoryReader(t *testing.T) sessionTranscriptHistoryReader {
	t.Helper()
	return func(ctx context.Context, req *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
		if req.SessionId.GetSessionId() != "session-1" || req.Direction != nil {
			t.Errorf("expected newest page for session-1, got %v", req)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > uiRuntimeHydrationReadTimeout {
			t.Error("history read must have the hydration read timeout")
		}
		return &transcriptpb.PageSuccess{Transcript: &transcriptpb.Page{SessionId: "session-1"}}, nil
	}
}

func expectTranscriptHistory(t *testing.T, events <-chan ongoingTranscriptEvent) {
	t.Helper()
	event := nextTranscriptEvent(t, events)
	if event.Kind != ongoingTranscriptEventHistory || event.Err != nil || event.Page.GetSessionId() != "session-1" {
		t.Fatalf("expected initial history page, got %+v", event)
	}
}

type recordingTranscriptSubscriber struct {
	sessionIDs []string
	subs       []*scriptedTranscriptSubscription
}

func (s *recordingTranscriptSubscriber) SubscribeSessionTranscript(_ context.Context, req *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
	s.sessionIDs = append(s.sessionIDs, req.SessionId.GetSessionId())
	if len(s.subs) == 0 {
		return nil, context.Canceled
	}
	sub := s.subs[0]
	s.subs = s.subs[1:]
	return sub, nil
}

type scriptedTranscriptSubscription struct {
	messages []*transcriptpb.Message
	err      error
	closed   bool
}

func (s *scriptedTranscriptSubscription) Next(ctx context.Context) (*transcriptpb.Message, error) {
	if len(s.messages) > 0 {
		message := s.messages[0]
		s.messages = s.messages[1:]
		return message, nil
	}
	if s.err != nil {
		return &transcriptpb.Message{}, s.err
	}
	<-ctx.Done()
	return &transcriptpb.Message{}, ctx.Err()
}

func (s *scriptedTranscriptSubscription) Close() error {
	s.closed = true
	return nil
}

func nextTranscriptEvent(t *testing.T, events <-chan ongoingTranscriptEvent) ongoingTranscriptEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("transcript events channel closed")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transcript event")
		return ongoingTranscriptEvent{}
	}
}
