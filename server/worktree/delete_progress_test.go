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
	"core/server/runtimecontrol"
	"core/server/session"
	"core/server/sessionruntime"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"
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

func TestDeleteWorktreeRetainsAcceptedWorkAfterEarlierSessionMoveAndRetries(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-accepted-work-progress")
	for range 51 {
		sess := createServiceTestSession(t, env.store, env.cfg, env.binding)
		updateServiceTestSessionTarget(t, env, sess.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
	}
	firstPage, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, nil)
	if err != nil {
		t.Fatalf("ListSessionsTargetingWorktreePage: %v", err)
	}
	if len(firstPage.Sessions) != 50 || firstPage.Next == nil {
		t.Fatalf("first targeting page = %+v, want 50 Sessions and another page", firstPage)
	}
	laterPage, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, firstPage.Next)
	if err != nil {
		t.Fatalf("ListSessionsTargetingWorktreePage later page: %v", err)
	}
	if len(laterPage.Sessions) != 1 || laterPage.Next != nil {
		t.Fatalf("later targeting page = %+v, want one Session", laterPage)
	}
	firstMovedID := firstPage.Sessions[0].SessionID
	blockedSessionID := laterPage.Sessions[0].SessionID
	blockedState := captureDeleteTargetState(t, env, blockedSessionID, target)
	client := newDeleteActivityGatedLLMClient()
	attachment, engine := openDeleteActivityRuntime(t, env, target, blockedSessionID, "delete-accepted-work-progress", client)
	t.Cleanup(func() {
		client.unblock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := engine.WaitForScheduledQueuedUserWork(ctx); err != nil {
			t.Errorf("finish accepted user work: %v", err)
		}
		if _, err := attachment.Release(ctx, sessionruntime.RuntimeReleaseClose); err != nil &&
			!errors.Is(err, serverapi.ErrRuntimeUnavailable) {
			t.Errorf("release accepted-work Runtime: %v", err)
		}
	})

	var acceptedTurn *runtimepb.SubmitUserTurnSuccess
	env.service.publisher = &deleteProgressPublisher{
		runtimePublisher: env.publisher,
		afterMove: func() {
			movedTarget, resolveErr := env.store.ResolveSessionExecutionTarget(env.ctx, firstMovedID)
			if resolveErr != nil {
				t.Fatalf("ResolveSessionExecutionTarget first moved Session: %v", resolveErr)
			}
			if movedTarget.Worktree != nil || movedTarget.EffectiveWorkdir != env.workspaceRoot {
				t.Fatalf("first Session was not moved before accepting later work: %+v", movedTarget)
			}
			var submitErr error
			acceptedTurn, submitErr = runtimecontrol.NewService(env.authority).SubmitUserTurn(env.ctx, &runtimepb.SubmitUserTurnRequest{
				SessionId: blockedSessionID,
				Input:     &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: "accepted after the first Session moved"}},
			})
			if submitErr != nil {
				t.Fatalf("accept user turn on later Session: %v", submitErr)
			}
			if acceptedTurn.GetQueued() == nil || !acceptedTurn.GetQueued().Steered {
				t.Fatalf("accepted user turn result = %+v, want accepted queued input", acceptedTurn)
			}
			select {
			case <-client.started:
			case <-time.After(3 * time.Second):
				t.Fatal("accepted user turn did not start provider work")
			}
		},
	}

	_, deleteErr := env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, target.WorktreeID))
	var partial *worktreecontract.DeletePartialError
	if !errors.As(deleteErr, &partial) || partial.RetargetedSessions != 50 {
		t.Fatalf("partial deletion = %v, want 50 completed Session moves", deleteErr)
	}
	if !errors.Is(deleteErr, worktreecontract.ErrWorktreeBlocked) {
		t.Fatalf("partial deletion stop reason = %v, want accepted work blocker", deleteErr)
	}
	var blocked *worktreecontract.BlockedError
	if !errors.As(deleteErr, &blocked) {
		t.Fatalf("partial deletion cause = %v, want structured blocker details", deleteErr)
	}
	blockers := blocked.Details.GetActiveSessions()
	if blockers == nil || len(blockers.Sessions) != 1 || blockers.Sessions[0].SessionId != blockedSessionID {
		t.Fatalf("partial deletion blockers = %v, want Session %q", blockers, blockedSessionID)
	}
	if acceptedTurn == nil {
		t.Fatal("user turn was not accepted after the first Session moved")
	}
	if !engine.HasActiveLiveRunGroup() {
		t.Fatal("accepted user work stopped before deletion reported its blocker")
	}
	blockedState.assertUnchanged(t, env, blockedSessionID, target.WorktreeID)
	firstMovedTarget, err := env.store.ResolveSessionExecutionTarget(env.ctx, firstMovedID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget first moved Session after partial delete: %v", err)
	}
	if firstMovedTarget.Worktree != nil || firstMovedTarget.EffectiveWorkdir != env.workspaceRoot {
		t.Fatalf("completed Session move was reverted: %+v", firstMovedTarget)
	}
	firstMovedReminder := readDeleteActivityReminder(t, env, firstMovedID)
	if firstMovedReminder == nil || firstMovedReminder.Mode != session.WorktreeReminderModeExit ||
		firstMovedReminder.WorktreePath != target.CanonicalRoot {
		t.Fatalf("completed Session move reminder = %+v", firstMovedReminder)
	}
	remaining, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, nil)
	if err != nil || len(remaining.Sessions) != 1 || remaining.Next != nil ||
		remaining.Sessions[0].SessionID != blockedSessionID {
		t.Fatalf("targeting Sessions after partial delete = %+v, %v; want only blocked Session", remaining, err)
	}
	if _, err := os.Stat(target.CanonicalRoot); err != nil {
		t.Fatalf("Worktree root changed after partial delete: %v", err)
	}

	client.unblock()
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.WaitForScheduledQueuedUserWork(waitCtx); err != nil {
		t.Fatalf("finish accepted user turn before retry: %v", err)
	}
	if _, err := env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, target.WorktreeID)); err != nil {
		t.Fatalf("retry Worktree deletion: %v", err)
	}
	firstMovedTarget, err = env.store.ResolveSessionExecutionTarget(env.ctx, firstMovedID)
	if err != nil || firstMovedTarget.Worktree != nil || firstMovedTarget.EffectiveWorkdir != env.workspaceRoot {
		t.Fatalf("first moved Session target after retry = %+v, %v", firstMovedTarget, err)
	}
	reminderAfterRetry := readDeleteActivityReminder(t, env, firstMovedID)
	if reminderAfterRetry == nil || !session.WorktreeReminderStateEqual(*firstMovedReminder, *reminderAfterRetry) {
		t.Fatalf("retry changed the earlier exit reminder: before=%+v after=%+v", firstMovedReminder, reminderAfterRetry)
	}
	finalRemaining, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, nil)
	if err != nil || len(finalRemaining.Sessions) != 0 {
		t.Fatalf("targeting Sessions after retry = %+v, %v; want none", finalRemaining, err)
	}
	if _, err := os.Stat(target.CanonicalRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Worktree root remains after retry: %v", err)
	}
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
