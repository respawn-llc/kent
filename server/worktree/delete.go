package worktree

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"core/server/metadata"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/worktreecontract"
)

func (s *Service) DeleteWorktree(ctx context.Context, req *worktreepb.DeleteRequest) (*worktreepb.DeleteSuccess, error) {
	release, management, err := s.beginManagementMutation(ctx, req.Scope)
	if err != nil {
		return nil, err
	}
	workspaceCtx := management.binding
	resolution, err := s.resolveWorktreeSelector(ctx, workspaceCtx, req.Selector)
	if err != nil {
		release()
		return nil, err
	}
	if _, err := deletionSelector(resolution.match.entry); err != nil {
		release()
		return nil, err
	}
	defer release()
	result, err := s.executeDeleteLocked(ctx, workspaceCtx, resolution.match.entry, req)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) executeDeleteLocked(
	ctx context.Context,
	workspaceCtx metadata.Binding,
	entry *worktreepb.TopologyEntry,
	req *worktreepb.DeleteRequest,
) (result *worktreepb.DeleteSuccess, resultErr error) {
	var retargeted uint64
	defer func() { resultErr = deletionProgressError(retargeted, resultErr) }()
	target, record, err := s.deleteTarget(ctx, workspaceCtx, entry)
	if err != nil {
		return nil, err
	}
	retainRecord, err := s.retainManagedTaskWorktreeRecord(ctx, record)
	if err != nil {
		return nil, err
	}
	var targetRoot *string
	if target != nil {
		targetRoot = &target.record.CanonicalRoot
	} else if record != nil {
		targetRoot = &record.CanonicalRoot
	}
	if err := s.checkDeleteTargetActivity(ctx, record, targetRoot); err != nil {
		return nil, err
	}
	if err := s.ensureDeleteFolderRemovalAuthorized(ctx, entry, req.ForceFolderRemoval); err != nil {
		return nil, err
	}
	if record != nil {
		retargeted, err = s.retargetDeleteSessions(ctx, workspaceCtx, *record)
		if err != nil {
			return nil, err
		}
	}
	leftoverRoot := missingLeftoverRoot(entry)
	if err := s.checkDeleteBackgroundProcesses(targetRoot); err != nil {
		return nil, err
	}
	if target != nil {
		var err error
		if req.ForceFolderRemoval && entry.GetRegistered() != nil &&
			entry.GetRegistered().GetGit().PrunableReason != nil {
			err = s.git.ForceRemovePrunableWorktree(ctx, workspaceCtx.CanonicalRoot, target.record.CanonicalRoot)
		} else {
			err = s.git.Remove(ctx, workspaceCtx.CanonicalRoot, target.record.CanonicalRoot, req.ForceFolderRemoval)
		}
		if err != nil {
			return nil, err
		}
	}
	if record != nil && !retainRecord {
		if err := s.metadata.DeleteWorktreeRecordByID(ctx, record.ID); err != nil {
			return nil, err
		}
	}
	cleanup := s.cleanupDeletedBranch(ctx, workspaceCtx.CanonicalRoot, entry, record, req.BranchCleanupPolicy)
	return &worktreepb.DeleteSuccess{Cleanup: cleanup, LeftoverRoot: leftoverRoot}, nil
}

func deletionProgressError(retargeted uint64, err error) error {
	if err == nil || retargeted == 0 {
		return err
	}
	return &worktreecontract.DeletePartialError{RetargetedSessions: retargeted, Cause: err}
}

func (s *Service) ensureDeleteFolderRemovalAuthorized(
	ctx context.Context,
	entry *worktreepb.TopologyEntry,
	forceFolderRemoval bool,
) error {
	dirtyState, err := s.evaluateDeleteCleanliness(ctx, entry)
	if err != nil {
		return err
	}
	if dirtyState.Kind != worktreepb.DirtyStateKind_DIRTY_STATE_CLEAN && !forceFolderRemoval {
		return worktreecontract.NewDeletePreconditionError(dirtyState)
	}
	return nil
}

