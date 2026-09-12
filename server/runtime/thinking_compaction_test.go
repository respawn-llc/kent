package runtime

import (
	"bytes"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/textutil"
)

func TestLocalCompactionKeepsCommittedThinkingAcrossToolRetry(t *testing.T) {
	for _, adjacent := range []bool{false, true} {
		name := "new update"
		if adjacent {
			name = "deferred adjacent update"
		}
		t.Run(name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			seed := mustNewTestEngine(t, store, &fakeClient{responses: []llm.Response{finalOutputItemResponse("seed")}}, tools.NewRegistry(), Config{
				Model: "gpt-6-astra", ThinkingLevel: "high",
			})
			if _, err := seed.SubmitUserMessage(t.Context(), "seed"); err != nil {
				t.Fatal(err)
			}
			if err := seed.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.AdoptOriginalThinkingEffort("high"); err != nil {
				t.Fatal(err)
			}
			log := mustMaterializeTestEventLog(t, store)
			if adjacent {
				item := llm.PrepareOpenAIInputItems([]llm.ResponseItem{{
					Type: llm.ResponseItemTypeConfigurationUpdate, ConfigurationEffort: textutil.Value("medium"),
				}})[0]
				history, err := sessionProviderHistoryItemFromLLM(0, item)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := log.AppendRecord(textutil.Value("prior-step"), session.ConfigurationUpdateRecord{Item: history}); err != nil {
					t.Fatal(err)
				}
			}
			client := &hookClient{
				caps: llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: true},
				response: llm.Response{ToolCalls: []llm.ToolCall{{
					ID: "rejected-tool", Name: "tool", Input: []byte(`{}`),
				}}},
			}
			var persisted []session.ProviderHistoryItem
			client.beforeReturn = func() error {
				client.mu.Lock()
				call := len(client.calls)
				client.response = finalOutputItemResponse("summary")
				client.mu.Unlock()
				if call == 1 {
					persisted = collectThinkingForkItems(t, mustMaterializeTestEventLog(t, store))
				}
				return nil
			}
			engine := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{
				Model: "gpt-6-astra", ThinkingLevel: "low", CompactionMode: "local",
			})
			scheduleManualCompactionAndWait(t, engine)
			if engine.store.Meta().OriginalThinkingEffort != nil {
				t.Fatal("successful compaction retained the previous context's Thinking baseline")
			}
			if len(client.calls) != 2 {
				t.Fatalf("local summary requests = %d, want two", len(client.calls))
			}
			for _, request := range client.calls {
				count := 0
				for _, item := range request.Items {
					if item.Type == llm.ResponseItemTypeConfigurationUpdate {
						if count >= len(persisted) || !bytes.Equal(item.Raw, persisted[count].Raw) {
							t.Fatal("local compaction dispatched an unpersisted Thinking update")
						}
						count++
					}
				}
				if request.ReasoningEffort != "high" || count != 1 {
					t.Fatalf("local summary effort=%q updates=%d", request.ReasoningEffort, count)
				}
			}
			first, retry := client.calls[0], client.calls[1]
			for i, item := range first.Items {
				if i >= len(retry.Items) || !bytes.Equal(item.Raw, retry.Items[i].Raw) {
					t.Fatal("local tool retry changed an already dispatched input position")
				}
			}
		})
	}
}

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
			for index, request := range []llm.Request{compacted[0], client.calls[len(client.calls)-2], client.calls[len(client.calls)-1]} {
				count := 0
				for _, item := range request.Items {
					if item.Type == llm.ResponseItemTypeConfigurationUpdate {
						count++
						if item.ConfigurationEffort == nil || *item.ConfigurationEffort != "low" {
							t.Fatal("compaction used stale desired effort")
						}
					}
				}
				wantEffort := "low"
				if index == 0 {
					wantEffort = "high"
				}
				if request.ReasoningEffort != wantEffort || count != 1 {
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

func TestUnsupportedThinkingCompactionIgnoresNativeBaseline(t *testing.T) {
	for _, tc := range []struct {
		name     string
		model    string
		mode     string
		provider string
		native   bool
	}{
		{name: "older model native compaction", model: "gpt-5", mode: "native", provider: "openai", native: true},
		{name: "older model local compaction", model: "gpt-5", mode: "local", provider: "openai", native: true},
		{name: "Astra custom endpoint local compaction", model: "gpt-6-astra", mode: "local", provider: "openai-compatible"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			if err := store.AdoptOriginalThinkingEffort("medium"); err != nil {
				t.Fatal(err)
			}
			client := &fakeCompactionClient{
				caps: llm.ProviderCapabilities{
					ProviderID: tc.provider, SupportsResponsesAPI: true,
					SupportsResponsesCompact: tc.provider == "openai", SupportsNativeThinkingUpdates: tc.native,
				},
				responses:           []llm.Response{finalOutputItemResponse("seed"), finalOutputItemResponse("after")},
				compactionResponses: []llm.CompactionResponse{remoteCompactionReplacement(100, 10, 200000)},
			}
			if tc.mode == "local" {
				client.responses = []llm.Response{finalOutputItemResponse("seed"), finalOutputItemResponse("summary"), finalOutputItemResponse("after")}
			}
			engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
				Model: tc.model, ThinkingLevel: "medium", CompactionMode: tc.mode,
			})
			if _, err := engine.SubmitUserMessage(t.Context(), "seed"); err != nil {
				t.Fatal(err)
			}
			if err := engine.SetThinkingLevel(t.Context(), "high"); err != nil {
				t.Fatal(err)
			}
			if store.Meta().OriginalThinkingEffort == nil || *store.Meta().OriginalThinkingEffort != "medium" {
				t.Fatal("test did not retain the deliberately stale baseline before compaction")
			}
			if _, err := engine.CompactContextForWorkflowPostCompletion(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.SubmitUserMessage(t.Context(), "after"); err != nil {
				t.Fatal(err)
			}
			requests := append([]llm.Request(nil), client.compactionCalls...)
			if tc.mode == "local" {
				requests = []llm.Request{client.calls[1]}
			}
			if len(requests) != 1 {
				t.Fatalf("expected one compaction request, got %d", len(requests))
			}
			requests = append(requests, client.calls[len(client.calls)-1])
			for _, request := range requests {
				if request.ReasoningEffort != "high" {
					t.Fatalf("unsupported request inherited stale native baseline: effort=%q", request.ReasoningEffort)
				}
				for _, item := range request.Items {
					if item.Type == llm.ResponseItemTypeConfigurationUpdate {
						t.Fatal("unsupported request contains a native Thinking update")
					}
				}
			}
		})
	}
}

