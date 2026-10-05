package main

import (
	"errors"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
)

type taskMoveJSON struct {
	Outcome                    string                `json:"outcome"`
	Applied                    *taskMoveMutationJSON `json:"applied,omitempty"`
	NoOp                       *taskMoveMutationJSON `json:"no_op,omitempty"`
	SelectionRequired          *taskSelectionJSON    `json:"selection_required,omitempty"`
	UnsatisfiedDependencyCount *int32                `json:"unsatisfied_dependency_count,omitempty"`
}

type taskMoveMutationJSON struct {
	CurrentNodes             []*taskpb.AttentionCurrentNode `json:"current_nodes"`
	RetainedPreviousWorktree *taskRetainedWorktreeJSON      `json:"retained_previous_worktree"`
}

type taskRetainedWorktreeJSON struct {
	Worktree taskRegisteredWorktreeJSON `json:"worktree"`
}

type taskRegisteredWorktreeJSON struct {
	Variant    string                  `json:"variant"`
	Registered taskRegisteredFactsJSON `json:"registered"`
}

type taskRegisteredFactsJSON struct {
	Git  taskWorktreeGitJSON   `json:"git"`
	Kent worktreeKentFactsJSON `json:"kent"`
}

// Task CLI output names this fact is_main_worktree. The value projection is
// shared with Worktree commands through the struct conversion below.
type taskWorktreeGitJSON struct {
	CanonicalRoot  string  `json:"canonical_root"`
	HeadObject     string  `json:"head_object"`
	BranchRef      *string `json:"branch_ref"`
	BranchName     *string `json:"branch_name"`
	Detached       bool    `json:"detached"`
	Bare           bool    `json:"bare"`
	LockedReason   *string `json:"locked_reason"`
	PrunableReason *string `json:"prunable_reason"`
	IsMain         bool    `json:"is_main_worktree"`
	PathAvailable  bool    `json:"path_available"`
}

func taskRegisteredWorktreeOutput(value *worktreepb.RegisteredFacts) taskRegisteredWorktreeJSON {
	return taskRegisteredWorktreeJSON{
		Variant: "registered",
		Registered: taskRegisteredFactsJSON{
			Git:  taskWorktreeGitJSON(worktreeGitFactsJSONFromProto(value.Git)),
			Kent: worktreeKentFactsJSONFromProto(value.Kent),
		},
	}
}

func taskRetainedWorktreeOutput(value *worktreepb.RetainedPreviousWorktree) *taskRetainedWorktreeJSON {
	if value == nil {
		return nil
	}
	return &taskRetainedWorktreeJSON{Worktree: taskRegisteredWorktreeOutput(value.Worktree)}
}

func taskMoveOutput(response *taskpb.MoveSuccess) (taskMoveJSON, error) {
	switch outcome := response.GetOutcome().(type) {
	case *taskpb.MoveSuccess_Applied:
		return taskMoveJSON{Outcome: "applied", Applied: &taskMoveMutationJSON{
			CurrentNodes:             outcome.Applied.CurrentNodes,
			RetainedPreviousWorktree: taskRetainedWorktreeOutput(outcome.Applied.RetainedPreviousWorktree),
		}}, nil
	case *taskpb.MoveSuccess_NoOp:
		return taskMoveJSON{Outcome: "no_op", NoOp: &taskMoveMutationJSON{
			CurrentNodes:             outcome.NoOp.CurrentNodes,
			RetainedPreviousWorktree: taskRetainedWorktreeOutput(outcome.NoOp.RetainedPreviousWorktree),
		}}, nil
	case *taskpb.MoveSuccess_SelectionRequired:
		selection, err := taskSelectionOutput(outcome.SelectionRequired)
		return taskMoveJSON{Outcome: "selection_required", SelectionRequired: selection}, err
	case *taskpb.MoveSuccess_DependencyConfirmationRequired:
		return taskMoveJSON{
			Outcome:                    "dependency_confirmation_required",
			UnsatisfiedDependencyCount: &outcome.DependencyConfirmationRequired.UnsatisfiedDependencyCount,
		}, nil
	default:
		return taskMoveJSON{}, errors.New("invalid Task Move outcome")
	}
}

type taskSetupRetainedJSON struct {
	Type                     string                     `json:"type"`
	RecoveryDisposition      string                     `json:"recovery_disposition"`
	Worktree                 taskRegisteredWorktreeJSON `json:"worktree"`
	ScriptPath               string                     `json:"script_path"`
	Diagnostic               string                     `json:"diagnostic"`
	RetainedPreviousWorktree *taskRetainedWorktreeJSON  `json:"retained_previous_worktree"`
}

func taskSetupRetainedOutput(details *worktreepb.SetupRetainedDetails) (taskSetupRetainedJSON, error) {
	if err := protoapi.Validate(details); err != nil {
		return taskSetupRetainedJSON{}, err
	}
	var disposition string
	switch details.RecoveryDisposition {
	case worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_RETRY_EXISTING:
		disposition = "retry_existing"
	case worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_FRESH_REPLACEMENT:
		disposition = "fresh_replacement"
	default:
		return taskSetupRetainedJSON{}, errors.New("invalid setup recovery disposition")
	}
	return taskSetupRetainedJSON{
		Type: "worktree_setup_retained", RecoveryDisposition: disposition,
		Worktree: taskRegisteredWorktreeOutput(details.Worktree), ScriptPath: details.ScriptPath,
		Diagnostic: details.Diagnostic, RetainedPreviousWorktree: taskRetainedWorktreeOutput(details.RetainedPreviousWorktree),
	}, nil
}