func (s *Service) deleteTarget(
	ctx context.Context,
	workspaceCtx metadata.Binding,
	entry *worktreepb.TopologyEntry,
) (*syncedWorktree, *metadata.WorktreeRecord, error) {
	switch {
	case entry.GetRegistered() != nil:
		record, err := s.metadata.GetWorktreeRecordByID(ctx, entry.GetRegistered().GetKent().GetWorktreeId())
		if err != nil {
			return nil, nil, err
		}
		gitEntry, err := gitWorktreeFromFacts(entry.GetRegistered().GetGit())
		if err != nil {
			return nil, nil, err
		}
		target := syncedWorktree{record: record, git: gitEntry}
		return &target, &record, nil
	case entry.GetExternal() != nil:
		gitEntry, err := gitWorktreeFromFacts(entry.GetExternal().GetGit())
		if err != nil {
			return nil, nil, err
		}
		target := syncedWorktree{
			record: metadata.WorktreeRecord{
				WorkspaceID:   workspaceCtx.WorkspaceID,
				CanonicalRoot: entry.GetExternal().GetGit().GetCanonicalRoot(),
				DisplayName:   filepath.Base(entry.GetExternal().GetGit().GetCanonicalRoot()),
			},
			git: gitEntry,
		}
		return &target, nil, nil
	case entry.GetMissing() != nil:
		record, err := s.metadata.GetWorktreeRecordByID(ctx, entry.GetMissing().GetKent().GetWorktreeId())
		if err != nil {
			return nil, nil, err
		}
		return nil, &record, nil
	default:
		return nil, nil, errors.New("worktree topology variant is invalid")
	}
}

func (s *Service) retainManagedTaskWorktreeRecord(ctx context.Context, record *metadata.WorktreeRecord) (bool, error) {
	if record == nil {
		return false, nil
	}
	taskManagers, err := s.metadata.Queries().CountNonTerminalTasksByManagedWorktree(ctx, sql.NullString{
		String: strings.TrimSpace(record.ID),
		Valid:  strings.TrimSpace(record.ID) != "",
	})
	if err != nil {
		return false, err
	}
	return taskManagers > 0, nil
}

func (s *Service) checkDeleteTargetActivity(
	ctx context.Context,
	record *metadata.WorktreeRecord,
	worktreeRoot *string,
) error {
	if worktreeRoot != nil && strings.TrimSpace(*worktreeRoot) == "" {
		return errors.New("delete target root must not be blank when present")
	}
	if err := s.checkDeleteBackgroundProcesses(worktreeRoot); err != nil {
		return err
	}
	if record != nil {
		const blockerLimit = 50
		activeBlockers := &worktreepb.ActiveSessionBlockers{}
		var cursor *metadata.WorktreeSessionCursor
		for {
			page, err := s.metadata.ListSessionsTargetingWorktreePage(ctx, record.ID, cursor)
			if err != nil {
				return err
			}
			for _, target := range page.Sessions {
				active, err := s.authority.HasBlockingRuntimeActivity(ctx, target.SessionID)
				if err != nil {
					return err
				}
				if !active {
					continue
				}
				if len(activeBlockers.Sessions) == blockerLimit {
					activeBlockers.HasMore = true
					return &worktreecontract.BlockedError{Details: &worktreepb.BlockedDetails{ActiveSessions: activeBlockers}}
				}
				activeBlockers.Sessions = append(activeBlockers.Sessions, &worktreepb.BlockingSession{
					SessionId: target.SessionID, Name: nonblankPointer(target.SessionName),
				})
			}
			if page.Next == nil {
				break
			}
			cursor = page.Next
		}
		if len(activeBlockers.Sessions) > 0 {
			return &worktreecontract.BlockedError{Details: &worktreepb.BlockedDetails{
				ActiveSessions: activeBlockers,
			}}
		}
	}
	return nil
}

