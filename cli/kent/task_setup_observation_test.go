package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"core/shared/apicontract"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"
)

type taskSetupObservationService struct {
	apicontract.WorkflowService
	subscription *taskSetupWaitingSubscription
}

func (s taskSetupObservationService) SubscribeWorktreeSetup(context.Context, *worktreepb.SetupSubscribeRequest) (apicontract.WorktreeSetupSubscription, error) {
	return s.subscription, nil
}

type taskSetupWaitingSubscription struct {
	closed chan struct{}
}

func (*taskSetupWaitingSubscription) Next(ctx context.Context) (*worktreepb.SetupEvent, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *taskSetupWaitingSubscription) Close() error {
	close(s.closed)
	return nil
}

func TestTaskSetupObservationBoundsPreparationRequest(t *testing.T) {
	subscription := &taskSetupWaitingSubscription{closed: make(chan struct{})}
	service := taskSetupObservationService{subscription: subscription}
	_, _, err := runWorkflowMutationWithSetupProgress(
		t.Context(), service, io.Discard,
		func(ctx context.Context, _ serverapi.WorkflowSetupOperationID) (bool, error) {
			deadline, present := ctx.Deadline()
			if !present || time.Until(deadline) > workflowTaskSetupObservationTimeout {
				t.Error("preparation request has no bounded observation deadline")
			}
			return false, nil
		},
		func(applied bool) bool { return applied },
	)
	if err != nil {
		t.Fatal(err)
	}
	<-subscription.closed
}

func TestTaskSetupObservationCancellationDoesNotReplayMutation(t *testing.T) {
	subscription := &taskSetupWaitingSubscription{closed: make(chan struct{})}
	service := taskSetupObservationService{subscription: subscription}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	_, _, err := runWorkflowMutationWithSetupProgress(
		ctx, service, io.Discard,
		func(ctx context.Context, _ serverapi.WorkflowSetupOperationID) (bool, error) {
			calls++
			cancel()
			<-ctx.Done()
			return false, ctx.Err()
		},
		func(applied bool) bool { return applied },
	)
	var observationErr *worktreeSetupObservationError
	if !errors.As(err, &observationErr) || !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("observation cancellation: calls=%d err=%v", calls, err)
	}
	<-subscription.closed
}
