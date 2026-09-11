package runtime

import (
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/shared/textutil"
)

func TestIndependentSessionsAdoptIndependentThinking(t *testing.T) {
	for _, effort := range []string{"high", "low"} {
		client := &fakeClient{
			caps:      llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: true},
			responses: []llm.Response{finalOutputItemResponse("done")},
		}
		store := mustCreateTestSession(t)
		engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-6-astra", ThinkingLevel: effort})
		if _, err := engine.SubmitUserMessage(t.Context(), "independent input"); err != nil {
			t.Fatal(err)
		}
		if store.Meta().OriginalThinkingEffort == nil || *store.Meta().OriginalThinkingEffort != effort ||
			client.calls[0].ReasoningEffort != effort {
			t.Fatal("independent Session inherited another baseline")
		}
	}
}

func TestCompactionReestablishesThinking(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		t.Run(mode, func(t *testing.T) {
			client := &fakeCompactionClient{
				caps:      llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsResponsesCompact: true, SupportsNativeThinkingUpdates: true},
				responses: []llm.Response{finalOutputItemResponse("seed"), finalOutputItemResponse("next"), finalOutputItemResponse("last")},
				compactionResponses: []llm.CompactionResponse{{
					Checkpoint: llm.ResponseItem{Type: llm.ResponseItemTypeCompaction, EncryptedContent: textutil.Value("checkpoint")},
					Usage:      llm.Usage{InputTokens: 100, OutputTokens: 10, WindowTokens: 200000},
				}},
			}
			if mode == "local" {
				client.responses = append([]llm.Response{finalOutputItemResponse("seed"), finalOutputItemResponse("summary")}, client.responses[1:]...)
			}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
				Model: "gpt-6-astra", ThinkingLevel: "high", CompactionMode: mode,
				Reviewer: ReviewerConfig{Model: "gpt-6-astra", ThinkingLevel: "medium"},
			})
			if _, err := engine.SubmitUserMessage(t.Context(), "seed"); err != nil {
				t.Fatal(err)
			}
			if err := engine.SetThinkingLevel(t.Context(), "low"); err != nil {
				t.Fatal(err)
			}
			scheduleManualCompactionAndWait(t, engine)
			if _, err := engine.SubmitUserMessage(t.Context(), "after compaction"); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.SubmitUserMessage(t.Context(), "subsequent"); err != nil {
				t.Fatal(err)
			}
			compacted := client.compactionCalls
			if mode == "local" {
				compacted = client.calls[1:2]
			}
			if len(compacted) != 1 || compacted[0].ReasoningEffort != "high" {
				t.Fatalf("compaction requests = %d, expected one with original effort", len(compacted))
			}
			for _, request := range []llm.Request{compacted[0], client.calls[len(client.calls)-2], client.calls[len(client.calls)-1]} {
				count := 0
				for _, item := range request.Items {
					if item.Type == llm.ResponseItemTypeConfigurationUpdate {
						count++
						if item.ConfigurationEffort == nil || *item.ConfigurationEffort != "low" {
							t.Fatal("compaction used stale desired effort")
						}
					}
				}
				if request.ReasoningEffort != "high" || count != 1 {
					t.Fatalf("request effort=%s updates=%d", request.ReasoningEffort, count)
				}
			}
			reviewer, err := engine.buildReviewerRequest(t.Context(), newObservedModelClient(client))
			if err != nil {
				t.Fatal(err)
			}
			if reviewer.ReasoningEffort != "medium" || engine.ThinkingLevel() != "low" {
				t.Fatal("reviewer changed independent Thinking")
			}
			for _, item := range reviewer.Items {
				if item.Type == llm.ResponseItemTypeConfigurationUpdate {
					t.Fatal("main configuration entered Reviewer input")
				}
			}
		})
	}
}
