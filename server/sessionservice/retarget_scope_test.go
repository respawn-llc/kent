package sessionservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/metadata"
	"core/server/operation"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/worktreecontract"
)

type gatedRetargetCommit struct {
	sessionRetargetMetadata
	started chan context.Context
	release chan struct{}
}

func (s gatedRetargetCommit) CommitSessionWorkspaceRetarget(ctx context.Context, plan metadata.SessionWorkspaceRetargetPlan, at time.Time) (metadata.SessionWorkspaceRetargetResult, error) {
	s.started <- ctx
	select {
	case <-s.release:
		return s.sessionRetargetMetadata.CommitSessionWorkspaceRetarget(ctx, plan, at)
	case <-ctx.Done():
		return metadata.SessionWorkspaceRetargetResult{}, context.Cause(ctx)
	}
}

func TestPublicSessionRetargetContinuesAfterCallerCancellation(t *testing.T) {
	fixture := newRealSessionRetargetFixture(t, false)
	gate := gatedRetargetCommit{
		sessionRetargetMetadata: fixture.metadata,
		started:                 make(chan context.Context, 1), release: make(chan struct{}),
	}
	service := newTestGlobalSessionLifecycleService(t, fixture.metadata.PersistenceRoot(), fixture.authority, fixture.metadata).
		WithWorkspaceRetargeter(fixture.retargeter(gate, retargetProcessSource{}))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.RetargetSessionWorkspace(ctx, &sessionlaunchpb.SessionRetargetWorkspaceRequest{
			SessionId: protoapi.SessionSelectorToProto(fixture.childID), WorkspaceRoot: fixture.targetWorkspaceRoot,
		})
		done <- err
	}()
	var operationCtx context.Context
	select {
	case operationCtx = <-gate.started:
	case err := <-done:
		t.Fatalf("retarget finished before commit gate: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("retarget did not reach commit")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("caller result: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller did not stop waiting")
	}
	if err := context.Cause(operationCtx); err != nil {
		close(gate.release)
		t.Fatalf("caller canceled retarget commit: %v", err)
	}
	close(gate.release)
	select {
	case <-operationCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("retarget did not finish")
	}
	target, err := fixture.metadata.ResolveSessionExecutionTarget(t.Context(), fixture.childID.String())
	if err != nil || canonicalRetargetTestPath(t, target.WorkspaceRoot) != canonicalRetargetTestPath(t, fixture.targetWorkspaceRoot) {
		t.Fatalf("retarget was not committed: %+v, %v", target, err)
	}
	if fixture.publisher[fixture.childID.String()] != 1 {
		t.Fatal("committed retarget did not publish Session identity")
	}
}

func TestSessionRetargetShutdownCancelsCommitBeforeJoining(t *testing.T) {
	fixture := newRealSessionRetargetFixture(t, false)
	sourceDir := fixture.child.Dir()
	gate := gatedRetargetCommit{
		sessionRetargetMetadata: fixture.metadata,
		started:                 make(chan context.Context, 1), release: make(chan struct{}),
	}
	service := newTestGlobalSessionLifecycleService(t, fixture.metadata.PersistenceRoot(), fixture.authority, fixture.metadata).
		WithWorkspaceRetargeter(fixture.retargeter(gate, retargetProcessSource{}))
	done := make(chan error, 1)
	go func() {
		_, err := service.RetargetSessionWorkspace(t.Context(), &sessionlaunchpb.SessionRetargetWorkspaceRequest{
			SessionId: protoapi.SessionSelectorToProto(fixture.childID), WorkspaceRoot: fixture.targetWorkspaceRoot,
		})
		done <- err
	}()
	select {
	case <-gate.started:
	case err := <-done:
		t.Fatalf("retarget finished before commit: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("retarget did not reach commit")
	}
	closed := make(chan error, 1)
	go func() { closed <- service.operations.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel and join commit")
	}
	select {
	case err := <-done:
		if !errors.Is(err, operation.ErrOperationOwnerClosed) {
			t.Fatalf("retarget shutdown result: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retarget caller did not receive shutdown failure")
	}
	target, err := fixture.metadata.ResolveSessionExecutionTarget(t.Context(), fixture.childID.String())
	if err != nil || target.WorkspaceRoot != fixture.sourceBinding.CanonicalRoot {
		t.Fatalf("canceled commit changed target: %+v, %v", target, err)
	}
	if fixture.child.Dir() != sourceDir || fixture.publisher[fixture.childID.String()] != 0 {
		t.Fatal("shutdown did not finish restoring the uncommitted retarget")
	}
}

type scheduledRetargetLifetime struct {
	started  chan context.Context
	canceled chan struct{}
	release  chan struct{}
}

func (s scheduledRetargetLifetime) RetargetWorkspace(
	scope operation.OperationScope,
	_ metadata.SessionWorkspaceRetargetRequest,
	_ *sessionlaunchpb.RuntimeStepOrigin,
	id worktreecontract.OperationID,
	_ func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error),
	scheduled func(*worktreepb.ScheduledAcknowledgement),
	_ func(error),
) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	ctx := scope.Context()
	s.started <- ctx
	scheduled(&worktreepb.ScheduledAcknowledgement{OperationId: id.String()})
	<-ctx.Done()
	close(s.canceled)
	<-s.release
	return nil, context.Cause(ctx)
}

