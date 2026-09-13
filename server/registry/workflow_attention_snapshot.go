package registry

import (
	"context"
	"errors"
	"io"
	"sync"

	"core/shared/clientui"
	"core/shared/serverapi"
)

const workflowAttentionNotificationSnapshotPageSize = 32

type WorkflowAttentionNotificationSnapshot interface {
	Next(context.Context, func(clientui.AttentionNotification) error) error
}

type WorkflowAttentionNotificationSnapshotSource interface {
	OpenSnapshot(int) (WorkflowAttentionNotificationSnapshot, error)
}

func (r *RuntimeRegistry) WithWorkflowAttentionNotificationSnapshot(source WorkflowAttentionNotificationSnapshotSource) *RuntimeRegistry {
	if r == nil {
		return r
	}
	r.workflowAttentionSnapshot = source
	return r
}

type attentionNotificationStreamResult struct {
	event clientui.AttentionNotificationEvent
	err   error
}

type workflowAttentionNotificationSnapshotResult struct {
	notification clientui.AttentionNotification
	acknowledged chan struct{}
	err          error
}

type workflowAttentionNotificationSubscription struct {
	live        serverapi.AttentionNotificationSubscription
	source      func(context.Context, func(clientui.AttentionNotification) error) error
	worker      context.Context
	cancel      context.CancelFunc
	closed      chan struct{}
	start       sync.Once
	close       sync.Once
	workers     sync.WaitGroup
	liveOut     chan attentionNotificationStreamResult
	snapshotOut chan workflowAttentionNotificationSnapshotResult
	closeErr    error

	snapshotDone bool
	snapshotErr  error
	sequence     uint64
}

func newWorkflowAttentionNotificationSubscription(
	live serverapi.AttentionNotificationSubscription,
	source func(context.Context, func(clientui.AttentionNotification) error) error,
) serverapi.AttentionNotificationSubscription {
	worker, cancel := context.WithCancel(context.Background())
	return &workflowAttentionNotificationSubscription{
		live:        live,
		source:      source,
		worker:      worker,
		cancel:      cancel,
		closed:      make(chan struct{}),
		liveOut:     make(chan attentionNotificationStreamResult, 1),
		snapshotOut: make(chan workflowAttentionNotificationSnapshotResult, 1),
	}
}

func (s *workflowAttentionNotificationSubscription) Next(ctx context.Context) (clientui.AttentionNotificationEvent, error) {
	if s == nil {
		return clientui.AttentionNotificationEvent{}, io.EOF
	}
	select {
	case <-s.closed:
		return clientui.AttentionNotificationEvent{}, io.EOF
	default:
	}
	s.start.Do(s.startWorkers)
	for {
		if s.snapshotErr != nil {
			return s.stopWithError(s.snapshotErr)
		}
		if s.snapshotDone {
			select {
			case <-ctx.Done():
				return clientui.AttentionNotificationEvent{}, ctx.Err()
			case <-s.closed:
				return clientui.AttentionNotificationEvent{}, io.EOF
			case result := <-s.liveOut:
				return s.emitLive(result)
			}
		}
		select {
		case <-ctx.Done():
			return clientui.AttentionNotificationEvent{}, ctx.Err()
		case <-s.closed:
			return clientui.AttentionNotificationEvent{}, io.EOF
		case result := <-s.liveOut:
			return s.emitLive(result)
		case result := <-s.snapshotOut:
			event, done, err := s.emitSnapshot(result)
			if err != nil {
				return clientui.AttentionNotificationEvent{}, err
			}
			if done {
				continue
			}
			return event, nil
		}
	}
}

func (s *workflowAttentionNotificationSubscription) Close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	s.close.Do(func() {
		close(s.closed)
		s.cancel()
		s.closeErr = s.live.Close()
		s.workers.Wait()
	})
	closeErr = s.closeErr
	return closeErr
}

func (s *workflowAttentionNotificationSubscription) startWorkers() {
	s.workers.Add(2)
	go s.pumpLive()
	go s.pumpSnapshot()
}

