package worktree

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"core/server/sessionruntime"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/worktreecontract"
)

func assertForeignManagementDeleteBlocked(t *testing.T, env *serviceTestEnv, worktreeID string) {
	t.Helper()
	root := t.TempDir()
	binding, err := env.store.RegisterWorkspaceBinding(env.ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := env.cfg
	cfg.WorkspaceRoot = root
	caller := createServiceTestSession(t, env.store, cfg, binding)
	before, err := env.store.ResolveSessionExecutionTarget(env.ctx, caller.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := worktreeDeleteRequest(env, worktreeID)
	id := caller.Meta().SessionID
	request.Scope = worktreecontract.WorkspaceManagementScope(env.binding.ProjectID, env.binding.WorkspaceID, &id)
	_, err = env.service.DeleteWorktree(context.Background(), request)
	if !errors.Is(err, worktreecontract.ErrWorktreeBlocked) {
		t.Fatalf("foreign delete error = %v, want blocked", err)
	}
	after, err := env.store.ResolveSessionExecutionTarget(env.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("blocked management moved foreign caller: before=%+v after=%+v", before, after)
	}
}

func TestWorkspaceManagementKeepsForeignCallerExecutionRunning(t *testing.T) {
	env := newServiceTestEnv(t)
	root := t.TempDir()
	initGitRepo(t, root)
	binding, err := env.store.RegisterWorkspaceBinding(env.ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	id := env.session.Meta().SessionID
	before, err := env.store.ResolveSessionExecutionTarget(env.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	started, released, interrupted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	plan := deleteActivityTestRuntimePlan(t, env, env.workspaceRoot)
	handle, err := env.authority.StartAgentExecution(env.ctx, sessionruntime.AgentExecutionRequest{
		Descriptor: openDeleteActivitySessionDescriptor(t, id),
		Runtime:    &plan,
		Resource:   sessionruntime.OpenAgentResource{},
		Runner: func(ctx context.Context, _ sessionruntime.ExecutionScope, _ sessionruntime.AgentRuntimeBridge) error {
			close(started)
			select {
			case <-released:
				return nil
			case <-ctx.Done():
				close(interrupted)
				return context.Cause(ctx)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	scope := worktreecontract.WorkspaceManagementScope(binding.ProjectID, binding.WorkspaceID, &id)
	request := createWorktreeRequestForTest(worktreecontract.NewSetupOperationID(), id, "", "HEAD", true, "foreign-live")
	request.Scope = scope
	created, err := env.service.CreateWorktree(env.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if created.Target.WorkspaceId != before.WorkspaceID {
		t.Fatalf("created target lost caller location: %v", created.Target)
	}
	_, err = env.service.DeleteWorktree(env.ctx, &worktreepb.DeleteRequest{
		Scope: scope, Selector: created.Worktree.Topology.GetRegistered().Kent.WorktreeId,
		BranchCleanupPolicy: worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_RETAIN,
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := env.store.ResolveSessionExecutionTarget(env.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("management moved live caller: %+v", after)
	}
	if _, live := env.authority.ExecutionByScope(handle.Scope().ID()); !live {
		t.Fatal("management ended caller execution")
	}
	select {
	case <-interrupted:
		t.Fatal("management interrupted caller")
	default:
	}
	env.publisher.mu.Lock()
	outcomeCount := len(env.publisher.outcomes)
	env.publisher.mu.Unlock()
	if outcomeCount != 0 {
		t.Fatal("management emitted a navigation outcome")
	}
	release()
	ctx, cancel := context.WithTimeout(env.ctx, 5*time.Second)
	defer cancel()
	if _, err := handle.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}
