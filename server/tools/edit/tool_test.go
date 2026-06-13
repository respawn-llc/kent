package edit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"core/server/tools"
	"core/server/tools/fsguard"
	"core/shared/toolspec"
)

func TestCreateMissingFileReturnsJSONStringAndDiff(t *testing.T) {
	dir := t.TempDir()
	tool := newTestTool(t, dir)

	result := callEdit(t, tool, map[string]any{
		"path":       "nested/a.txt",
		"old_string": "",
		"new_string": "hello\n",
	})

	if result.IsError {
		t.Fatalf("expected success, got %s", string(result.Output))
	}
	assertJSONText(t, result.Output, "ok")
	got, err := os.ReadFile(filepath.Join(dir, "nested", "a.txt"))
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("created content = %q", string(got))
	}
	if result.Presentation == nil || result.Presentation.PatchRender == nil {
		t.Fatalf("expected result diff metadata, got %+v", result.Presentation)
	}
	if summary := result.Presentation.PatchRender.SummaryText(); !strings.Contains(summary, "nested/a.txt") || !strings.Contains(summary, "+1") {
		t.Fatalf("unexpected diff summary: %q", summary)
	}
}

func TestExactReplaceAndReplaceAll(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(target, []byte("one two one\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	tool := newTestTool(t, dir)

	first := callEdit(t, tool, map[string]any{
		"path":       "a.txt",
		"old_string": "one",
		"new_string": "ONE",
	})
	if !first.IsError || !strings.Contains(toolResultText(t, first), "matched 2 occurrences") {
		t.Fatalf("expected multiple occurrence failure, got %+v text=%q", first, toolResultText(t, first))
	}

	second := callEdit(t, tool, map[string]any{
		"path":        "a.txt",
		"old_string":  "one",
		"new_string":  "ONE",
		"replace_all": true,
	})
	if second.IsError {
		t.Fatalf("expected success, got %s", string(second.Output))
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(data) != "ONE two ONE\n" {
		t.Fatalf("edited content = %q", string(data))
	}
}

func TestInputAliasesAndConflicts(t *testing.T) {
	dir := t.TempDir()
	tool := newTestTool(t, dir)

	ok := callEdit(t, tool, map[string]any{
		"filePath":   "a.txt",
		"oldText":    "",
		"newText":    "hello",
		"replaceAll": true,
	})
	if ok.IsError {
		t.Fatalf("expected alias success, got %s", string(ok.Output))
	}

	conflict := callEdit(t, tool, map[string]any{
		"path":      "a.txt",
		"file_path": "b.txt",
		"oldText":   "",
		"newText":   "hello",
	})
	if !conflict.IsError || !strings.Contains(toolResultText(t, conflict), "conflicting aliases") {
		t.Fatalf("expected conflict failure, got %q", toolResultText(t, conflict))
	}
}

func TestCreateRejectsNonEmptyAndAllowsWhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	nonEmpty := filepath.Join(dir, "non-empty.txt")
	if err := os.WriteFile(nonEmpty, []byte("already\n"), 0o644); err != nil {
		t.Fatalf("seed non-empty: %v", err)
	}
	blank := filepath.Join(dir, "blank.txt")
	if err := os.WriteFile(blank, []byte("  \n\t"), 0o644); err != nil {
		t.Fatalf("seed blank: %v", err)
	}
	tool := newTestTool(t, dir)

	rejected := callEdit(t, tool, map[string]any{"path": "non-empty.txt", "old_string": "", "new_string": "new"})
	if !rejected.IsError || !strings.Contains(toolResultText(t, rejected), "already contains text") {
		t.Fatalf("expected non-empty rejection, got %q", toolResultText(t, rejected))
	}
	allowed := callEdit(t, tool, map[string]any{"path": "blank.txt", "old_string": "", "new_string": "new"})
	if allowed.IsError {
		t.Fatalf("expected blank replacement success, got %s", string(allowed.Output))
	}
	got, err := os.ReadFile(blank)
	if err != nil {
		t.Fatalf("read blank replacement: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("blank replacement = %q", string(got))
	}
}

func TestEncodingAndBinaryGuards(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nul.txt"), []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatalf("seed nul: %v", err)
	}
	tool := newTestTool(t, dir)

	nul := callEdit(t, tool, map[string]any{"path": "nul.txt", "old_string": "a", "new_string": "b"})
	if !nul.IsError || !strings.Contains(toolResultText(t, nul), "binary file rejected") {
		t.Fatalf("expected binary rejection, got %q", toolResultText(t, nul))
	}
	png := callEdit(t, tool, map[string]any{"path": "image.png", "old_string": "", "new_string": "text"})
	if !png.IsError || !strings.Contains(toolResultText(t, png), "binary file extension") {
		t.Fatalf("expected extension rejection, got %q", toolResultText(t, png))
	}
}

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
	if !result.IsError || !strings.Contains(toolResultText(t, result), "matched 0 occurrences") {
		t.Fatalf("expected 0-match failure, got %+v text=%q", result, toolResultText(t, result))
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
	tool := newTestTool(t, workspace, WithOutsideWorkspaceApprover(func(context.Context, fsguard.Request) (fsguard.Approval, error) {
		prompts++
		return fsguard.Approval{Decision: fsguard.DecisionAllowOnce}, nil
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
	tool := newTestTool(t, workspace, WithOutsideWorkspaceApprover(func(context.Context, fsguard.Request) (fsguard.Approval, error) {
		prompts++
		return fsguard.Approval{Decision: fsguard.DecisionAllowOnce}, nil
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
	tool := newTestTool(t, workspace, WithOutsideWorkspaceApprover(func(context.Context, fsguard.Request) (fsguard.Approval, error) {
		prompts++
		return fsguard.Approval{Decision: fsguard.DecisionAllowOnce}, nil
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

func toolResultText(t *testing.T, result tools.Result) string {
	t.Helper()
	var text string
	if err := json.Unmarshal(result.Output, &text); err != nil {
		t.Fatalf("decode result output: %v", err)
	}
	return text
}

func assertJSONText(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	got := toolResultText(t, tools.Result{Output: raw})
	if got != want {
		t.Fatalf("result output = %q, want %q", got, want)
	}
}
