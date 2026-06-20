package edit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"core/server/tools"
	"core/shared/toolspec"
)

func TestDeletionIncludesFollowingNewlineAfterUniqueness(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(target, []byte("before\nremove me\nafter\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	tool := newTestTool(t, dir)

	result := callEdit(t, tool, map[string]any{"path": "a.txt", "old_string": "remove me", "new_string": ""})
	if result.IsError {
		t.Fatalf("expected success, got %s", string(result.Output))
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(got) != "before\nafter\n" {
		t.Fatalf("deleted content = %q", string(got))
	}
}

func TestContextAwareFallbackRejectsCommonMiddleLineWithoutBoundaryMatch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	original := "alpha\nTODO\nomega\n"
	if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	tool := newTestTool(t, dir)

	result := callEdit(t, tool, map[string]any{
		"path":       "a.txt",
		"old_string": "before\nTODO\nafter\n",
		"new_string": "changed\n",
	})
	if !result.IsError {
		t.Fatalf("expected 0-match failure, got %+v", result)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != original {
		t.Fatalf("file was unexpectedly changed: %q", string(got))
	}
}

func TestContextAwareFallbackRejectsMismatchedInteriorLines(t *testing.T) {
	content := "header\nkeep one\nTODO\nkeep two\nfooter\n"
	old := "header\nwant one\nTODO\nwant two\nfooter\n"

	matches := contextAwareMatches(content, old)
	if len(matches) != 0 {
		t.Fatalf("context-aware matches = %+v, want none", matches)
	}
}

func TestContextAwareFallbackAcceptsNormalizedBoundaryAndMiddleLines(t *testing.T) {
	content := "alpha   beta\nTODO item\nomega   tail\n"
	old := "alpha beta\nTODO item\nomega tail\n"

	matches := contextAwareMatches(content, old)
	if len(matches) != 1 {
		t.Fatalf("context-aware matches = %d, want 1", len(matches))
	}
	if matches[0].actual != content {
		t.Fatalf("matched actual = %q, want %q", matches[0].actual, content)
	}
}

func TestPreserveCurlyQuotesKeepsOpeningSingleQuote(t *testing.T) {
	got := preserveCurlyQuotes("‘old’", "'new'")
	if got != "‘new’" {
		t.Fatalf("preserved quote replacement = %q, want %q", got, "‘new’")
	}
}

func TestOutsideWorkspaceAncestorAliasUsesSingleCallApproval(t *testing.T) {
	workspace := t.TempDir()
	outside := newNonTemporaryOutsideDir(t)
	targetDir := filepath.Join(outside, "target")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("create outside target dir: %v", err)
	}
	target := filepath.Join(targetDir, "target.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("seed outside target: %v", err)
	}
	alias := filepath.Join(outside, "alias")
	if err := os.Symlink(targetDir, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	prompts := 0
	tool := newTestTool(t, workspace, WithOutsideWorkspaceApprover(func(context.Context, tools.FSGuardRequest) (tools.FSGuardApproval, error) {
		prompts++
		return tools.FSGuardApproval{Decision: tools.FSGuardDecisionAllowOnce}, nil
	}))

	result := callEdit(t, tool, map[string]any{"path": filepath.Join(alias, "target.txt"), "old_string": "old", "new_string": "new"})
	if result.IsError {
		t.Fatalf("expected success, got %s", string(result.Output))
	}
	if prompts != 1 {
		t.Fatalf("outside approval prompts = %d, want 1", prompts)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "new\n" {
		t.Fatalf("target content = %q", string(got))
	}
}

func TestOutsideWorkspaceMissingAncestorAliasUsesSingleCallApproval(t *testing.T) {
	workspace := t.TempDir()
	outside := newNonTemporaryOutsideDir(t)
	targetDir := filepath.Join(outside, "target")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("create outside target dir: %v", err)
	}
	alias := filepath.Join(outside, "alias")
	if err := os.Symlink(targetDir, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	prompts := 0
	tool := newTestTool(t, workspace, WithOutsideWorkspaceApprover(func(context.Context, tools.FSGuardRequest) (tools.FSGuardApproval, error) {
		prompts++
		return tools.FSGuardApproval{Decision: tools.FSGuardDecisionAllowOnce}, nil
	}))

	result := callEdit(t, tool, map[string]any{"path": filepath.Join(alias, "new.txt"), "old_string": "", "new_string": "new\n"})
	if result.IsError {
		t.Fatalf("expected success, got %s", string(result.Output))
	}
	if prompts != 1 {
		t.Fatalf("outside approval prompts = %d, want 1", prompts)
	}
	got, err := os.ReadFile(filepath.Join(targetDir, "new.txt"))
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "new\n" {
		t.Fatalf("target content = %q", string(got))
	}
}

func TestOutsideWorkspaceFinalSymlinkRequiresRealPathApproval(t *testing.T) {
	workspace := t.TempDir()
	outside := newNonTemporaryOutsideDir(t)
	target := filepath.Join(outside, "target.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("seed outside target: %v", err)
	}
	link := filepath.Join(outside, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	prompts := 0
	tool := newTestTool(t, workspace, WithOutsideWorkspaceApprover(func(context.Context, tools.FSGuardRequest) (tools.FSGuardApproval, error) {
		prompts++
		return tools.FSGuardApproval{Decision: tools.FSGuardDecisionAllowOnce}, nil
	}))

	result := callEdit(t, tool, map[string]any{"path": link, "old_string": "old", "new_string": "new"})
	if result.IsError {
		t.Fatalf("expected success, got %s", string(result.Output))
	}
	if prompts != 2 {
		t.Fatalf("outside approval prompts = %d, want 2", prompts)
	}
}

func newNonTemporaryOutsideDir(t *testing.T) string {
	t.Helper()
	outside, err := os.MkdirTemp(".", "edit-outside-approval-")
	if err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	outside, err = filepath.Abs(outside)
	if err != nil {
		t.Fatalf("resolve outside dir: %v", err)
	}
	if filepath.IsAbs(outside) && strings.Contains(outside, string(filepath.Separator)+"tmp"+string(filepath.Separator)) {
		t.Skip("test outside dir is under temporary editable root")
	}
	return outside
}

func newTestTool(t *testing.T, dir string, opts ...Option) *Tool {
	t.Helper()
	tool, err := New(dir, true, opts...)
	if err != nil {
		t.Fatalf("new edit tool: %v", err)
	}
	return tool
}

func callEdit(t *testing.T, tool *Tool, payload map[string]any) tools.Result {
	t.Helper()
	input, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	result, err := tool.Call(context.Background(), tools.Call{ID: "call", Name: toolspec.ToolEdit, Input: input})
	if err != nil {
		t.Fatalf("edit call error: %v", err)
	}
	return result
}
