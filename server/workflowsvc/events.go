package workflowsvc

import (
	"context"
	"io"
	"sync"
	"time"

	"core/server/workflowstore"
	"core/shared/apicontract"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const workflowProjectEventBufferSize = 64

type workflowProjectEventBroker struct {
	mu          sync.Mutex
	nextID      uint64
	closed      bool
	subscribers map[uint64]*workflowProjectSubscription
}

type workflowProjectSubscription struct {
	projectID  *string
	workflowID *runtimeids.WorkflowID
	ch         chan *pb.ProjectEvent
	onClose    func()

	mu   sync.Mutex
	err  error
	done bool
}

func newWorkflowProjectEventBroker() *workflowProjectEventBroker {
	return &workflowProjectEventBroker{subscribers: make(map[uint64]*workflowProjectSubscription)}
}

func (b *workflowProjectEventBroker) subscribe(projectID *string, workflowID *runtimeids.WorkflowID) (*workflowProjectSubscription, error) {
	sub := &workflowProjectSubscription{
		projectID:  projectID,
		workflowID: workflowID,
		ch:         make(chan *pb.ProjectEvent, workflowProjectEventBufferSize),
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		sub.closeWithError(io.EOF)
		return sub, nil
	}
	id := b.nextID
	b.nextID++
	sub.onClose = func() {
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
	}
	b.subscribers[id] = sub
	b.mu.Unlock()
	return sub, nil
}

func (b *workflowProjectEventBroker) PublishWorkflowEvent(_ context.Context, event workflowstore.WorkflowEventRecord) error {
	if b == nil {
		return nil
	}
	occurredAt := event.OccurredAtUnixMs
	if occurredAt == 0 {
		occurredAt = time.Now().UTC().UnixMilli()
	}
	resource, err := protoapi.WorkflowEventResource.Encode(string(event.Resource))
	if err != nil {
		return err
	}
	action, err := protoapi.WorkflowEventAction.Encode(string(event.Action))
	if err != nil {
		return err
	}
	var workflowID *string
	if event.WorkflowID != nil {
		value := event.WorkflowID.String()
		workflowID = &value
	}
	b.publish(&pb.ProjectEvent{
		ProjectId: event.ProjectID, WorkflowId: workflowID,
		Resource: resource, Action: action, PrimaryEntityId: event.PrimaryEntityID,
		RelatedIds: append([]string(nil), event.RelatedIDs...),
		OccurredAt: timestamppb.New(time.UnixMilli(occurredAt)),
	})
	return nil
}

func (b *workflowProjectEventBroker) publish(event *pb.ProjectEvent) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	subs := make([]*workflowProjectSubscription, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		if workflowSubscriptionMatches(sub, event) {
			subs = append(subs, sub)
		}
	}
	b.mu.Unlock()
	for _, sub := range subs {
		if !sub.publish(event) {
			sub.closeWithError(serverapi.ErrStreamGap)
		}
	}
}

func workflowProjectEventMatches(subscribedProjectID *string, eventProjectID *string) bool {
	return subscribedProjectID == nil || (eventProjectID != nil && *subscribedProjectID == *eventProjectID)
}

func workflowSubscriptionMatches(sub *workflowProjectSubscription, event *pb.ProjectEvent) bool {
	if sub.workflowID != nil {
		return event.ProjectId == nil && event.WorkflowId != nil && sub.workflowID.String() == *event.WorkflowId
	}
	return workflowProjectEventMatches(sub.projectID, event.ProjectId)
}

func (b *workflowProjectEventBroker) Close(err error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subs := make([]*workflowProjectSubscription, 0, len(b.subscribers))
	for id, sub := range b.subscribers {
		subs = append(subs, sub)
		delete(b.subscribers, id)
	}
	b.mu.Unlock()
	for _, sub := range subs {
		sub.closeWithError(err)
	}
}

func (s *workflowProjectSubscription) publish(event *pb.ProjectEvent) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return false
	}
	select {
	case s.ch <- event:
		return true
	default:
		return false
	}
}

func (s *workflowProjectSubscription) Next(ctx context.Context) (*pb.ProjectEvent, error) {
	if s == nil {
		return nil, io.EOF
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case event, ok := <-s.ch:
		if ok {
			return event, nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.err != nil {
			return nil, serverapi.NormalizeStreamError(s.err)
		}
		return nil, io.EOF
	}
}

func (s *workflowProjectSubscription) Close() error {
	if s == nil {
		return nil
	}
	s.closeWithError(io.EOF)
	return nil
}

func (s *workflowProjectSubscription) closeWithError(err error) {
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

var _ apicontract.WorkflowEventSubscription = (*workflowProjectSubscription)(nil)
