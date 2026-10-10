package app

import (
	"path/filepath"
	"strings"
	"testing"

	"core/cli/app/internal/worktreeui"
	"core/shared/clientui"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
)

func TestWorktreeEntryPathDisplay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	item := worktreeui.Item{DisplayName: "feature", CanonicalRoot: filepath.Join(home, "worktree")}
	lines := renderWorktreeEntry(item, false, 100, "dark", uiStyles{})
	got := stripANSIAndTrimRight(strings.Join(lines, "\n"))
	displayItem := item
	displayItem.CanonicalRoot = "~/worktree"
	want := stripANSIAndTrimRight(strings.Join(renderWorktreeEntry(displayItem, false, 100, "dark", uiStyles{}), "\n"))
	if got != want {
		t.Fatalf("worktree path display = %q, want %q", got, want)
	}
}

func TestApprovalPathDisplayPreservesCanonicalTargets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	requested := filepath.Join(home, "alias")
	resolved := filepath.Join(home, "file")
	event := testApprovalAskEvent("approval-path-display", "", clientui.ApprovalDecisionAllowOnce, clientui.ApprovalDecisionDeny)
	event.prompt.GetApproval().AccessTargets = []*promptpb.FileAccessTarget{{
		RequestedPath: requested, ResolvedPath: resolved,
	}}
	m := sizedTestUIModel(newProjectedStaticUIModel(), 100, 24)
	next, projection := m.Update(askEventMsg{event: event})
	viewModel := updateUIModel(t, next.(*uiModel), projection())
	identity, ok := viewModel.currentQuestionRenderIdentity()
	want := clientui.FormatFileAccessApprovalMarkdown([]clientui.FileAccessTarget{{
		RequestedPath: "~/alias", ResolvedPath: "~/file",
	}})
	if !ok || identity.questionSource != want {
		t.Fatalf("approval path display = %q, want %q", identity.questionSource, want)
	}
	target := viewModel.ask.current.prompt.GetApproval().AccessTargets[0]
	if target.RequestedPath != requested || target.ResolvedPath != resolved {
		t.Fatal("display changed canonical approval paths")
	}
}

func TestStatusPathDisplayBoundaries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "project")
	for _, test := range []struct{ path, workdir, want string }{
		{cwd, cwd, "./"},
		{filepath.Join(cwd, "file"), cwd, "./file"},
		{filepath.Join(home, "elsewhere"), cwd, "~/elsewhere"},
		{filepath.Join(home, "file"), "", "~/file"},
		{home + "-other/file", cwd, home + "-other/file"},
	} {
		if got := statusDisplayPath(test.path, test.workdir); got != test.want {
			t.Fatalf("status path = %q, want %q", got, test.want)
		}
	}
}

func TestWorkspaceChangePathDisplay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	selected := filepath.Join(home, "old")
	current := filepath.Join(home, "new")
	model := newWorkspaceChangePromptModel(selected, current, "dark")
	displayModel := newWorkspaceChangePromptModel("~/old", "~/new", "dark")
	if got, want := model.promptLines()[0].Text, displayModel.promptLines()[0].Text; got != want {
		t.Fatalf("workspace change path display = %q, want %q", got, want)
	}
	if model.selectedRoot != selected || model.currentRoot != current {
		t.Fatal("display changed canonical workspace roots")
	}
}

func TestWorktreeSetupProgressPathDisplay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	script := filepath.Join(home, "setup.sh")
	root := filepath.Join(home, "feature")
	model := sizedTestUIModel(newProjectedStaticUIModel(), 120, 50)
	model.worktrees.create.submitting = true
	model.worktrees.create.setupEvent = &worktreepb.SetupEvent{
		Phase: &worktreepb.SetupEvent_Started{Started: &worktreepb.SetupStarted{
			ScriptPath: script, WorktreeRoot: root,
		}},
	}
	layout := uiViewLayout{model: model}
	got := stripANSIAndTrimRight(strings.Join(layout.renderWorktreeCreateDialog(120, 50, uiStyles{}), "\n"))
	started := model.worktrees.create.setupEvent.GetStarted()
	if started.ScriptPath != script || started.WorktreeRoot != root {
		t.Fatal("display changed canonical setup paths")
	}
	model.worktrees.create.setupEvent = &worktreepb.SetupEvent{
		Phase: &worktreepb.SetupEvent_Started{Started: &worktreepb.SetupStarted{
			ScriptPath: "~/setup.sh", WorktreeRoot: "~/feature",
		}},
	}
	want := stripANSIAndTrimRight(strings.Join(layout.renderWorktreeCreateDialog(120, 50, uiStyles{}), "\n"))
	if got != want {
		t.Fatalf("setup path display = %q, want %q", got, want)
	}
}

func TestWorktreeDeletePreviewPathDisplay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "feature")
	model := sizedTestUIModel(newProjectedStaticUIModel(), 120, 50)
	model.worktrees.deleteConfirm.target = worktreeui.Item{DisplayName: "feature", CanonicalRoot: root}
	layout := uiViewLayout{model: model}
	got := stripANSIAndTrimRight(strings.Join(layout.renderWorktreeDeleteDialog(120, 50, uiStyles{}), "\n"))
	if model.worktrees.deleteConfirm.target.CanonicalRoot != root {
		t.Fatal("display changed canonical deletion target")
	}
	model.worktrees.deleteConfirm.target.CanonicalRoot = "~/feature"
	want := stripANSIAndTrimRight(strings.Join(layout.renderWorktreeDeleteDialog(120, 50, uiStyles{}), "\n"))
	if got != want {
		t.Fatalf("delete path display = %q, want %q", got, want)
	}
}