func TestCompactionThinkingBaselineReopensBeforeNextRequest(t *testing.T) {
	client := &fakeCompactionClient{
		caps: llm.ProviderCapabilities{
			ProviderID: "openai", SupportsResponsesAPI: true, SupportsResponsesCompact: true, SupportsNativeThinkingUpdates: true,
		},
		responses:           []llm.Response{finalOutputItemResponse("seed")},
		compactionResponses: []llm.CompactionResponse{remoteCompactionReplacement(100, 10, 200000)},
	}
	store := mustCreateTestSession(t)
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-6-astra", ThinkingLevel: "medium", CompactionMode: "native",
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.CompactContextForWorkflowPostCompletion(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedStore := mustOpenTestSession(t, store.Dir())
	if reopenedStore.Meta().OriginalThinkingEffort != nil {
		t.Fatal("reopen restored the previous context's baseline")
	}
	nextClient := &fakeClient{caps: client.caps, responses: []llm.Response{finalOutputItemResponse("next")}}
	reopened := mustNewTestEngine(t, reopenedStore, nextClient, tools.NewRegistry(), Config{Model: "gpt-6-astra", ThinkingLevel: "medium"})
	if err := reopened.SetThinkingLevel(t.Context(), "high"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.SubmitUserMessage(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	if len(nextClient.calls) != 1 || nextClient.calls[0].ReasoningEffort != "high" ||
		reopenedStore.Meta().OriginalThinkingEffort == nil || *reopenedStore.Meta().OriginalThinkingEffort != "high" {
		t.Fatal("first post-compaction request did not adopt its current selected effort")
	}
}
