package serverapi

import worktreepb "core/shared/protoapi/gen/kent/api/worktree"

func (e *WorkflowSetupRetainedError) protoDetails() *worktreepb.SetupRetainedDetails {
	var disposition worktreepb.SetupRecoveryDisposition
	switch e.RecoveryDisposition {
	case WorkflowSetupRecoveryRetryExisting:
		disposition = worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_RETRY_EXISTING
	case WorkflowSetupRecoveryFreshReplacement:
		disposition = worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_FRESH_REPLACEMENT
	}
	result := &worktreepb.SetupRetainedDetails{
		Worktree: registeredWorktreeProto(e.Worktree), ScriptPath: e.ScriptPath,
		Diagnostic: e.Diagnostic, RecoveryDisposition: disposition,
	}
	if e.RetainedPreviousWorktree != nil {
		result.RetainedPreviousWorktree = &worktreepb.RetainedPreviousWorktree{
			Worktree: registeredWorktreeProto(e.RetainedPreviousWorktree.Worktree),
		}
	}
	return result
}

func registeredWorktreeProto(topology WorkflowRegisteredWorktreeTopology) *worktreepb.RegisteredFacts {
	if topology.Registered == nil {
		return nil
	}
	git, kent := topology.Registered.Git, topology.Registered.Kent
	return &worktreepb.RegisteredFacts{
		Git: &worktreepb.GitFacts{
			CanonicalRoot: git.CanonicalRoot, HeadObject: git.HeadObject,
			BranchRef: git.BranchRef, BranchName: git.BranchName, Detached: git.Detached,
			Bare: git.Bare, LockedReason: git.LockedReason, PrunableReason: git.PrunableReason,
			IsMainWorktree: git.IsMainWorktree, PathAvailable: git.PathAvailable,
		},
		Kent: &worktreepb.KentFacts{
			WorktreeId: kent.WorktreeID, CanonicalRoot: kent.CanonicalRoot,
			DisplayName: kent.DisplayName, Managed: kent.Managed,
			CreatedBranch: kent.CreatedBranch, OriginSessionId: kent.OriginSessionID,
		},
	}
}
