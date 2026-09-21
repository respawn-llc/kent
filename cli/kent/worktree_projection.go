package main

import (
	"fmt"
	"io"

	"core/shared/protoapi"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
)

type worktreeScheduledAcknowledgementJSON struct {
	OperationID string `json:"operation_id"`
}

type worktreeExecutionTargetOutput struct {
	WorkspaceID           *string                      `json:"workspace_id"`
	WorkspaceName         string                       `json:"workspace_name"`
	WorkspaceRoot         string                       `json:"workspace_root"`
	WorkspaceAvailability string                       `json:"workspace_availability"`
	Worktree              *worktreeExecutionTargetItem `json:"worktree"`
	CwdRelpath            string                       `json:"cwd_relpath"`
	EffectiveWorkdir      string                       `json:"effective_workdir"`
}

type worktreeExecutionTargetItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Root         string `json:"root"`
	Availability string `json:"availability"`
}

type worktreeStatusJSONOutput struct {
	Target   worktreeExecutionTargetOutput `json:"target"`
	Worktree worktreeStatusTargetJSON      `json:"worktree"`
	Problems []worktreeStatusProblemJSON   `json:"problems"`
}

type worktreeStatusTargetJSON struct {
	RecordedRoot      string  `json:"recorded_root"`
	ObservedRoot      *string `json:"observed_root,omitempty"`
	DisplayName       *string `json:"display_name,omitempty"`
	RecordedBranchRef *string `json:"recorded_branch_ref,omitempty"`
	ObservedBranchRef *string `json:"observed_branch_ref,omitempty"`
}

type worktreeStatusProblemJSON struct {
	Kind string  `json:"kind"`
	Root *string `json:"root,omitempty"`
	Ref  *string `json:"ref,omitempty"`
}

type worktreeListJSONOutput struct {
	Target    worktreeExecutionTargetOutput `json:"target"`
	Worktrees []worktreeListEntryJSON       `json:"worktrees"`
}

type worktreeWorkspaceListJSONOutput struct {
	WorkspaceID string                  `json:"workspace_id"`
	Worktrees   []worktreeListEntryJSON `json:"worktrees"`
}

type worktreeCreateJSONOutput struct {
	Target   *worktreeExecutionTargetOutput `json:"target,omitempty"`
	Worktree worktreeListEntryJSON          `json:"worktree"`
}

type worktreeListEntryJSON struct {
	Topology   worktreeTopologyJSON   `json:"topology"`
	Projection worktreeProjectionJSON `json:"projection"`
}

type worktreeTopologyJSON struct {
	Variant       string                       `json:"variant"`
	MainWorkspace *worktreeExternalFactsJSON   `json:"main_workspace,omitempty"`
	Registered    *worktreeRegisteredFactsJSON `json:"registered,omitempty"`
	External      *worktreeExternalFactsJSON   `json:"external,omitempty"`
	Missing       *worktreeMissingFactsJSON    `json:"missing,omitempty"`
}

type worktreeRegisteredFactsJSON struct {
	Git  worktreeGitFactsJSON  `json:"git"`
	Kent worktreeKentFactsJSON `json:"kent"`
}

type worktreeExternalFactsJSON struct {
	Git worktreeGitFactsJSON `json:"git"`
}

type worktreeMissingFactsJSON struct {
	Kent worktreeKentFactsJSON `json:"kent"`
}

type worktreeGitFactsJSON struct {
	CanonicalRoot  string  `json:"canonical_root"`
	HeadObject     string  `json:"head_object"`
	BranchRef      *string `json:"branch_ref"`
	BranchName     *string `json:"branch_name"`
	Detached       bool    `json:"detached"`
	Bare           bool    `json:"bare"`
	LockedReason   *string `json:"locked_reason"`
	PrunableReason *string `json:"prunable_reason"`
	IsMain         bool    `json:"is_main"`
	PathAvailable  bool    `json:"path_available"`
}

type worktreeKentFactsJSON struct {
	WorktreeID      string  `json:"worktree_id"`
	CanonicalRoot   string  `json:"canonical_root"`
	DisplayName     string  `json:"display_name"`
	Managed         bool    `json:"managed"`
	CreatedBranch   bool    `json:"created_branch"`
	OriginSessionID *string `json:"origin_session_id"`
}

type worktreeProjectionJSON struct {
	Selector         string                              `json:"selector"`
	IsCurrent        bool                                `json:"is_current"`
	Switch           *worktreeSwitchOperationJSON        `json:"switch,omitempty"`
	DeletePreview    *worktreeDeletePreviewOperationJSON `json:"delete_preview,omitempty"`
	FallbackIdentity *string                             `json:"fallback_identity,omitempty"`
}

