package runtime

import (
	"context"
	"sync"
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/server/workflowruntime"
	"core/shared/config"
	"core/shared/toolspec"
)

func TestActiveWorkflowManualCompactionRestoresCurrentNodeBeforeNextRequest(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"native", "local"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			started, release := make(chan struct{}), make(chan struct{})
			releaseTool := sync.OnceFunc(func() { close(release) })
			defer releaseTool()
			client := &fakeCompactionClient{
				responses: []llm.Response{
					commentaryResponse("", llm.ToolCall{
						ID: "work", Name: string(toolspec.ToolExecCommand),
						Input: mustJSON(map[string]any{"cmd": "pwd"}),
					}),
				},
				compactionResponses: []llm.CompactionResponse{
					remoteCompactionReplacement(1_000, 100, 200_000),
				},
			}
			if mode == "local" {
				client.responses = append(client.responses, finalOutputItemResponse("work completed; finish the current node"))
			}
			client.responses = append(client.responses, commentaryResponse("", completeNodeCall(
				"complete", mustJSON(map[string]any{"transition": "done", "summary": "done"}),
			)))
			controller := &fakeWorkflowController{}
			workflowConfig := testWorkflowConfig(controller, config.WorkflowCompletionModeTool)
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t, tools.HandlerRegistration{
				ID: toolspec.ToolExecCommand,
				Handler: blockingTool{
					name: toolspec.ToolExecCommand, started: started, release: release,
				},
			}), Config{
				Model: "gpt-6-sol", CompactionMode: mode, Debug: true,
				EnabledTools: []toolspec.ID{toolspec.ToolExecCommand},
			})
			publishTestWorkflowExecution(t, engine, workflowConfig)
			ctx, cancel := context.WithTimeout(t.Context(), runtimeTestSynchronizationTimeout)
			defer cancel()
			type outcome struct {
				err   error
				panic any
			}
			done := make(chan outcome, 1)
			go func() {
				result := outcome{}
				defer func() {
					result.panic = recover()
					done <- result
				}()
				_, result.err = engine.SubmitWorkflowTurn(ctx)
			}()
			select {
			case <-started:
			case result := <-done:
				t.Fatalf("workflow exited before running its tool: error=%v panic=%v", result.err, result.panic)
			case <-ctx.Done():
				t.Fatal("timed out waiting for workflow tool")
			}
			if err := engine.CompactContext(ctx, ""); err != nil {
				t.Fatalf("admit manual compaction during active workflow: %v", err)
			}
			releaseTool()
			select {
			case result := <-done:
				if result.panic != nil {
					t.Fatalf("workflow panicked after manual compaction: %v", result.panic)
				}
				if result.err != nil {
					t.Fatalf("workflow failed after manual compaction: %v", result.err)
				}
			case <-ctx.Done():
				t.Fatal("timed out waiting for workflow completion after manual compaction")
			}
			if got := controller.completed.Load(); got != 1 {
				t.Fatalf("completed nodes = %d, want one", got)
			}
			if got := engine.CompactionCount(); got != 1 {
				t.Fatalf("completed compactions = %d, want one", got)
			}
			client.mu.Lock()
			defer client.mu.Unlock()
			wantCalls, wantRemoteCalls := 2, 1
			if mode == "local" {
				wantCalls, wantRemoteCalls = 3, 0
			}
			if len(client.calls) != wantCalls || len(client.compactionCalls) != wantRemoteCalls {
				t.Fatalf("generation/remote compaction calls = %d/%d, want %d/%d",
					len(client.calls), len(client.compactionCalls), wantCalls, wantRemoteCalls)
			}
			request := client.calls[len(client.calls)-1]
			identity := workflowruntime.CurrentNodePromptIdentity(workflowConfig.Instructions.CurrentNode)
			workflowModes, summaries := 0, 0
			for _, item := range request.Items {
				if item.Type == llm.ResponseItemTypeCompaction ||
					item.MessageType != nil && *item.MessageType == llm.MessageTypeCompactionSummary {
					summaries++
				}
				if item.Type != llm.ResponseItemTypeMessage ||
					item.MessageType == nil || *item.MessageType != llm.MessageTypeWorkflowMode {
					continue
				}
				workflowModes++
				if item.SourcePath == nil || *item.SourcePath != identity {
					t.Fatalf("restored workflow prompt has wrong current node identity: %+v", item.SourcePath)
				}
			}
			if workflowModes != 1 || summaries != 1 {
				t.Fatalf("post-compaction workflow prompts/summaries = %d/%d, want one/one", workflowModes, summaries)
			}
		})
	}
}
