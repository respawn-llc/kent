package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/metadata"
	"core/server/session"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/worktreecontract"
)

type deleteProgressPublisher struct {
	runtimePublisher
	afterMove func()
}

func (p *deleteProgressPublisher) PublishSessionIdentity(id string) error {
	if p.afterMove != nil {
		action := p.afterMove
		p.afterMove = nil
		action()
	}
	return p.runtimePublisher.PublishSessionIdentity(id)
}

func TestDeleteWorktreeRejectsLaterSessionStartWithoutUndoingCompletedMoves(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-later-start")
	for range 51 {
		sess := createServiceTestSession(t, env.store, env.cfg, env.binding)
		updateServiceTestSessionTarget(t, env, sess.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
	}
	first, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Next == nil {
		t.Fatal("fixture requires more than one page")
	}
	later, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, first.Next)
	if err != nil || len(later.Sessions) == 0 {
		t.Fatalf("later Session page: %+v, %v", later, err)
	}
	startedID := later.Sessions[0].SessionID
	env.service.publisher = &deleteProgressPublisher{
		runtimePublisher: env.publisher,
		afterMove: func() {
			holdWorktreeSessionExecution(t, env, target, startedID)
		},
	}
	_, err = env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, target.WorktreeID))
	var progress *worktreecontract.DeletePartialError
	if !errors.As(err, &progress) || progress.RetargetedSessions == 0 || progress.RetargetedSessions >= 51 {
		t.Fatalf("expected partial progress: %v", err)
	}
	if !errors.Is(err, worktreecontract.ErrWorktreeBlocked) {
		t.Fatalf("newly started Session did not block deletion: %v", err)
	}
	activeTarget, err := env.store.ResolveSessionExecutionTarget(env.ctx, startedID)
	if err != nil || activeTarget.GetWorktree().GetId() != target.WorktreeID {
		t.Fatalf("active Session target changed: %v, %v", activeTarget, err)
	}
	remaining, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, nil)
	if err != nil || remaining.Next != nil || uint64(len(remaining.Sessions))+progress.RetargetedSessions != 51 {
		t.Fatalf("partial progress count does not match retained targets: %+v, %v", remaining, err)
	}
	if _, err := os.Stat(target.CanonicalRoot); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteWorktreeKeepsAtomicSessionMoveWhenPublicationFails(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-publication-failure")
	sess := createServiceTestSession(t, env.store, env.cfg, env.binding)
	id := sess.Meta().SessionID
	updateServiceTestSessionTarget(t, env, id, env.binding.WorkspaceID, target.WorktreeID, ".")
	publicationFailure := errors.New("publication failed")
	env.publisher.identityErr = publicationFailure
	_, err := env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, target.WorktreeID))
	var progress *worktreecontract.DeletePartialError
	if !errors.As(err, &progress) || progress.RetargetedSessions != 1 || !errors.Is(err, publicationFailure) {
		t.Fatalf("expected durable move and publication failure: %v", err)
	}
	moved, err := env.store.ResolveSessionExecutionTarget(env.ctx, id)
	if err != nil || moved.Worktree != nil {
		t.Fatalf("move not persisted: %+v, %v", moved, err)
	}
	var reminder *session.WorktreeReminderState
	if err := env.authority.WithSessionStore(env.ctx, openDeleteActivitySessionDescriptor(t, id), func(_ context.Context, store *session.Store) error {
		reminder = store.Meta().WorktreeReminder
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if reminder == nil || reminder.ContextID == nil || reminder.EffectiveCwd != moved.EffectiveWorkdir {
		t.Fatalf("move and reminder disagree: %+v, %+v", moved, reminder)
	}
	env.publisher.identityErr = nil
	if _, err := env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, target.WorktreeID)); err != nil {
		t.Fatal(err)
	}
	if err := env.authority.WithSessionStore(env.ctx, openDeleteActivitySessionDescriptor(t, id), func(_ context.Context, store *session.Store) error {
		current := store.Meta().WorktreeReminder
		if current == nil || !session.WorktreeReminderStateEqual(*current, *reminder) {
			t.Fatal("retry rewrote the already committed reminder")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStaleSessionMoveDoesNotWriteTargetOrReminder(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-stale-move")
	sess := createServiceTestSession(t, env.store, env.cfg, env.binding)
	id := sess.Meta().SessionID
	updateServiceTestSessionTarget(t, env, id, env.binding.WorkspaceID, target.WorktreeID, ".")
	other := mustCreateWorktree(t, env, "feature/other-target")
	record, err := env.store.GetWorktreeRecordByID(env.ctx, target.WorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	reminder, err := worktreeReminderStateForExitedWorktree(record, env.workspaceRoot, env.workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	err = env.store.UpdateSessionExecutionTarget(env.ctx, metadata.SessionExecutionTargetUpdate{
		SessionID: id, Workspace: &metadata.SessionExecutionTargetUpdateWorkspace{ID: env.binding.WorkspaceID},
		CwdRelpath: ".", ExpectedWorktreeID: &other.WorktreeID, WorktreeReminder: &reminder,
	})
	if err == nil {
		t.Fatal("stale target precondition accepted")
	}
	current, err := env.store.ResolveSessionExecutionTarget(env.ctx, id)
	if err != nil || current.GetWorktree().GetId() != target.WorktreeID {
		t.Fatalf("stale move changed target: %+v, %v", current, err)
	}
	if err := env.authority.WithSessionStore(env.ctx, openDeleteActivitySessionDescriptor(t, id), func(_ context.Context, store *session.Store) error {
		if store.Meta().WorktreeReminder != nil {
			t.Fatal("stale move persisted a reminder")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFailedWorktreeRemovalKeepsCompletedSessionMoves(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-partial-progress")
	sess := createServiceTestSession(t, env.store, env.cfg, env.binding)
	updateServiceTestSessionTarget(t, env, sess.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
	barrier := testsetup.NewStartBarrier()
	t.Cleanup(barrier.Unblock)
	env.service.git = NewGitInspector(&deleteRemovalBarrierRunner{
		delegate: env.service.git.runner, barrier: barrier,
	})
	deleted := testsetup.Start(func() (*worktreepb.DeleteSuccess, error) {
		return env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, target.WorktreeID))
	})
	select {
	case <-barrier.Entered():
	case result := <-deleted:
		t.Fatalf("delete ended before physical removal: %v", result.Err)
	case <-time.After(5 * time.Second):
		t.Fatal("delete did not reach physical removal")
	}
	if err := os.WriteFile(filepath.Join(target.CanonicalRoot, "keep.txt"), []byte("uncommitted content"), 0o600); err != nil {
		t.Fatal(err)
	}
	barrier.Unblock()
	result := <-deleted
	if result.Err == nil {
		t.Fatal("expected Git to reject removal of changed worktree")
	}
	var progress *worktreecontract.DeletePartialError
	if !errors.As(result.Err, &progress) || progress.RetargetedSessions != 1 {
		t.Fatalf("expected one completed Session move in deletion failure: %v", result.Err)
	}
	next, err := env.store.ResolveSessionExecutionTarget(env.ctx, sess.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Worktree != nil || next.EffectiveWorkdir != env.workspaceRoot {
		t.Fatalf("completed Session move was reverted: %+v", next)
	}
	if err := env.authority.WithSessionStore(env.ctx, openDeleteActivitySessionDescriptor(t, sess.Meta().SessionID), func(_ context.Context, store *session.Store) error {
		reminder := store.Meta().WorktreeReminder
		if reminder == nil || reminder.Mode != session.WorktreeReminderModeExit || reminder.ContextID == nil {
			t.Fatalf("completed Session move lost its reminder: %+v", reminder)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target.CanonicalRoot, "keep.txt")); err != nil {
		t.Fatal(err)
	}
}
