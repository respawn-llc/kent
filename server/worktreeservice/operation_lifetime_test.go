package worktree

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"core/server/metadata"
	"core/server/operation"
	"core/shared/clientui"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/worktreecontract"
)

type trackedWorktreeRetargeter struct {
	canceled chan struct{}
	release  chan struct{}
	context  chan context.Context
}

func (r trackedWorktreeRetargeter) RetargetWorkspace(
	scope operation.OperationScope,
	_ metadata.SessionWorkspaceRetargetRequest,
	_ *sessionlaunchpb.RuntimeStepOrigin,
	id worktreecontract.OperationID,
	_ func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error),
	scheduled func(*worktreepb.ScheduledAcknowledgement),
	completion func(error),
) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	ctx := scope.Context()
	r.context <- ctx
	scheduled(&worktreepb.ScheduledAcknowledgement{OperationId: id.String()})
	<-ctx.Done()
	close(r.canceled)
	<-r.release
	err := context.Cause(ctx)
	completion(err)
	return nil, err
}

func TestCrossWorkspaceEnterAcknowledgesBeforeCompletionAndJoinsOnShutdown(t *testing.T) {
	env := newServiceTestEnv(t)
	source := createNonGitSourceSession(t, env)
	retargeter := trackedWorktreeRetargeter{
		canceled: make(chan struct{}), release: make(chan struct{}), context: make(chan context.Context, 1),
	}
	defer func() {
		select {
		case <-retargeter.release:
		default:
			close(retargeter.release)
		}
	}()
	env.service.sessionRetargeter = retargeter
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	id := clientui.NewWorktreeTransitionID()
	done := make(chan error, 1)
	go func() {
		ack, err := env.service.EnterWorktree(ctx, &worktreepb.EnterRequest{
			OperationId: id.String(), Selector: "feature/deferred",
			SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{
				SessionId: source.Meta().SessionID,
			}},
			TargetWorkspace: &worktreepb.TransitionWorkspace{
				WorkspaceId: env.binding.WorkspaceID, WorkspaceRoot: env.workspaceRoot,
			},
		})
		if err == nil && ack.GetOperationId() != id.String() {
			err = errors.New("wrong Worktree transition acknowledgement")
		}
		done <- err
	}()
	if err := waitTerminal(t, done, "early Enter acknowledgement"); err != nil {
		t.Fatal(err)
	}
	operationCtx := waitTerminal(t, retargeter.context, "retarget operation scope")
	cancel()
	if err := context.Cause(operationCtx); err != nil {
		t.Fatalf("caller canceled acknowledged Enter: %v", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- env.service.operations.Close() }()
	waitTerminal(t, retargeter.canceled, "retarget shutdown cancellation")
	select {
	case err := <-closed:
		t.Fatalf("shutdown returned before Enter completion: %v", err)
	default:
	}
	close(retargeter.release)
	if err := waitTerminal(t, closed, "retarget shutdown join"); err != nil {
		t.Fatal(err)
	}
	env.publisher.mu.Lock()
	defer env.publisher.mu.Unlock()
	if len(env.publisher.outcomes) != 1 || env.publisher.outcomes[0].State != clientui.WorktreeTransitionFailed {
		t.Fatalf("final shutdown outcome: %+v", env.publisher.outcomes)
	}
}

type gatedMutationGitRunner struct {
	delegate gitCommandRunner
	command  []string
	started  chan context.Context
	release  chan struct{}
	once     sync.Once
}

func (r *gatedMutationGitRunner) gate(ctx context.Context, args []string) error {
	if len(args) < len(r.command) || !slices.Equal(args[:len(r.command)], r.command) {
		return nil
	}
	var err error
	r.once.Do(func() {
		r.started <- ctx
		select {
		case <-r.release:
		case <-ctx.Done():
			err = context.Cause(ctx)
		}
	})
	return err
}

