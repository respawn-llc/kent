package main

import (
	"bytes"
	"strings"
	"testing"

	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
)

func TestTaskDeleteRetainedBranchWarnsWithoutFailingDeletion(t *testing.T) {
	displayID := "KENT-42"
	branchName := "task/KENT-42"
	diagnostic := "branch has unmerged commits"
	var stdout, stderr bytes.Buffer

	exitCode := writeTaskDeleteSuccess(&stdout, &stderr, displayID, &taskpb.DeleteSuccess{
		BranchCleanup: &worktreepb.BranchCleanupOutcome{
			Kind:       worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED,
			BranchName: &branchName,
			Diagnostic: &diagnostic,
		},
	})

	if exitCode != 0 {
		t.Fatalf("retained branch deletion exit code = %d, want success", exitCode)
	}
	if !strings.Contains(stdout.String(), displayID) {
		t.Fatalf("successful deletion output omitted task identity: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), branchName) || !strings.Contains(stderr.String(), diagnostic) {
		t.Fatalf("retained branch warning omitted cleanup details: %q", stderr.String())
	}
}

func TestTaskDeleteNonRetainedCleanupDoesNotWarn(t *testing.T) {
	branchName := "task/KENT-42"
	tests := []struct {
		name    string
		cleanup *worktreepb.BranchCleanupOutcome
	}{
		{name: "no managed worktree"},
		{
			name: "not applicable",
			cleanup: &worktreepb.BranchCleanupOutcome{
				Kind: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_APPLICABLE,
			},
		},
		{
			name: "not requested",
			cleanup: &worktreepb.BranchCleanupOutcome{
				Kind: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_REQUESTED,
			},
		},
		{
			name: "deleted",
			cleanup: &worktreepb.BranchCleanupOutcome{
				Kind:       worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_DELETED,
				BranchName: &branchName,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exitCode := writeTaskDeleteSuccess(&stdout, &stderr, "KENT-42", &taskpb.DeleteSuccess{
				BranchCleanup: test.cleanup,
			})
			if exitCode != 0 || stdout.Len() == 0 || stderr.Len() != 0 {
				t.Fatalf("deletion output exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
		})
	}
}
