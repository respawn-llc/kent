package processview

import (
	"context"
	"time"

	processpb "core/shared/protoapi/gen/kent/api/process"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
)

type processObservation struct {
	service *ProcessViewService
	request *processpb.ListRequest
	ctx     context.Context
	cancel  context.CancelCauseFunc
	results chan *processpb.ListSuccess
	dirty   chan struct{}
}

func (s *ProcessViewService) ObserveProcesses(ctx context.Context, request *processpb.ObserveRequest) (serverapi.ProcessObservationSubscription, error) {
	listRequest := &processpb.ListRequest{ProjectId: request.ProjectId, OwnerSessionId: proto.String(request.SessionId)}
	initial, err := s.ListProcesses(ctx, listRequest)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancelCause(ctx)
	sub := &processObservation{
		service: s, request: listRequest, ctx: lifetime, cancel: cancel,
		results: make(chan *processpb.ListSuccess, 1), dirty: make(chan struct{}, 1),
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
		list, err := s.service.ListProcesses(s.ctx, s.request)
		if err != nil {
			s.cancel(err)
			return
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
		if sub.request.GetOwnerSessionId() == sessionID {
			select {
			case sub.dirty <- struct{}{}:
			default:
			}
		}
	}
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