func (s *Service) checkDeleteBackgroundProcesses(worktreeRoot *string) error {
	if worktreeRoot != nil {
		if processBlockers := s.backgroundProcessBlockers(*worktreeRoot); len(processBlockers) > 0 {
			return errors.Join(worktreecontract.ErrWorktreeBlocked, fmt.Errorf("worktree has active background processes: %s", strings.Join(processBlockers, ", ")))
		}
	}
	return nil
}

func missingLeftoverRoot(entry *worktreepb.TopologyEntry) *string {
	if entry.GetMissing() == nil {
		return nil
	}
	root := strings.TrimSpace(entry.GetMissing().GetKent().GetCanonicalRoot())
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	return &root
}

func (s *Service) cleanupDeletedBranch(
	ctx context.Context,
	workspaceRoot string,
	entry *worktreepb.TopologyEntry,
	record *metadata.WorktreeRecord,
	policy worktreepb.BranchCleanupMode,
) *worktreepb.BranchCleanupOutcome {
	gitEntry, live, err := branchCleanupGitEntry(entry, record)
	if err != nil {
		return &worktreepb.BranchCleanupOutcome{
			Kind: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_APPLICABLE,
		}
	}
	branchName, named := worktreeNamedBranch(gitEntry)
	if !named {
		return &worktreepb.BranchCleanupOutcome{
			Kind: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_APPLICABLE,
		}
	}
	switch policy {
	case worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_RETAIN:
		return &worktreepb.BranchCleanupOutcome{
			Kind: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_REQUESTED,
		}
	case worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_AUTO_IF_KENT_CREATED:
		if record == nil {
			diagnostic := "Kent cannot prove this worktree created the branch"
			return &worktreepb.BranchCleanupOutcome{
				Kind:       worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED,
				BranchName: &branchName,
				Diagnostic: &diagnostic,
			}
		}
		createdBranch, proven, err := kentCreatedBranchForCleanup(*record, live)
		if err != nil {
			diagnostic := err.Error()
			return &worktreepb.BranchCleanupOutcome{
				Kind:       worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED,
				BranchName: &branchName,
				Diagnostic: &diagnostic,
			}
		}
		if !proven {
			diagnostic := "Kent cannot prove this worktree created the branch"
			return &worktreepb.BranchCleanupOutcome{
				Kind:       worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED,
				BranchName: &branchName,
				Diagnostic: &diagnostic,
			}
		}
		branchName = createdBranch
	case worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_DELETE_SAFE:
	case worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_DELETE_FORCE:
	default:
		panic(fmt.Sprintf("invalid branch cleanup policy %q", policy))
	}
	force := policy == worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_DELETE_FORCE
	if err := s.git.deleteBranch(ctx, workspaceRoot, branchName, force); err != nil {
		diagnostic := err.Error()
		return &worktreepb.BranchCleanupOutcome{
			Kind:       worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED,
			BranchName: &branchName,
			Diagnostic: &diagnostic,
		}
	}
	return &worktreepb.BranchCleanupOutcome{
		Kind:       worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_DELETED,
		BranchName: &branchName,
	}
}

func branchCleanupGitEntry(
	entry *worktreepb.TopologyEntry,
	record *metadata.WorktreeRecord,
) (GitWorktree, *GitWorktree, error) {
	switch {
	case entry.GetRegistered() != nil:
		live, err := gitWorktreeFromFacts(entry.GetRegistered().GetGit())
		if err != nil {
			return GitWorktree{}, nil, err
		}
		return live, &live, nil
	case entry.GetExternal() != nil:
		live, err := gitWorktreeFromFacts(entry.GetExternal().GetGit())
		if err != nil {
			return GitWorktree{}, nil, err
		}
		return live, &live, nil
	case entry.GetMissing() != nil:
		if record == nil {
			return GitWorktree{}, nil, nil
		}
		persisted, err := worktreeGitMetadataFromRecord(*record)
		return persisted, nil, err
	default:
		return GitWorktree{}, nil, errors.New("worktree topology variant is invalid")
	}
}
