package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"core/server/metadata"
	"core/server/session"
	"core/server/sessionruntime"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/worktreecontract"

	"github.com/google/uuid"
)

func applyWorktreeTargetMutation[T any](write func() error, finish func() (T, error), rollback func() error) (T, error) {
	var zero T
	if err := write(); err != nil {
		return zero, worktreeUnappliedTechnical(err)
	}
	value, err := finish()
	if err == nil {
		return value, nil
	}
	failure := worktreeUnappliedTechnicalUnlessClassified(err)
	if isWorktreeApplied(failure) {
		return value, failure
	}
	return value, classifyWorktreeRollback(failure, rollback())
}

func classifyWorktreeRollback(failure, rollback error) error {
	if isWorktreeUnapplied(failure) && (rollback == nil || isWorktreeApplied(rollback)) {
		return worktreeUnappliedAfterRollback(failure, rollback)
	}
	return worktreeIndeterminate(errors.Join(failure, rollback))
}

func (s *Service) retargetDeleteSessions(ctx context.Context, workspace metadata.Binding, worktree metadata.WorktreeRecord) (uint64, error) {
	var retargeted uint64
	for {
		// Successful moves leave this result set. Re-read its first bounded page
		// instead of keeping historical IDs or an operation-wide undo list.
		page, err := s.metadata.ListSessionsTargetingWorktreePage(ctx, worktree.ID, nil)
		if err != nil {
			return retargeted, err
		}
		if len(page.Sessions) == 0 {
			return retargeted, nil
		}
		moved, err := s.retargetDeleteSessionPage(ctx, workspace, worktree, page.Sessions)
		retargeted += moved
		if err != nil {
			return retargeted, err
		}
	}
}

