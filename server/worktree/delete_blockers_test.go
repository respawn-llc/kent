package worktree

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/session"
	"core/server/sessionruntime"
	"core/shared/worktreecontract"
)

func holdWorktreeSessionExecution(t *testing.T, env *serviceTestEnv, target serviceTestWorktree, sessionID string) {
	t.Helper()
	barrier := testsetup.NewStartBarrier()
	t.Cleanup(barrier.Unblock)
	plan := deleteActivityTestRuntimePlan(t, env, target.CanonicalRoot)
	handle, err := env.authority.StartAgentExecution(context.Background(), sessionruntime.AgentExecutionRequest{
		Descriptor: openDeleteActivitySessionDescriptor(t, sessionID),
		Runtime:    &plan,
		Resource:   sessionruntime.OpenAgentResource{},
		Runner: func(ctx context.Context, _ sessionruntime.ExecutionScope, _ sessionruntime.AgentRuntimeBridge) error {
			return barrier.ArriveAndWait(ctx)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		barrier.Unblock()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := handle.Wait(ctx); err != nil {
			t.Errorf("finish held execution: %v", err)
		}
	})
	select {
	case <-barrier.Entered():
	case <-time.After(3 * time.Second):
		t.Fatal("execution did not start")
	}
}

func TestDeleteWorktreeFindsActiveSessionAfterIdlePage(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/paged-blockers")
	sessions := make([]*session.Store, 0, 51)
	for range 51 {
		sess := createServiceTestSession(t, env.store, env.cfg, env.binding)
		updateServiceTestSessionTarget(t, env, sess.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
		sessions = append(sessions, sess)
	}
	firstPage, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]struct{}, len(firstPage.Sessions))
	for _, entry := range firstPage.Sessions {
		seen[entry.SessionID] = struct{}{}
	}
	var active *session.Store
	for _, sess := range sessions {
		if _, present := seen[sess.Meta().SessionID]; !present {
			active = sess
		}
	}
	if active == nil || firstPage.Next == nil {
		t.Fatal("fixture requires a Session beyond the idle first page")
	}
	id := active.Meta().SessionID
	holdWorktreeSessionExecution(t, env, target, id)
	if err := active.SetListingMetadata("renamed active session", ""); err != nil {
		t.Fatal(err)
	}
	next, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, firstPage.Next)
	if err != nil || len(next.Sessions) != 1 || next.Sessions[0].SessionID != id {
		t.Fatalf("renaming a later Session skipped it during discovery: %+v, %v", next, err)
	}
	state := captureDeleteTargetState(t, env, id, target)
	_, err = env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, target.WorktreeID))
	var blocked *worktreecontract.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("delete error = %v, want structured blockers", err)
	}
	details := blocked.Details.GetActiveSessions()
	if details == nil || len(details.Sessions) != 1 || details.HasMore {
		t.Fatalf("unexpected blockers: %v", details)
	}
	if details.Sessions[0].SessionId != id || details.Sessions[0].GetName() != active.Meta().Name {
		t.Fatalf("blocking Session identity lost: %v", details.Sessions[0])
	}
	state.assertUnchanged(t, env, id, target.WorktreeID)
}

func TestDeleteWorktreeBoundsActiveSessionDetails(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/many-blockers")
	identities := make(map[string]string)
	for range 51 {
		sess := createServiceTestSession(t, env.store, env.cfg, env.binding)
		id := sess.Meta().SessionID
		if err := sess.SetListingMetadata("session "+id, ""); err != nil {
			t.Fatal(err)
		}
		updateServiceTestSessionTarget(t, env, id, env.binding.WorkspaceID, target.WorktreeID, ".")
		holdWorktreeSessionExecution(t, env, target, id)
		identities[id] = sess.Meta().Name
	}
	_, err := env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, target.WorktreeID))
	var blocked *worktreecontract.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("delete error = %v, want structured blockers", err)
	}
	details := blocked.Details.GetActiveSessions()
	if details == nil || len(details.Sessions) != 50 || !details.HasMore {
		t.Fatalf("expected 50 blockers and more: %v", details)
	}
	for _, entry := range details.Sessions {
		name, exists := identities[entry.SessionId]
		if !exists || entry.GetName() != name {
			t.Fatalf("unexpected or repeated blocker identity: %v", entry)
		}
		delete(identities, entry.SessionId)
	}
	if _, err := env.store.GetWorktreeRecordByID(env.ctx, target.WorktreeID); err != nil {
		t.Fatalf("blocked deletion lost the Worktree: %v", err)
	}
}