func (s *workflowAttentionNotificationSubscription) pumpLive() {
	defer s.workers.Done()
	for {
		event, err := s.live.Next(s.worker)
		select {
		case s.liveOut <- attentionNotificationStreamResult{event: event, err: err}:
		case <-s.worker.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func (s *workflowAttentionNotificationSubscription) pumpSnapshot() {
	defer s.workers.Done()
	err := s.source(s.worker, func(notification clientui.AttentionNotification) error {
		acknowledged := make(chan struct{})
		select {
		case s.snapshotOut <- workflowAttentionNotificationSnapshotResult{notification: notification, acknowledged: acknowledged}:
		case <-s.worker.Done():
			return context.Cause(s.worker)
		}
		select {
		case <-acknowledged:
			return nil
		case <-s.worker.Done():
			return context.Cause(s.worker)
		}
	})
	if err == nil {
		err = io.EOF
	}
	select {
	case s.snapshotOut <- workflowAttentionNotificationSnapshotResult{err: err}:
	case <-s.worker.Done():
	}
}

func (r *RuntimeRegistry) visitAttentionSnapshot(ctx context.Context, emit func(clientui.AttentionNotification) error) error {
	if r.workflowAttentionSnapshot != nil {
		snapshot, err := r.workflowAttentionSnapshot.OpenSnapshot(workflowAttentionNotificationSnapshotPageSize)
		if err != nil {
			return err
		}
		for {
			err := snapshot.Next(ctx, emit)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
		}
	}
	return r.pendingPrompts.Visit(ctx, func(sessionID string, snapshot PendingPromptSnapshot) error {
		if snapshot.Request.AttentionTarget != nil && snapshot.Request.AttentionTarget.Kind == clientui.AttentionNotificationTargetWorkflowTask {
			return nil
		}
		event, err := r.attentionPendingEventFromPrompt(sessionID, snapshot, clientui.AttentionNotificationSourceSnapshot)
		if err != nil {
			return err
		}
		return emit(*event.Pending)
	})
}

func (s *workflowAttentionNotificationSubscription) emitLive(result attentionNotificationStreamResult) (clientui.AttentionNotificationEvent, error) {
	if result.err != nil {
		return s.stopWithError(result.err)
	}
	return s.withLocalSequence(result.event)
}

func (s *workflowAttentionNotificationSubscription) emitSnapshot(result workflowAttentionNotificationSnapshotResult) (clientui.AttentionNotificationEvent, bool, error) {
	if errors.Is(result.err, io.EOF) {
		s.snapshotDone = true
		return clientui.AttentionNotificationEvent{}, true, nil
	}
	if errors.Is(result.err, context.Canceled) && s.worker.Err() != nil {
		return clientui.AttentionNotificationEvent{}, false, io.EOF
	}
	if result.err != nil {
		event, err := s.stopWithError(result.err)
		return event, false, err
	}
	close(result.acknowledged)
	notification := result.notification
	event, err := s.withLocalSequence(clientui.AttentionNotificationEvent{
		Source:  clientui.AttentionNotificationSourceSnapshot,
		Type:    clientui.AttentionNotificationEventPending,
		Pending: &notification,
	})
	return event, false, err
}

func (s *workflowAttentionNotificationSubscription) stopWithError(err error) (clientui.AttentionNotificationEvent, error) {
	_ = s.Close()
	return clientui.AttentionNotificationEvent{}, err
}

func (s *workflowAttentionNotificationSubscription) withLocalSequence(event clientui.AttentionNotificationEvent) (clientui.AttentionNotificationEvent, error) {
	s.sequence++
	event.Sequence = s.sequence
	if err := serverapi.ValidateAttentionNotificationEvent(event); err != nil {
		return s.stopWithError(err)
	}
	return event, nil
}

var _ serverapi.AttentionNotificationSubscription = (*workflowAttentionNotificationSubscription)(nil)