func (s *Service) retargetDeleteSessionPage(ctx context.Context, workspace metadata.Binding, worktree metadata.WorktreeRecord, sessions []metadata.WorktreeSessionBlocker) (uint64, error) {
	ids := make([]runtimeids.SessionID, 0, len(sessions))
	for _, target := range sessions {
		id, err := runtimeids.ParseSessionID(target.SessionID)
		if err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	block, err := s.acquireSessionStartAdmission(ctx, ids, sessionStartAdmissionTry)
	if err != nil {
		if errors.Is(err, sessionruntime.ErrSessionStartAdmissionBusy) {
			return 0, errors.Join(worktreecontract.ErrWorktreeBlocked, err)
		}
		return 0, err
	}
	defer releaseSessionStarts(block)
	ctx = authorizeSessionMaintenance(ctx, block)
	var retargeted uint64
	for _, target := range sessions {
		previous, err := s.metadata.ResolveSessionExecutionTarget(ctx, target.SessionID)
		if err != nil {
			return retargeted, err
		}
		if previous.Worktree == nil || previous.Worktree.Id != worktree.ID {
			continue
		}
		retired, err := s.authority.RetireIdleRuntime(ctx, target.SessionID)
		if err != nil {
			return retargeted, err
		}
		if !retired {
			return retargeted, &worktreecontract.BlockedError{Details: &worktreepb.BlockedDetails{
				ActiveSessions: &worktreepb.ActiveSessionBlockers{Sessions: []*worktreepb.BlockingSession{{
					SessionId: target.SessionID, Name: nonblankPointer(target.SessionName),
				}}},
			}}
		}
		cwd := clampCwdRelpath(previous.CwdRelpath, workspace.CanonicalRoot)
		reminder, err := worktreeReminderStateForExitedWorktree(worktree, workspace.CanonicalRoot, filepath.Join(workspace.CanonicalRoot, cwd))
		if err != nil {
			return retargeted, err
		}
		if err := s.metadata.UpdateSessionExecutionTarget(ctx, metadata.SessionExecutionTargetUpdate{
			SessionID:  target.SessionID,
			Workspace:  &metadata.SessionExecutionTargetUpdateWorkspace{ID: workspace.WorkspaceID},
			CwdRelpath: cwd, ExpectedWorktreeID: &worktree.ID, WorktreeReminder: &reminder,
		}); err != nil {
			return retargeted, err
		}
		retargeted++
		if err := s.publisher.PublishSessionIdentity(target.SessionID); err != nil {
			return retargeted, fmt.Errorf("publish retargeted session %q: %w", target.SessionID, err)
		}
	}
	return retargeted, nil
}

func (s *Service) switchSessionTarget(ctx context.Context, workspaceCtx sessionWorkspaceContext, previous *syncedWorktree, next syncedWorktree) (*worktreepb.SessionExecutionTarget, error) {
	target, err := s.switchSessionTargetWithSync(ctx, workspaceCtx, previous, next, nil, func(syncCtx context.Context, target *worktreepb.SessionExecutionTarget, reminder *session.WorktreeReminderState) error {
		return s.syncExecutionTarget(syncCtx, workspaceCtx.sessionID, target, reminder)
	})
	return target, err
}

func (s *Service) switchSessionTargetWithSync(
	ctx context.Context,
	workspaceCtx sessionWorkspaceContext,
	previous *syncedWorktree,
	next syncedWorktree,
	stepAuthority transitionAuthority,
	sync transitionTargetSync,
) (*worktreepb.SessionExecutionTarget, error) {
	if sync == nil {
		return &worktreepb.SessionExecutionTarget{}, worktreeUnappliedTechnical(errors.New("execution target synchronizer is required"))
	}
	if stepAuthority == nil {
		sessionIDs, err := parseSessionStartAdmissionIDs([]string{workspaceCtx.sessionID})
		if err != nil {
			return &worktreepb.SessionExecutionTarget{}, worktreeUnappliedTechnical(err)
		}
		release, err := s.acquireSessionStartAdmission(ctx, sessionIDs, sessionStartAdmissionWait)
		if err != nil {
			return &worktreepb.SessionExecutionTarget{}, worktreeUnappliedTechnical(err)
		}
		defer releaseSessionStarts(release)
		ctx = authorizeSessionMaintenance(ctx, release)
	}
	nextWorktreeID := strings.TrimSpace(next.record.ID)
	nextBaseRoot := strings.TrimSpace(next.record.CanonicalRoot)
	var nextWorktree *metadata.SessionExecutionTargetUpdateWorktree
	if strings.TrimSpace(next.record.ID) == "" {
		nextBaseRoot = workspaceCtx.workspaceRoot
	} else {
		nextWorktree = &metadata.SessionExecutionTargetUpdateWorktree{ID: nextWorktreeID}
	}
	previousTarget := workspaceCtx.target
	if err := validatePresentExecutionTargetWorktreeID(previousTarget); err != nil {
		return &worktreepb.SessionExecutionTarget{}, worktreeUnappliedTechnical(err)
	}
	cwdRelpath := clampCwdRelpath(previousTarget.CwdRelpath, nextBaseRoot)
	if err := s.metadata.UpdateSessionExecutionTarget(ctx, metadata.SessionExecutionTargetUpdate{
		SessionID:  workspaceCtx.sessionID,
		Workspace:  &metadata.SessionExecutionTargetUpdateWorkspace{ID: workspaceCtx.workspaceID},
		Worktree:   nextWorktree,
		CwdRelpath: cwdRelpath,
	}); err != nil {
		return &worktreepb.SessionExecutionTarget{}, worktreeUnappliedTechnical(err)
	}
	rollback := func(err error) error {
		failure := worktreeUnappliedTechnicalUnlessClassified(err)
		if isWorktreeApplied(failure) {
			return failure
		}
		return classifyWorktreeRollback(failure, s.rollbackSessionTargetWithSync(ctx, workspaceCtx, previousTarget, sync))
	}
	nextTarget, err := s.metadata.ResolveSessionExecutionTarget(ctx, workspaceCtx.sessionID)
	if err != nil {
		return &worktreepb.SessionExecutionTarget{}, rollback(err)
	}
	reminder, ok, err := worktreeReminderStateForTransition(previous, previousTarget, next, nextTarget)
	if err != nil {
		return &worktreepb.SessionExecutionTarget{}, rollback(err)
	}
	if ok {
		if err := sync(ctx, nextTarget, &reminder); err != nil {
			return &worktreepb.SessionExecutionTarget{}, rollback(err)
		}
		return nextTarget, nil
	}
	if err := sync(ctx, nextTarget, nil); err != nil {
		return &worktreepb.SessionExecutionTarget{}, rollback(err)
	}
	return nextTarget, nil
}

func (s *Service) rollbackSessionTargetWithSync(
	ctx context.Context,
	workspaceCtx sessionWorkspaceContext,
	previousTarget *worktreepb.SessionExecutionTarget,
	sync transitionTargetSync,
) error {
	rollbackCtx, cancel := liveRollbackContext(ctx)
	defer cancel()
	var collected []error
	indeterminate := false
	if err := s.metadata.UpdateSessionExecutionTarget(rollbackCtx, metadata.SessionExecutionTargetUpdateFromReadModel(workspaceCtx.sessionID, previousTarget)); err != nil {
		collected = append(collected, fmt.Errorf("rollback execution target: %w", err))
		indeterminate = true
	}
	if err := s.authority.ClearWorktreeReminder(rollbackCtx, workspaceCtx.sessionID); err != nil {
		collected = append(collected, fmt.Errorf("rollback worktree reminder: %w", err))
		indeterminate = true
	}
	if strings.TrimSpace(previousTarget.EffectiveWorkdir) != "" {
		if err := sync(rollbackCtx, previousTarget, nil); err != nil {
			collected = append(collected, fmt.Errorf("rollback runtime target: %w", err))
			if !isWorktreeApplied(err) {
				indeterminate = true
			}
		}
	}
	err := errors.Join(collected...)
	if indeterminate {
		return worktreeIndeterminate(err)
	}
	return worktreeApplied(err)
}

func (s *Service) syncExecutionTarget(ctx context.Context, sessionID string, target *worktreepb.SessionExecutionTarget, reminder *session.WorktreeReminderState) error {
	if err := s.authority.SyncExecutionTarget(ctx, sessionID, target, reminder); err != nil {
		return err
	}
	if err := s.publisher.PublishSessionIdentity(sessionID); err != nil {
		return fmt.Errorf("publish session identity: %w", err)
	}
	return nil
}

func liveRollbackContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), rollbackSessionTargetTimeout)
	}
	return context.WithTimeout(context.WithoutCancel(ctx), rollbackSessionTargetTimeout)
}