func TestDormantWorktreeTransitionContinuesAfterCallerCancellation(t *testing.T) {
	for _, leaving := range []bool{false, true} {
		t.Run(map[bool]string{false: "enter", true: "leave"}[leaving], func(t *testing.T) {
			env := newServiceTestEnv(t)
			next := mustCreateWorktree(t, env, "feature/dormant-cancellation")
			selector := &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: env.session.Meta().SessionID}}
			enter := &worktreepb.EnterRequest{
				OperationId: clientui.NewWorktreeTransitionID().String(), SessionId: selector, Selector: next.WorktreeID,
				TargetWorkspace: &worktreepb.TransitionWorkspace{WorkspaceId: env.binding.WorkspaceID, WorkspaceRoot: env.workspaceRoot},
			}
			if leaving {
				if _, err := env.service.EnterWorktree(t.Context(), enter); err != nil {
					t.Fatal(err)
				}
			}
			runner := &gatedMutationGitRunner{
				delegate: execGitCommandRunner{}, command: []string{"worktree", "list"},
				started: make(chan context.Context, 1), release: make(chan struct{}),
			}
			env.service.git = NewGitInspector(runner)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if leaving {
					_, err := env.service.LeaveWorktree(ctx, &worktreepb.LeaveRequest{
						OperationId: clientui.NewWorktreeTransitionID().String(), SessionId: selector,
					})
					done <- err
					return
				}
				_, err := env.service.EnterWorktree(ctx, enter)
				done <- err
			}()
			operationCtx := waitTerminal(t, runner.started, "dormant transition Git read")
			cancel()
			if err := waitTerminal(t, done, "canceled transition caller"); !errors.Is(err, context.Canceled) {
				t.Fatalf("caller result: %v", err)
			}
			if err := context.Cause(operationCtx); err != nil {
				close(runner.release)
				t.Fatalf("caller canceled transition: %v", err)
			}
			close(runner.release)
			select {
			case <-operationCtx.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("transition did not finish")
			}
			if leaving {
				assertServiceTestSessionTarget(t, env, "", env.workspaceRoot)
			} else {
				assertServiceTestSessionTarget(t, env, next.WorktreeID, next.CanonicalRoot)
			}
		})
	}
}

func (r *gatedMutationGitRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, int, error) {
	if err := r.gate(ctx, args); err != nil {
		return nil, 1, err
	}
	return r.delegate.Run(ctx, dir, args...)
}

func (r *gatedMutationGitRunner) Output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if err := r.gate(ctx, args); err != nil {
		return nil, err
	}
	return r.delegate.Output(ctx, dir, args...)
}

func TestPublicWorktreeMutationContinuesAfterCallerCancellation(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "delete"}[deleting], func(t *testing.T) {
			env := newServiceTestEnv(t)
			var deleteRequest *worktreepb.DeleteRequest
			command := []string{"worktree", "add"}
			if deleting {
				created := mustCreateWorktree(t, env, "feature/caller-cancel-delete")
				deleteRequest = worktreeDeleteRequest(env, created.WorktreeID)
				command = []string{"worktree", "remove"}
			}
			runner := &gatedMutationGitRunner{
				delegate: execGitCommandRunner{}, command: command,
				started: make(chan context.Context, 1), release: make(chan struct{}),
			}
			env.service.git = NewGitInspector(runner)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if deleting {
					_, err := env.service.DeleteWorktree(ctx, deleteRequest)
					done <- err
					return
				}
				_, err := env.service.CreateWorktree(ctx, createWorktreeRequestForTest(
					worktreecontract.NewSetupOperationID(), env.session.Meta().SessionID, "", "HEAD", true, "feature/caller-cancel-create",
				))
				done <- err
			}()
			var operationCtx context.Context
			select {
			case operationCtx = <-runner.started:
			case err := <-done:
				t.Fatalf("mutation finished before Git gate: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("mutation did not reach Git")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("caller result: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("caller cancellation did not stop waiting")
			}
			if err := context.Cause(operationCtx); err != nil {
				close(runner.release)
				t.Fatalf("caller canceled server mutation: %v", err)
			}
			close(runner.release)
			select {
			case <-operationCtx.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("mutation did not complete after releasing Git")
			}
			if deleting {
				if _, err := env.store.GetWorktreeRecordByID(t.Context(), deleteRequest.Selector); !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("deleted Worktree remains: %v", err)
				}
			} else {
				if len(mustListWorktrees(t, env).Worktrees) != 2 {
					t.Fatal("canceled caller prevented Worktree creation")
				}
			}
		})
	}
}
