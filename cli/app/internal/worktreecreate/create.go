package worktreecreate

import (
	"errors"
	"strings"

	"core/shared/serverapi"
)

// ErrBranchTargetRequired and ErrBaseRefRequired guard worktree-create request
// validation. Callers and tests match these with errors.Is rather than
// comparing rendered message text.
var (
	ErrBranchTargetRequired = errors.New("Branch or ref is required")
	ErrBaseRefRequired      = errors.New("Base ref is required")
)

func Request(branchTarget string, baseRef string, kind serverapi.WorktreeCreateTargetResolutionKind) (serverapi.WorktreeCreateRequest, error) {
	if strings.TrimSpace(branchTarget) == "" {
		return serverapi.WorktreeCreateRequest{}, ErrBranchTargetRequired
	}
	target := strings.TrimSpace(branchTarget)
	if kind == serverapi.WorktreeCreateTargetResolutionKindExistingBranch || kind == serverapi.WorktreeCreateTargetResolutionKindDetachedRef {
		return serverapi.WorktreeCreateRequest{BaseRef: target, CreateBranch: false}, nil
	}
	trimmedBaseRef := strings.TrimSpace(baseRef)
	if trimmedBaseRef == "" {
		return serverapi.WorktreeCreateRequest{}, ErrBaseRefRequired
	}
	return serverapi.WorktreeCreateRequest{BaseRef: trimmedBaseRef, CreateBranch: true, BranchName: target}, nil
}