func worktreeReminderStateForTransition(previous *syncedWorktree, previousTarget *worktreepb.SessionExecutionTarget, next syncedWorktree, nextTarget *worktreepb.SessionExecutionTarget) (session.WorktreeReminderState, bool, error) {
	if err := validatePresentExecutionTargetWorktreeID(previousTarget); err != nil {
		return session.WorktreeReminderState{}, false, err
	}
	if strings.TrimSpace(next.record.ID) == "" {
		if previous == nil || previousTarget.Worktree == nil || strings.TrimSpace(previous.record.ID) == "" {
			return session.WorktreeReminderState{}, false, nil
		}
		return session.WorktreeReminderState{
			Mode: session.WorktreeReminderModeExit,
			WorktreeContext: session.WorktreeContext{
				Branch:        optionalWorktreeBranchName(previous.git.Branch),
				WorktreePath:  strings.TrimSpace(previous.record.CanonicalRoot),
				WorkspaceRoot: strings.TrimSpace(nextTarget.WorkspaceRoot),
				EffectiveCwd:  strings.TrimSpace(nextTarget.EffectiveWorkdir),
			},
		}, true, nil
	}
	return session.WorktreeReminderState{
		Mode: session.WorktreeReminderModeEnter,
		WorktreeContext: session.WorktreeContext{
			Branch:        optionalWorktreeBranchName(next.git.Branch),
			WorktreePath:  strings.TrimSpace(next.record.CanonicalRoot),
			WorkspaceRoot: strings.TrimSpace(nextTarget.WorkspaceRoot),
			EffectiveCwd:  strings.TrimSpace(nextTarget.EffectiveWorkdir),
		},
	}, true, nil
}

func worktreeReminderStateForExitedWorktree(worktree metadata.WorktreeRecord, workspaceRoot, effectiveCwd string) (session.WorktreeReminderState, error) {
	worktreeContext, err := ContextFromRecord(worktree, workspaceRoot, effectiveCwd)
	if err != nil {
		return session.WorktreeReminderState{}, err
	}
	contextID := uuid.New()
	worktreeContext.ContextID = &contextID
	return session.WorktreeReminderState{
		Mode:            session.WorktreeReminderModeExit,
		WorktreeContext: worktreeContext,
	}, nil
}

// ContextFromRecord projects the recorded Worktree identity and Git branch
// into the context used by Session reminders.
func ContextFromRecord(worktree metadata.WorktreeRecord, workspaceRoot, effectiveCwd string) (session.WorktreeContext, error) {
	gitMetadata, err := worktreeGitMetadataFromRecord(worktree)
	if err != nil {
		return session.WorktreeContext{}, err
	}
	return session.WorktreeContext{
		Branch:        optionalWorktreeBranchName(gitMetadata.Branch),
		WorktreePath:  strings.TrimSpace(worktree.CanonicalRoot),
		WorkspaceRoot: workspaceRoot,
		EffectiveCwd:  effectiveCwd,
	}, nil
}

func optionalWorktreeBranchName(branch *localBranch) *string {
	if branch == nil {
		return nil
	}
	name := branch.Name()
	return &name
}

func worktreeGitMetadataFromRecord(worktree metadata.WorktreeRecord) (GitWorktree, error) {
	metadataJSON := strings.TrimSpace(worktree.GitMetadataJSON)
	if metadataJSON == "" {
		return GitWorktree{}, nil
	}
	var persisted persistedGitWorktree
	if err := json.Unmarshal([]byte(metadataJSON), &persisted); err != nil {
		return GitWorktree{}, fmt.Errorf("decode git worktree metadata: %w", err)
	}
	branch, err := optionalLocalBranch(persisted.BranchRef, persisted.BranchName)
	if err != nil {
		return GitWorktree{}, fmt.Errorf("decode git worktree metadata: %w", err)
	}
	decoded := GitWorktree{
		Root:           worktree.CanonicalRoot,
		HeadOID:        persisted.HeadOID,
		RecordedBranch: branch,
		Detached:       persisted.Detached,
		Bare:           persisted.Bare,
		LockedReason:   persisted.LockedReason,
		PrunableReason: persisted.PrunableReason,
	}
	if !persisted.Detached {
		decoded.Branch = branch
	}
	if err := decoded.validateHead(); err != nil {
		return GitWorktree{}, fmt.Errorf("decode git worktree metadata: %w", err)
	}
	return decoded, nil
}
