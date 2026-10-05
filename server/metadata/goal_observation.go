package metadata

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"core/server/goalview"
	"core/server/session"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
)

const goalObservationBufferSize = 64

type goalObservationBroker struct {
	mu       sync.Mutex
	nextID   uint64
	sessions map[string]map[uint64]*goalObservationSubscription
}

type goalObservationSubscription struct {
	broker    *goalObservationBroker
	sessionID string
	id        uint64
	ch        chan *runtimepb.GoalObservation
	done      chan struct{}
	closeOnce sync.Once

	mu       sync.Mutex
	err      error
	sequence uint64
	last     *runtimepb.GoalProjection
}

func newGoalObservationBroker() *goalObservationBroker {
	return &goalObservationBroker{sessions: make(map[string]map[uint64]*goalObservationSubscription)}
}

func (s *Store) SubscribeGoalObservation(
	ctx context.Context,
	req *runtimepb.GoalObserveRequest,
) (serverapi.GoalObservationSubscription, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("metadata store is required")
	}
	record, err := session.ResolvePersistedSessionRecord(ctx, s, strings.TrimSpace(req.SessionId))
	if err != nil {
		return nil, err
	}
	status, err := goalProjection(record.Meta)
	if err != nil {
		return nil, err
	}
	return s.goalObservations.subscribe(strings.TrimSpace(req.SessionId), status), nil
}

func goalProjection(meta *session.Meta) (*runtimepb.GoalProjection, error) {
	if meta == nil {
		return &runtimepb.GoalProjection{}, errors.New("persisted Session metadata is required")
	}
	availability, err := session.GoalAvailabilityFromMeta(*meta)
	if err != nil {
		return &runtimepb.GoalProjection{}, err
	}
	projectedAvailability := goalview.AvailabilityFromSession(availability)
	goal, err := goalview.CoreFromSessionState(meta.Goal)
	if err != nil {
		return nil, err
	}
	return &runtimepb.GoalProjection{
		Goal:         goal,
		Availability: &projectedAvailability,
	}, nil
}

func (b *goalObservationBroker) subscribe(
	sessionID string,
	status *runtimepb.GoalProjection,
) *goalObservationSubscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	subscription := &goalObservationSubscription{
		broker:    b,
		sessionID: sessionID,
		id:        b.nextID,
		ch:        make(chan *runtimepb.GoalObservation, goalObservationBufferSize),
		done:      make(chan struct{}),
		sequence:  1,
		last:      cloneGoalProjection(status),
	}
	subscription.ch <- &runtimepb.GoalObservation{
		Sequence: 1,
		Kind:     runtimepb.GoalObservationKind_GOAL_OBSERVATION_KIND_HYDRATION,
		Status:   cloneGoalProjection(status),
	}
	subscribers := b.sessions[sessionID]
	if subscribers == nil {
		subscribers = make(map[uint64]*goalObservationSubscription)
		b.sessions[sessionID] = subscribers
	}
	subscribers[subscription.id] = subscription
	return subscription
}

func (b *goalObservationBroker) publish(sessionID string, status *runtimepb.GoalProjection) {
	if b == nil {
		return
	}
	b.mu.Lock()
	subscribers := b.sessions[strings.TrimSpace(sessionID)]
	var overflowed []*goalObservationSubscription
	for id, subscription := range subscribers {
		if proto.Equal(subscription.last, status) {
			continue
		}
		subscription.sequence++
		message := &runtimepb.GoalObservation{
			Sequence: subscription.sequence,
			Kind:     runtimepb.GoalObservationKind_GOAL_OBSERVATION_KIND_UPDATE,
			Status:   cloneGoalProjection(status),
		}
		select {
		case subscription.ch <- message:
			subscription.last = cloneGoalProjection(status)
		default:
			delete(subscribers, id)
			overflowed = append(overflowed, subscription)
		}
	}
	if len(subscribers) == 0 {
		delete(b.sessions, strings.TrimSpace(sessionID))
	}
	b.mu.Unlock()
	for _, subscription := range overflowed {
		subscription.finish(errors.New("Goal observation subscriber is too slow"))
	}
}

func (b *goalObservationBroker) remove(subscription *goalObservationSubscription) {
	if b == nil || subscription == nil {
		return
	}
	b.mu.Lock()
	subscribers := b.sessions[subscription.sessionID]
	delete(subscribers, subscription.id)
	if len(subscribers) == 0 {
		delete(b.sessions, subscription.sessionID)
	}
	b.mu.Unlock()
}

func (s *goalObservationSubscription) Next(ctx context.Context) (*runtimepb.GoalObservation, error) {
	if s == nil {
		return &runtimepb.GoalObservation{}, io.EOF
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case message := <-s.ch:
		return message, nil
	case <-s.done:
		s.mu.Lock()
		err := s.err
		s.mu.Unlock()
		if err == nil {
			err = io.EOF
		}
		return &runtimepb.GoalObservation{}, err
	case <-ctx.Done():
		return &runtimepb.GoalObservation{}, context.Cause(ctx)
	}
}

func (s *goalObservationSubscription) Close() error {
	if s == nil {
		return nil
	}
	s.broker.remove(s)
	s.finish(io.EOF)
	return nil
}

func (s *goalObservationSubscription) finish(err error) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.done)
	})
}

func cloneGoalProjection(input *runtimepb.GoalProjection) *runtimepb.GoalProjection {
	return proto.Clone(input).(*runtimepb.GoalProjection)
}
