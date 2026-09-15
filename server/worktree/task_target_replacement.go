package worktree

import (
	"context"
	"errors"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/textutil"
)

func (s *Service) prepareReplacementTaskExecutionRoot(
	ctx context.Context,
	req TaskExecutionRootPreparationRequest,
	task sqlitegen.TaskRecord,
	workspace taskSourceWorkspace,
	target *GitRevision,
) (TaskExecutionRootPreparation, error) {
	replacement := req.Replacement
	if replacement.Commit == nil {
		return TaskExecutionRootPreparation{}, errors.New("replacement target commit is required")
	}
	if err := replacement.Selection.Validate(); err != nil {
		return TaskExecutionRootPreparation{}, err
	}
	if (replacement.Selection.Mode == workflow.ExecutionTargetModeNone) != (target == nil) {
		return TaskExecutionRootPreparation{}, errors.New("replacement selection and resolved target disagree")
	}
	if !textutil.EqualOptional(metadata.OptionalString(task.ManagedWorktreeID), replacement.ExpectedManagedWorktreeID) {
		return TaskExecutionRootPreparation{}, errors.New("Task managed Worktree changed before replacement")
	}
	_, err := s.validateLockedTaskWorktree(ctx, LockedTaskWorktreeValidationRequest{TaskID: req.TaskID}, task, workspace)
	var missing *workflow.MissingManagedWorktree
	if !errors.As(err, &missing) {
		if err == nil {
			err = workflowstore.ErrExecutionTargetAlreadyLocked
		}
		return TaskExecutionRootPreparation{}, err
	}
	var previous *metadata.WorktreeRecord
	if task.ManagedWorktreeID.Valid {
		record, err := s.metadata.GetWorktreeRecordByID(ctx, task.ManagedWorktreeID.String)
		if err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		previous = &record
		if err := s.ensureNoOtherNonTerminalTasksManageWorktree(ctx, task.ID, record); err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		activity, err := s.acquireDeleteTargetActivity(ctx, &record, &record.CanonicalRoot)
		if err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		defer activity.Close()
		ctx = activity.Context()
		if _, err := s.retargetDeleteSessions(ctx, metadata.Binding{
			WorkspaceID: workspace.WorkspaceID, CanonicalRoot: workspace.RootPath,
		}, record); err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		_, registered, err := s.git.FindCreatedWorktree(ctx, workspace.RootPath, record.CanonicalRoot)
		if err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		if registered {
			if err := s.git.Remove(ctx, workspace.RootPath, record.CanonicalRoot, false); err != nil {
				return TaskExecutionRootPreparation{}, err
			}
		}
	}
	root := workflowstore.ExecutionRoot{SourceWorkspaceID: workspace.WorkspaceID, SourceWorkspaceRoot: workspace.RootPath}
	if target == nil {
		if err := replacement.Commit(ctx, nil); err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		return TaskExecutionRootPreparation{Root: root}, nil
	}
	if previous != nil && replacement.Selection.Mode == workflow.ExecutionTargetModeCustomRef {
		recorded, err := worktreeGitMetadataFromRecord(*previous)
		if err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		if recorded.RecordedBranch != nil && target.CanonicalRef != nil &&
			*target.CanonicalRef == recorded.RecordedBranch.Ref() {
			if err := validateInitialTaskBranchAssertion(req.BranchName, *recorded.RecordedBranch); err != nil {
				return TaskExecutionRootPreparation{}, err
			}
			materialized, err := s.createManagedTaskWorktree(ctx, managedTaskWorktreeCreationRequest{
				Task: task, Workspace: workspace,
				CreateSpec:       CreateSpec{BaseRef: recorded.RecordedBranch.Name()},
				SetupOperationID: req.SetupOperationID, CreationBaseOID: &target.CommitOID,
				Replacement: &managedTaskWorktreeReplacement{missingRoot: &previous.CanonicalRoot, commit: replacement.Commit},
			})
			return taskExecutionRootPreparation(root, materialized, nil), err
		}
	}
	materialized, err := s.prepareManagedTaskWorktree(ctx, task, workspace, req.SetupOperationID, *target, nil, req.SetupRequirement, req.BranchName)
	return taskExecutionRootPreparation(root, materialized, nil), err
}