func TestScheduledSessionRetargetRemainsOwnedUntilFinalCompletion(t *testing.T) {
	fixture := newSessionRemovalServiceFixture(t)
	retargeter := scheduledRetargetLifetime{
		started: make(chan context.Context, 1), canceled: make(chan struct{}), release: make(chan struct{}),
	}
	service := fixture.service.WithWorkspaceRetargeter(retargeter)
	defer func() {
		select {
		case <-retargeter.release:
		default:
			close(retargeter.release)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	response, err := service.RetargetSessionWorkspace(ctx, &sessionlaunchpb.SessionRetargetWorkspaceRequest{
		SessionId: protoapi.SessionSelectorToProto(fixture.session.Identity().ID), WorkspaceRoot: t.TempDir(),
	})
	if err != nil || response.Scheduled == nil {
		t.Fatalf("scheduled acknowledgement: %+v, %v", response, err)
	}
	operationCtx := <-retargeter.started
	cancel()
	if err := context.Cause(operationCtx); err != nil {
		close(retargeter.release)
		t.Fatalf("caller canceled acknowledged retarget: %v", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- service.operations.Close() }()
	select {
	case <-retargeter.canceled:
	case <-time.After(5 * time.Second):
		close(retargeter.release)
		t.Fatal("shutdown did not cancel acknowledged retarget")
	}
	select {
	case err := <-closed:
		close(retargeter.release)
		t.Fatalf("shutdown returned before retarget finished: %v", err)
	default:
	}
	close(retargeter.release)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join retarget completion")
	}
}

// Direct retargeter tests supply the same operation lifetime as its public callers.
func (s *SessionWorkspaceRetargeter) observeRetarget(
	t *testing.T,
	ctx context.Context,
	request metadata.SessionWorkspaceRetargetRequest,
	origin *sessionlaunchpb.RuntimeStepOrigin,
	id worktreecontract.OperationID,
	resolve func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error),
	completion func(error),
) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	t.Helper()
	owner := operation.NewOperationOwner()
	t.Cleanup(func() { _ = owner.Close() })
	type outcome struct {
		response *sessionlaunchpb.SessionRetargetWorkspaceSuccess
		err      error
	}
	reply := make(chan outcome, 1)
	_, err := owner.Start(ctx, func(scope operation.OperationScope) error {
		scheduled := false
		response, runErr := s.RetargetWorkspace(scope, request, origin, id, resolve,
			func(ack *worktreepb.ScheduledAcknowledgement) {
				scheduled = true
				reply <- outcome{response: &sessionlaunchpb.SessionRetargetWorkspaceSuccess{Scheduled: ack}}
			}, completion)
		if !scheduled {
			reply <- outcome{response: response, err: runErr}
		}
		return runErr
	})
	if err != nil {
		return nil, err
	}
	select {
	case result := <-reply:
		return result.response, result.err
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

func (s *SessionWorkspaceRetargeter) retargetWorkspace(t *testing.T, ctx context.Context, request metadata.SessionWorkspaceRetargetRequest) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	return s.observeRetarget(t, ctx, request, nil, worktreecontract.NewOperationID(),
		func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error) { return request, nil }, nil)
}

func (s *SessionWorkspaceRetargeter) scheduleWorkspaceRetarget(t *testing.T, ctx context.Context, request metadata.SessionWorkspaceRetargetRequest, origin *sessionlaunchpb.RuntimeStepOrigin, id worktreecontract.OperationID) (*worktreepb.ScheduledAcknowledgement, error) {
	return s.scheduleWorkspaceRetargetWithCompletion(t, ctx, request, origin, id,
		func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error) { return request, nil }, nil)
}

func (s *SessionWorkspaceRetargeter) scheduleWorkspaceRetargetWithCompletion(t *testing.T, ctx context.Context, request metadata.SessionWorkspaceRetargetRequest, origin *sessionlaunchpb.RuntimeStepOrigin, id worktreecontract.OperationID, resolve func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error), completion func(error)) (*worktreepb.ScheduledAcknowledgement, error) {
	_, err := s.observeRetarget(t, ctx, request, origin, id, resolve, completion)
	if err != nil {
		return nil, err
	}
	return &worktreepb.ScheduledAcknowledgement{OperationId: id.String()}, nil
}
