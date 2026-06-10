package worktreecreate

import (
	"errors"
	"strings"

	"builder/shared/serverapi"
)

func Request(branchTarget string, baseRef string, kind serverapi.WorktreeCreateTargetResolutionKind) (serverapi.WorktreeCreateRequest, error) {
	if strings.TrimSpace(branchTarget) == "" {
		return serverapi.WorktreeCreateRequest{}, errors.New("Branch or ref is required")
	}
	target := strings.TrimSpace(branchTarget)
	if kind == serverapi.WorktreeCreateTargetResolutionKindExistingBranch || kind == serverapi.WorktreeCreateTargetResolutionKindDetachedRef {
		return serverapi.WorktreeCreateRequest{BaseRef: target, CreateBranch: false}, nil
	}
	trimmedBaseRef := strings.TrimSpace(baseRef)
	if trimmedBaseRef == "" {
		return serverapi.WorktreeCreateRequest{}, errors.New("Base ref is required")
	}
	return serverapi.WorktreeCreateRequest{BaseRef: trimmedBaseRef, CreateBranch: true, BranchName: target}, nil
}
