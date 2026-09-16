package worktree

import (
	"context"
	"errors"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/server/workflowstore"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
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
	_, err := s.validateLockedTaskWorktree(ctx, LockedTaskWorktreeValidationRequest{TaskID: req.TaskID, SetupRecovery: req.SetupRecovery}, task, workspace)
	var missing *workflow.MissingManagedWorktree
	if err != nil && !errors.As(err, &missing) {
		return TaskExecutionRootPreparation{}, err
	}
	if missing == nil && req.SetupRecovery == nil {
		return TaskExecutionRootPreparation{}, workflowstore.ErrExecutionTargetAlreadyLocked
	}
	var previous *metadata.WorktreeRecord
	var retainedPrevious *worktreepb.RetainedPreviousWorktree
	if task.ManagedWorktreeID.Valid {
		record, err := s.metadata.GetWorktreeRecordByID(ctx, task.ManagedWorktreeID.String)
		if err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		previous = &record
		if missing == nil {
			if err := validateRetainedTaskSetupRecovery(req.SetupRecovery, task, record); err != nil {
				return TaskExecutionRootPreparation{}, err
			}
		}
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
		if missing == nil {
			retainedPrevious, err = s.releaseTaskWorktreeArtifact(ctx, workspace, record, nil)
			if err != nil {
				return TaskExecutionRootPreparation{}, err
			}
		} else {
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
	}
	root := workflowstore.ExecutionRoot{SourceWorkspaceID: workspace.WorkspaceID, SourceWorkspaceRoot: workspace.RootPath}
	if target == nil {
		if err := replacement.Commit(ctx, nil, retainedPrevious); err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		return TaskExecutionRootPreparation{Root: root, RetainedPreviousWorktree: retainedPrevious}, nil
	}
	createSpec := CreateSpec{BaseRef: target.CommitOID, CreateBranch: true, BranchName: task.ShortID}
	if req.BranchName != nil {
		createSpec.BranchName = *req.BranchName
	}
	if missing != nil && previous != nil && replacement.Selection.Mode == workflow.ExecutionTargetModeCustomRef {
		recorded, err := worktreeGitMetadataFromRecord(*previous)
		if err != nil {
			return TaskExecutionRootPreparation{}, err
		}
		if recorded.RecordedBranch != nil && target.CanonicalRef != nil &&
			*target.CanonicalRef == recorded.RecordedBranch.Ref() {
			if err := validateInitialTaskBranchAssertion(req.BranchName, *recorded.RecordedBranch); err != nil {
				return TaskExecutionRootPreparation{}, err
			}
			createSpec = CreateSpec{BaseRef: recorded.RecordedBranch.Name()}
		}
	}
	if createSpec.CreateBranch {
		if err := s.git.InspectProspectiveInitialTaskBranch(ctx, workspace.RootPath, createSpec.BranchName); err != nil {
			return TaskExecutionRootPreparation{}, workflowTaskInitialBranchError(err)
		}
	}
	var missingRoot *string
	if previous != nil && retainedPrevious == nil {
		missingRoot = &previous.CanonicalRoot
	}
	materialized, err := s.createManagedTaskWorktree(ctx, managedTaskWorktreeCreationRequest{
		Task: task, Workspace: workspace, CreateSpec: createSpec,
		SetupOperationID: req.SetupOperationID, CreationBaseOID: &target.CommitOID,
		Replacement: &managedTaskWorktreeReplacement{
			missingRoot: missingRoot,
			commit: func(ctx context.Context, record *metadata.WorktreeRecord) error {
				return replacement.Commit(ctx, record, retainedPrevious)
			},
		},
	})
	return taskExecutionRootPreparation(root, materialized, retainedPrevious), err
}

func (s *Service) retrySelectedTaskWorktreeSetup(
	ctx context.Context,
	req TaskExecutionRootPreparationRequest,
	task sqlitegen.TaskRecord,
	workspace taskSourceWorkspace,
	target *GitRevision,
) (TaskExecutionRootPreparation, error) {
	// Resume captures this canonical recovery while holding the Task mutation lane,
	// before accepted preparation clears the Current Node's interruption.
	recovery := req.SetupRecovery
	if target == nil || !task.ManagedWorktreeID.Valid {
		return TaskExecutionRootPreparation{}, errors.New("locked target setup retry requires its retained setup recovery")
	}
	record, err := s.metadata.GetWorktreeRecordByID(ctx, task.ManagedWorktreeID.String)
	if err != nil {
		return TaskExecutionRootPreparation{}, err
	}
	if err := validateRetainedTaskSetupRecovery(recovery, task, record); err != nil {
		return TaskExecutionRootPreparation{}, err
	}
	materialized, err := s.prepareManagedTaskWorktree(ctx, task, workspace, req.SetupOperationID, *target, &record, req.SetupRequirement, req.BranchName)
	root := workflowstore.ExecutionRoot{SourceWorkspaceID: workspace.WorkspaceID, SourceWorkspaceRoot: workspace.RootPath}
	return taskExecutionRootPreparation(root, materialized, nil), err
}

func validateRetainedTaskSetupRecovery(recovery *workflow.CurrentNodeSetupRecoveryDetail, task sqlitegen.TaskRecord, record metadata.WorktreeRecord) error {
	if recovery == nil || recovery.RetainedWorktree == nil || !task.ManagedWorktreeID.Valid ||
		task.ManagedWorktreeID.String != recovery.RetainedWorktree.WorktreeID ||
		record.ID != recovery.RetainedWorktree.WorktreeID || record.CanonicalRoot != recovery.RetainedWorktree.Root {
		return errors.New("setup recovery does not match the retained Task Worktree")
	}
	return recovery.Validate()
}
