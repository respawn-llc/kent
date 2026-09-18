package workflowrunner

import (
	"testing"

	"core/prompts"
	"core/server/llm"
)

func TestManagedWorkflowFirstRequestIncludesWorktreeContext(t *testing.T) {
	previous := prompts.WorktreeInitialPrompt
	prompts.WorktreeInitialPrompt = "{{branch}}"
	t.Cleanup(func() { prompts.WorktreeInitialPrompt = previous })
	f := newCurrentNodeRunnerFixture(t, ScriptedFinalAnswer(`{"commentary":"done"}`))
	task := f.createTask(t, createCurrentNodeAgentWorkflow(t, f.store))
	candidate, _ := prepareMissingWorktreeCompletion(t, f, task)
	startManagedCompletionTask(t, f, task, candidate)
	request := f.waitForModelRequests(t, 1)[0]
	for _, item := range request.Items {
		if item.MessageType == nil || *item.MessageType != llm.MessageTypeWorktreeMode {
			continue
		}
		context := item.WorktreeContext
		if context == nil || context.WorktreePath != candidate.Root.Managed.Root ||
			context.WorkspaceRoot != f.workspace || context.EffectiveCwd != candidate.Root.Managed.Root ||
			context.Branch == nil {
			t.Fatalf("incorrect initial Worktree context: %+v", context)
		}
		if item.Content == nil || *item.Content != *context.Branch {
			t.Fatalf("first workflow request did not use the initial Worktree template: content=%v item=%+v", item.Content, item)
		}
		return
	}
	t.Fatal("first workflow model request omitted Worktree context")
}