type worktreeSwitchOperationJSON struct {
	Kind     string  `json:"kind"`
	Selector *string `json:"selector,omitempty"`
}

type worktreeDeletePreviewOperationJSON struct {
	Selector string `json:"selector"`
}

type worktreeDeleteJSONOutput struct {
	Cleanup      worktreeBranchCleanupJSON `json:"cleanup"`
	LeftoverRoot *string                   `json:"leftover_root,omitempty"`
}

type worktreeBranchCleanupJSON struct {
	Kind       string  `json:"kind"`
	BranchName *string `json:"branch_name,omitempty"`
	Diagnostic *string `json:"diagnostic,omitempty"`
}

func writeWorktreeJSON(stdout, stderr io.Writer, output any, err error) int {
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return writeCommandJSON(stdout, stderr, output)
}

func worktreeStatusJSON(success *worktreepb.StatusSuccess) (worktreeStatusJSONOutput, error) {
	target, err := worktreeExecutionTargetJSON(success.GetTarget())
	if err != nil {
		return worktreeStatusJSONOutput{}, err
	}
	problems := make([]worktreeStatusProblemJSON, len(success.Problems))
	for i, problem := range success.Problems {
		kind, err := worktreeStatusProblemKindJSON(problem.Kind)
		if err != nil {
			return worktreeStatusJSONOutput{}, err
		}
		problems[i] = worktreeStatusProblemJSON{Kind: kind, Root: problem.Root, Ref: problem.Ref}
	}
	worktree := success.Worktree
	return worktreeStatusJSONOutput{
		Target: target,
		Worktree: worktreeStatusTargetJSON{
			RecordedRoot: worktree.RecordedRoot, ObservedRoot: worktree.ObservedRoot,
			DisplayName: worktree.DisplayName, RecordedBranchRef: worktree.RecordedBranchRef,
			ObservedBranchRef: worktree.ObservedBranchRef,
		},
		Problems: problems,
	}, nil
}

func worktreeListJSON(success *worktreepb.ListSuccess) (worktreeListJSONOutput, error) {
	target, err := worktreeExecutionTargetJSON(success.Target)
	if err != nil {
		return worktreeListJSONOutput{}, err
	}
	worktrees, err := worktreeListEntriesJSON(success.Worktrees)
	return worktreeListJSONOutput{Target: target, Worktrees: worktrees}, err
}

func worktreeWorkspaceListJSON(success *worktreepb.WorkspaceListSuccess) (worktreeWorkspaceListJSONOutput, error) {
	worktrees, err := worktreeListEntriesJSON(success.Worktrees)
	return worktreeWorkspaceListJSONOutput{WorkspaceID: success.WorkspaceId, Worktrees: worktrees}, err
}

func worktreeCreateJSON(success *worktreepb.CreateSuccess) (worktreeCreateJSONOutput, error) {
	var target *worktreeExecutionTargetOutput
	if success.Target != nil {
		value, err := worktreeExecutionTargetJSON(success.Target)
		if err != nil {
			return worktreeCreateJSONOutput{}, err
		}
		target = &value
	}
	worktree, err := worktreeListEntryJSONFromProto(success.Worktree)
	return worktreeCreateJSONOutput{Target: target, Worktree: worktree}, err
}

func worktreeDeleteJSON(success *worktreepb.DeleteSuccess) (worktreeDeleteJSONOutput, error) {
	kind, err := worktreeBranchCleanupKindJSON(success.GetCleanup().GetKind())
	if err != nil {
		return worktreeDeleteJSONOutput{}, err
	}
	return worktreeDeleteJSONOutput{
		Cleanup: worktreeBranchCleanupJSON{
			Kind: kind, BranchName: success.Cleanup.BranchName, Diagnostic: success.Cleanup.Diagnostic,
		},
		LeftoverRoot: success.LeftoverRoot,
	}, nil
}

func worktreeExecutionTargetJSON(target *worktreepb.SessionExecutionTarget) (worktreeExecutionTargetOutput, error) {
	availability, err := protoapi.ProjectAvailabilityFromProto(target.WorkspaceAvailability)
	if err != nil {
		return worktreeExecutionTargetOutput{}, err
	}
	var worktree *worktreeExecutionTargetItem
	if value := target.Worktree; value != nil {
		availability, err := protoapi.ProjectAvailabilityFromProto(value.Availability)
		if err != nil {
			return worktreeExecutionTargetOutput{}, err
		}
		worktree = &worktreeExecutionTargetItem{ID: value.Id, Name: value.Name, Root: value.Root, Availability: string(availability)}
	}
	return worktreeExecutionTargetOutput{
		WorkspaceID: target.WorkspaceId, WorkspaceName: target.WorkspaceName, WorkspaceRoot: target.WorkspaceRoot,
		WorkspaceAvailability: string(availability), Worktree: worktree,
		CwdRelpath: target.CwdRelpath, EffectiveWorkdir: target.EffectiveWorkdir,
	}, nil
}

