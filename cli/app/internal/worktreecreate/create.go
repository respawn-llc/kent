package worktreecreate

import (
	"errors"
	"strings"

	"builder/shared/serverapi"
)

func ValidateTarget(branchTarget string) error {
	if strings.TrimSpace(branchTarget) == "" {
		return errors.New("Branch or ref is required")
	}
	return nil
}

func Request(branchTarget string, baseRef string, kind serverapi.WorktreeCreateTargetResolutionKind) (serverapi.WorktreeCreateRequest, error) {
	if err := ValidateTarget(branchTarget); err != nil {
		return serverapi.WorktreeCreateRequest{}, err
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
