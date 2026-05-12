package registry

import (
	"context"
	"fmt"
	"io"
	"sync"

	"builder/shared/clientui"
	"builder/shared/serverapi"
)

type sessionActivityBroker struct {
	mu          sync.Mutex
	nextID      uint64
	nextSeq     uint64
	history     []clientui.Event
	closed      bool
	subscribers map[uint64]*sessionActivitySubscription
}

type sessionActivitySubscription struct {
	ch      chan clientui.Event
	onClose func()

	mu   sync.Mutex
	err  error
	done bool
}

func newSessionActivityBroker() *sessionActivityBroker {
	return &sessionActivityBroker{subscribers: make(map[uint64]*sessionActivitySubscription)}
}

func (b *sessionActivityBroker) Subscribe(afterSequence uint64) (*sessionActivitySubscription, error) {
	if b == nil {
		return nil, fmt.Errorf("session activity stream is unavailable: %w", serverapi.ErrSessionActivityUnavailable)
	}
	sub := &sessionActivitySubscription{ch: make(chan clientui.Event, sessionActivityBufferSize)}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		sub.closeWithError(io.EOF)
		return sub, nil
	}
	if afterSequence > 0 && !b.canReplayLocked(afterSequence) {
		b.mu.Unlock()
		return nil, fmt.Errorf("session activity cursor %d is outside retained range: %w", afterSequence, serverapi.ErrStreamGap)
	}
	id := b.nextID
	b.nextID++
	replay := b.replayAfterLocked(afterSequence)
	for _, evt := range replay {
		if !sub.publish(evt) {
			b.mu.Unlock()
			sub.closeWithError(serverapi.ErrStreamGap)
			return sub, nil
		}
	}
	sub.onClose = func() {
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
	}
	b.subscribers[id] = sub
	b.mu.Unlock()
	return sub, nil
}

func (b *sessionActivityBroker) Publish(evt clientui.Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.nextSeq++
	evt.Sequence = b.nextSeq
	b.history = append(b.history, evt)
	if len(b.history) > sessionActivityBufferSize {
		copy(b.history, b.history[len(b.history)-sessionActivityBufferSize:])
		b.history = b.history[:sessionActivityBufferSize]
	}
	subs := make([]*sessionActivitySubscription, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		subs = append(subs, sub)
	}
	b.mu.Unlock()
	for _, sub := range subs {
		if !sub.publish(evt) {
			sub.closeWithError(serverapi.ErrStreamGap)
		}
	}
}

func (b *sessionActivityBroker) canReplayLocked(afterSequence uint64) bool {
	if afterSequence == 0 || afterSequence == b.nextSeq {
		return true
	}
	if afterSequence > b.nextSeq {
		return false
	}
	if len(b.history) == 0 {
		return false
	}
	return afterSequence >= b.history[0].Sequence-1
}

func (b *sessionActivityBroker) replayAfterLocked(afterSequence uint64) []clientui.Event {
	if afterSequence == 0 || len(b.history) == 0 {
		return nil
	}
	replay := make([]clientui.Event, 0)
	for _, evt := range b.history {
		if evt.Sequence > afterSequence {
			replay = append(replay, evt)
		}
	}
	return replay
}

func (b *sessionActivityBroker) Close(err error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subs := make([]*sessionActivitySubscription, 0, len(b.subscribers))
	for id, sub := range b.subscribers {
		subs = append(subs, sub)
		delete(b.subscribers, id)
	}
	b.mu.Unlock()
	for _, sub := range subs {
		sub.closeWithError(err)
	}
}

func (s *sessionActivitySubscription) publish(evt clientui.Event) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return false
	}
	select {
	case s.ch <- evt:
		return true
	default:
		return false
	}
}

func (s *sessionActivitySubscription) Next(ctx context.Context) (clientui.Event, error) {
	if s == nil {
		return clientui.Event{}, io.EOF
	}
	select {
	case <-ctx.Done():
		return clientui.Event{}, ctx.Err()
	case evt, ok := <-s.ch:
		if ok {
			return evt, nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.err != nil {
			return clientui.Event{}, serverapi.NormalizeStreamError(s.err)
		}
		return clientui.Event{}, io.EOF
	}
}

func (s *sessionActivitySubscription) Close() error {
	if s == nil {
		return nil
	}
	s.closeWithError(io.EOF)
	return nil
}

func (s *sessionActivitySubscription) closeWithError(err error) {
	if s == nil {
		return
	}
	var onClose func()
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.err = err
	close(s.ch)
	onClose = s.onClose
	s.mu.Unlock()
	if onClose != nil {
		onClose()
	}
}

var _ serverapi.SessionActivitySubscription = (*sessionActivitySubscription)(nil)