func worktreeListEntriesJSON(entries []*worktreepb.ListEntry) ([]worktreeListEntryJSON, error) {
	result := make([]worktreeListEntryJSON, len(entries))
	for i, entry := range entries {
		projected, err := worktreeListEntryJSONFromProto(entry)
		if err != nil {
			return nil, err
		}
		result[i] = projected
	}
	return result, nil
}

func worktreeListEntryJSONFromProto(entry *worktreepb.ListEntry) (worktreeListEntryJSON, error) {
	variant, err := worktreeTopologyVariantJSON(entry.Topology)
	if err != nil {
		return worktreeListEntryJSON{}, err
	}
	topology := worktreeTopologyJSON{Variant: variant}
	switch value := entry.Topology.Topology.(type) {
	case *worktreepb.TopologyEntry_MainWorkspace:
		topology.MainWorkspace = &worktreeExternalFactsJSON{Git: worktreeGitFactsJSONFromProto(value.MainWorkspace.Git)}
	case *worktreepb.TopologyEntry_Registered:
		topology.Registered = &worktreeRegisteredFactsJSON{
			Git: worktreeGitFactsJSONFromProto(value.Registered.Git), Kent: worktreeKentFactsJSONFromProto(value.Registered.Kent),
		}
	case *worktreepb.TopologyEntry_External:
		topology.External = &worktreeExternalFactsJSON{Git: worktreeGitFactsJSONFromProto(value.External.Git)}
	case *worktreepb.TopologyEntry_Missing:
		topology.Missing = &worktreeMissingFactsJSON{Kent: worktreeKentFactsJSONFromProto(value.Missing.Kent)}
	}
	projection := entry.Projection
	result := worktreeListEntryJSON{Topology: topology, Projection: worktreeProjectionJSON{
		Selector: projection.Selector, IsCurrent: projection.IsCurrent, FallbackIdentity: projection.FallbackIdentity,
	}}
	if value := projection.Switch; value != nil {
		kind, err := worktreeSwitchKindJSON(value.Kind)
		if err != nil {
			return worktreeListEntryJSON{}, err
		}
		result.Projection.Switch = &worktreeSwitchOperationJSON{Kind: kind, Selector: value.Selector}
	}
	if value := projection.DeletePreview; value != nil {
		result.Projection.DeletePreview = &worktreeDeletePreviewOperationJSON{Selector: value.Selector}
	}
	return result, nil
}

func worktreeGitFactsJSONFromProto(facts *worktreepb.GitFacts) worktreeGitFactsJSON {
	return worktreeGitFactsJSON{
		CanonicalRoot: facts.CanonicalRoot, HeadObject: facts.HeadObject,
		BranchRef: facts.BranchRef, BranchName: facts.BranchName, Detached: facts.Detached, Bare: facts.Bare,
		LockedReason: facts.LockedReason, PrunableReason: facts.PrunableReason,
		IsMain: facts.IsMainWorktree, PathAvailable: facts.PathAvailable,
	}
}

func worktreeKentFactsJSONFromProto(facts *worktreepb.KentFacts) worktreeKentFactsJSON {
	return worktreeKentFactsJSON{
		WorktreeID: facts.WorktreeId, CanonicalRoot: facts.CanonicalRoot, DisplayName: facts.DisplayName,
		Managed: facts.Managed, CreatedBranch: facts.CreatedBranch, OriginSessionID: facts.OriginSessionId,
	}
}

func worktreeSwitchKindJSON(kind worktreepb.SwitchOperationKind) (string, error) {
	switch kind {
	case worktreepb.SwitchOperationKind_WORKTREE_SWITCH_OPERATION_ENTER:
		return "enter", nil
	case worktreepb.SwitchOperationKind_WORKTREE_SWITCH_OPERATION_LEAVE_MAIN:
		return "leave", nil
	default:
		return "", fmt.Errorf("unsupported worktree switch operation %s", kind)
	}
}

func worktreeBranchCleanupKindJSON(kind worktreepb.BranchCleanupOutcomeKind) (string, error) {
	switch kind {
	case worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_REQUESTED:
		return "not_requested", nil
	case worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_APPLICABLE:
		return "not_applicable", nil
	case worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_DELETED:
		return "deleted", nil
	case worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED:
		return "retained", nil
	default:
		return "", fmt.Errorf("unsupported worktree branch cleanup outcome %s", kind)
	}
}
