package worktreeui

import (
	"strings"

	"core/shared/protoapi"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
)

func Request(branchTarget string, baseRef string, kind worktreepb.CreateTargetResolutionKind) (*worktreepb.CreateRequest, error) {
	target := strings.TrimSpace(branchTarget)
	spec := &worktreepb.CreateSpec{CreateBranch: kind == worktreepb.CreateTargetResolutionKind_WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH}
	if spec.CreateBranch {
		trimmedBaseRef := strings.TrimSpace(baseRef)
		spec.BaseRef = &trimmedBaseRef
		spec.BranchName = &target
	} else {
		spec.BaseRef = &target
	}
	if err := protoapi.Validate(spec); err != nil {
		return nil, protoapi.ClassifyWorktreeCreateValidation(err)
	}
	return &worktreepb.CreateRequest{Spec: spec}, nil
}
