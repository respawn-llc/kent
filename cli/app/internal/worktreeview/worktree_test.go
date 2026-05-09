package worktreeview

import (
	"errors"
	"strings"
	"testing"

	"builder/shared/serverapi"
)

func TestResolveTokenUsesMatcherPrecedence(t *testing.T) {
	entries := []serverapi.WorktreeView{
		{WorktreeID: "wt-1", DisplayName: "feature", CanonicalRoot: "/wt/feature-display"},
		{WorktreeID: "wt-2", DisplayName: "other", BranchName: "feature", CanonicalRoot: "/wt/feature-branch"},
	}
	resolved, err := ResolveToken(entries, "feature")
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if resolved.WorktreeID != "wt-1" {
		t.Fatalf("resolved worktree id = %q, want wt-1", resolved.WorktreeID)
	}
}

func TestResolveDeletionTargetRejectsCurrentMainWorkspace(t *testing.T) {
	_, err := ResolveDeletionTarget([]serverapi.WorktreeView{{WorktreeID: "main", IsMain: true, IsCurrent: true}}, "")
	if err == nil || !strings.Contains(err.Error(), "main workspace is not deletable") {
		t.Fatalf("expected main workspace rejection, got %v", err)
	}
}

func TestResolveDeletionTargetFallsBackToNotFound(t *testing.T) {
	_, err := ResolveDeletionTarget(nil, "")
	if !errors.Is(err, serverapi.ErrWorktreeNotFound) {
		t.Fatalf("expected worktree not found, got %v", err)
	}
}

func TestSanitizeBranchSuggestion(t *testing.T) {
	if got := SanitizeBranchSuggestion(" Fix: My Feature!! "); got != "fix-my-feature" {
		t.Fatalf("suggestion = %q, want fix-my-feature", got)
	}
}

func TestDisplayNameFallbacks(t *testing.T) {
	if got := DisplayName(serverapi.WorktreeView{CanonicalRoot: "/tmp/worktree-name", WorktreeID: "wt-1"}); got != "worktree-name" {
		t.Fatalf("display name = %q, want worktree-name", got)
	}
}

func TestDeleteCanAutoDeleteBranchRequiresManagedCreatedBranch(t *testing.T) {
	if !DeleteCanAutoDeleteBranch(serverapi.WorktreeView{BuilderManaged: true, CreatedBranch: true, BranchName: "feature"}) {
		t.Fatal("expected auto delete branch")
	}
	if DeleteCanAutoDeleteBranch(serverapi.WorktreeView{BuilderManaged: true, CreatedBranch: false, BranchName: "feature"}) {
		t.Fatal("did not expect auto delete branch")
	}
}
