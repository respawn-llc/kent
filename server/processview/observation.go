package processview

import (
	"context"
	"errors"
	"maps"
	"time"

	processpb "core/shared/protoapi/gen/kent/api/process"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type processObservation struct {
	service   *ProcessViewService
	scope     *processpb.ObserveRequest
	lifecycle map[string]processLifecycle
	ctx       context.Context
	cancel    context.CancelCauseFunc
	results   chan *processpb.ListSuccess
	dirty     chan struct{}
}

func (s *ProcessViewService) ObserveProcesses(ctx context.Context, request *processpb.ObserveRequest) (serverapi.ProcessObservationSubscription, error) {
	initial, err := s.observationList(ctx, request)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancelCause(ctx)
	sub := &processObservation{
		service: s, scope: request, ctx: lifetime, cancel: cancel,
		results: make(chan *processpb.ListSuccess, 1), dirty: make(chan struct{}, 1),
	}
	if _, project := request.Scope.(*processpb.ObserveRequest_ProjectId); project {
		sub.lifecycle = lifecycleProjection(initial)
	}
	sub.results <- initial
	// Initial reading deliberately does not synchronize with change publication.
	s.mu.Lock()
	s.observers[sub] = struct{}{}
	s.mu.Unlock()
	go sub.run()
	return sub, nil
}

func (s *processObservation) run() {
	defer s.Close()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.dirty:
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		// Combine accumulated changes before projecting, not after building lists.
		select {
		case <-s.dirty:
		default:
		}
		list, err := s.service.observationList(s.ctx, s.scope)
		if err != nil {
			s.cancel(err)
			return
		}
		if s.lifecycle != nil {
			next := lifecycleProjection(list)
			if maps.EqualFunc(s.lifecycle, next, sameProcessLifecycle) {
				continue
			}
			s.lifecycle = next
		}
		select {
		case s.results <- list:
		default:
			s.cancel(serverapi.ErrStreamGap)
			return
		}
	}
}

func (s *ProcessViewService) BackgroundListChanged(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.observers {
		scope, session := sub.scope.Scope.(*processpb.ObserveRequest_SessionId)
		if !session || scope.SessionId == sessionID {
			select {
			case sub.dirty <- struct{}{}:
			default:
			}
		}
	}
}

func (s *ProcessViewService) observationList(ctx context.Context, request *processpb.ObserveRequest) (*processpb.ListSuccess, error) {
	switch scope := request.Scope.(type) {
	case *processpb.ObserveRequest_SessionId:
		return s.listSessionProcesses(scope.SessionId)
	case *processpb.ObserveRequest_ProjectId:
		return s.ListProcesses(ctx, &processpb.ListRequest{ProjectId: scope.ProjectId})
	default:
		return nil, errors.New("process observation scope is required")
	}
}

type processLifecycle struct {
	state                                string
	running, backgrounded, killRequested bool
	finishedAt                           *timestamppb.Timestamp
	exitCode                             *int64
}

func lifecycleProjection(list *processpb.ListSuccess) map[string]processLifecycle {
	result := make(map[string]processLifecycle, len(list.Processes))
	for _, process := range list.Processes {
		result[process.Id] = processLifecycle{
			state: process.State, running: process.Running, backgrounded: process.Backgrounded,
			killRequested: process.KillRequested, finishedAt: process.FinishedAt, exitCode: process.ExitCode,
		}
	}
	return result
}

func sameProcessLifecycle(left, right processLifecycle) bool {
	return left.state == right.state && left.running == right.running &&
		left.backgrounded == right.backgrounded && left.killRequested == right.killRequested &&
		proto.Equal(left.finishedAt, right.finishedAt) &&
		((left.exitCode == nil && right.exitCode == nil) ||
			(left.exitCode != nil && right.exitCode != nil && *left.exitCode == *right.exitCode))
}

func (s *processObservation) Next(ctx context.Context) (*processpb.ListSuccess, error) {
	if err := context.Cause(s.ctx); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, context.Cause(s.ctx)
	case list := <-s.results:
		return list, nil
	}
}

func (s *processObservation) Close() error {
	s.cancel(context.Canceled)
	s.service.mu.Lock()
	delete(s.service.observers, s)
	s.service.mu.Unlock()
	return nil
}
