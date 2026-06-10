package worktreecreate

import (
	"testing"

	"builder/shared/serverapi"
)

func TestRequestForExistingBranchUsesTargetAsBaseRef(t *testing.T) {
	req, err := Request(" main ", "ignored", serverapi.WorktreeCreateTargetResolutionKindExistingBranch)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req.BaseRef != "main" || req.CreateBranch || req.BranchName != "" {
		t.Fatalf("request = %+v, want existing branch checkout", req)
	}
}

func TestRequestForDetachedRefUsesTargetAsBaseRef(t *testing.T) {
	req, err := Request(" HEAD~1 ", "ignored", serverapi.WorktreeCreateTargetResolutionKindDetachedRef)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req.BaseRef != "HEAD~1" || req.CreateBranch || req.BranchName != "" {
		t.Fatalf("request = %+v, want detached ref checkout", req)
	}
}

func TestRequestForNewBranchRequiresBaseRef(t *testing.T) {
	req, err := Request(" feature/a ", " HEAD ", serverapi.WorktreeCreateTargetResolutionKindNewBranch)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req.BaseRef != "HEAD" || !req.CreateBranch || req.BranchName != "feature/a" {
		t.Fatalf("request = %+v, want new branch request", req)
	}
}

func TestRequestRejectsBlankTarget(t *testing.T) {
	if _, err := Request(" ", "HEAD", serverapi.WorktreeCreateTargetResolutionKindExistingBranch); err == nil || err.Error() != "Branch or ref is required" {
		t.Fatalf("error = %v, want target required", err)
	}
}

func TestRequestRejectsBlankBaseRefForNewBranch(t *testing.T) {
	if _, err := Request("feature/a", " ", serverapi.WorktreeCreateTargetResolutionKindNewBranch); err == nil || err.Error() != "Base ref is required" {
		t.Fatalf("error = %v, want base ref required", err)
	}
}
