package runtime

import (
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestCompactionRefreshesFullContract(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		t.Run(mode, func(t *testing.T) {
			store := mustCreateTestSession(t)
			reloader := &mutablePromptFacingSnapshotReloader{}
			if err := store.MarkModelDispatchLocked(session.LockedContract{
				Model: "gpt-6-astra", Temperature: 0.5, MaxOutputToken: 100,
				ModelCapabilities: session.LockedModelCapabilities{SupportsReasoningEffort: true},
				EnabledTools:      []string{string(toolspec.ToolViewImage)}, HasEnabledTools: true,
				SystemPrompt: "outgoing context", HasSystemPrompt: true,
				ReviewerPrompt: "outgoing reviewer", HasReviewerPrompt: true,
			}); err != nil {
				t.Fatal(err)
			}
			client := &fakeCompactionClient{
				caps: llm.ProviderCapabilities{
					ProviderID: "openai", SupportsResponsesAPI: true,
					SupportsResponsesCompact: true, SupportsNativeThinkingUpdates: true,
				},
				responses: []llm.Response{finalOutputItemResponse("seed"), finalOutputItemResponse("next")},
				compactionResponses: []llm.CompactionResponse{{
					Checkpoint: llm.ResponseItem{Type: llm.ResponseItemTypeCompaction, EncryptedContent: textutil.Value("checkpoint")},
				}},
			}
			if mode == "local" {
				client.responses = []llm.Response{finalOutputItemResponse("seed"), finalOutputItemResponse("summary"), finalOutputItemResponse("next")}
			}
			engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t, tools.HandlerRegistration{
				ID: toolspec.ToolViewImage, Handler: fakeTool{name: toolspec.ToolViewImage},
			}), Config{
				Model: "gpt-6-astra", ThinkingLevel: "medium", CompactionMode: mode,
				Temperature: 1, MaxTokens: 200, EnabledTools: []toolspec.ID{toolspec.ToolViewImage},
				PromptFacingSnapshotReloader: reloader,
			})
			if _, err := engine.SubmitUserMessage(t.Context(), "seed"); err != nil {
				t.Fatal(err)
			}
			if len(client.calls[0].Tools) != 0 {
				t.Fatal("existing context gained vision before compaction")
			}
			reloader.settings.ToolPreambles = true
			scheduleManualCompactionAndWait(t, engine)
			if store.Meta().Locked != nil {
				t.Fatal("committed compaction retained part of the contract")
			}
			if reopened := mustOpenTestSession(t, store.Dir()); reopened.Meta().Locked != nil {
				t.Fatal("committed compaction retained a durable contract")
			}
			if _, err := engine.SubmitUserMessage(t.Context(), "after compaction"); err != nil {
				t.Fatal(err)
			}
			locked := store.Meta().Locked
			if locked == nil || !locked.ModelCapabilities.SupportsVisionInputs ||
				locked.Temperature != 1 || locked.MaxOutputToken != 200 || locked.HasReviewerPrompt ||
				locked.ToolPreambles == nil || !*locked.ToolPreambles {
				t.Fatalf("next request did not establish a fresh full contract: %+v", locked)
			}
			request := client.calls[len(client.calls)-1]
			if len(request.Tools) != 1 || request.Tools[0].Name != string(toolspec.ToolViewImage) {
				t.Fatalf("next context did not advertise view_image: %+v", request.Tools)
			}
		})
	}
}

func TestRepeatedManualCompactionLeavesSummaryPending(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		t.Run(mode, func(t *testing.T) {
			store := mustCreateTestSession(t)
			client := &fakeCompactionClient{
				caps: llm.ProviderCapabilities{
					ProviderID: "openai", SupportsResponsesAPI: true,
					SupportsResponsesCompact: true,
				},
				responses: []llm.Response{
					finalOutputItemResponse("seed"), finalOutputItemResponse("summary"),
					finalOutputItemResponse("next"), finalOutputItemResponse("next summary"),
				},
				compactionResponses: []llm.CompactionResponse{{
					Checkpoint: llm.ResponseItem{Type: llm.ResponseItemTypeCompaction, EncryptedContent: textutil.Value("checkpoint")},
				}, {
					Checkpoint: llm.ResponseItem{Type: llm.ResponseItemTypeCompaction, EncryptedContent: textutil.Value("next checkpoint")},
				}},
			}
			engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
				Model: "gpt-6-astra", CompactionMode: mode,
			})
			if _, err := engine.SubmitUserMessage(t.Context(), "seed"); err != nil {
				t.Fatal(err)
			}
			scheduleManualCompactionAndWait(t, engine)
			calls, compactCalls := len(client.calls), len(client.compactionCalls)
			count := engine.CompactionCount()
			for range 2 {
				scheduleManualCompactionAndWait(t, engine)
			}
			if len(client.calls) != calls || len(client.compactionCalls) != compactCalls {
				t.Fatal("repeated compaction dispatched a provider request")
			}
			if store.Meta().Locked != nil {
				t.Fatal("repeated compaction prepared the next generation contract")
			}
			if engine.CompactionCount() != count {
				t.Fatal("repeated compaction replaced the saved summary")
			}
			reopened := mustOpenTestSession(t, store.Dir())
			resumed := mustNewTestEngine(t, reopened, client, tools.NewRegistry(), Config{
				Model: "gpt-6-astra", CompactionMode: mode,
			})
			scheduleManualCompactionAndWait(t, resumed)
			if len(client.calls) != calls || len(client.compactionCalls) != compactCalls ||
				resumed.CompactionCount() != count || reopened.Meta().Locked != nil {
				t.Fatal("compaction after reopening changed the pending summary or prepared its context")
			}
			if _, err := resumed.SubmitUserMessage(t.Context(), "continue"); err != nil {
				t.Fatal(err)
			}
			scheduleManualCompactionAndWait(t, resumed)
			if resumed.CompactionCount() != count+1 {
				t.Fatal("compaction after a new model turn did not produce a new summary")
			}
		})
	}
}
