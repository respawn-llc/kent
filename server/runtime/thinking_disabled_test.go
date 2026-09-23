package runtime

import (
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/textutil"
)

func TestDisabledThinkingInitialNativeRequest(t *testing.T) {
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna"} {
		t.Run(model, func(t *testing.T) {
			client := &fakeClient{
				caps:      llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: true},
				responses: []llm.Response{finalOutputItemResponse("done")},
			}
			store := mustCreateTestSession(t)
			engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: model})
			if _, err := engine.SubmitUserMessage(t.Context(), "hello"); err != nil {
				t.Fatal(err)
			}
			if len(client.calls) != 1 || client.calls[0].ReasoningEffort != "none" {
				t.Fatalf("disabled request = %+v", client.calls)
			}
			if baseline := store.Meta().OriginalThinkingEffort; baseline == nil || *baseline != "none" {
				t.Fatalf("disabled baseline = %v", baseline)
			}
			if engine.ThinkingLevel() != "" {
				t.Fatal("provider effort changed the desired disabled setting")
			}
		})
	}
}

func TestDisabledThinkingDuringNativeCompaction(t *testing.T) {
	client := &fakeCompactionClient{
		caps:                llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsResponsesCompact: true, SupportsNativeThinkingUpdates: true},
		responses:           []llm.Response{finalOutputItemResponse("seed"), finalOutputItemResponse("disabled"), finalOutputItemResponse("after")},
		compactionResponses: []llm.CompactionResponse{remoteCompactionReplacement(100, 10, 200000)},
	}
	store := mustCreateTestSession(t)
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-6-sol", ThinkingLevel: "high", CompactionMode: "native"})
	if _, err := engine.SubmitUserMessage(t.Context(), "seed"); err != nil {
		t.Fatal(err)
	}
	dispatched := llm.CloneResponseItems(client.calls[0].Items)
	if err := engine.SetThinkingLevel(t.Context(), "none"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SubmitUserMessage(t.Context(), "disable"); err != nil {
		t.Fatal(err)
	}
	request := client.calls[1]
	if request.ReasoningEffort != "high" {
		t.Fatalf("changed original baseline to %q", request.ReasoningEffort)
	}
	requireDisabledThinkingUpdate(t, request.Items)
	if !reflect.DeepEqual(dispatched, client.calls[0].Items) {
		t.Fatal("changing Thinking mutated the dispatched request")
	}
	if _, err := engine.CompactContextForWorkflowPostCompletion(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(client.compactionCalls) != 1 || client.compactionCalls[0].ReasoningEffort != "high" {
		t.Fatalf("compaction did not keep outgoing baseline: %+v", client.compactionCalls)
	}
	requireDisabledThinkingUpdate(t, client.compactionCalls[0].Items)
	if _, err := engine.SubmitUserMessage(t.Context(), "after"); err != nil {
		t.Fatal(err)
	}
	if client.calls[2].ReasoningEffort != "none" {
		t.Fatal("post-compaction request did not adopt disabled baseline")
	}
	if engine.ThinkingLevel() != "none" {
		t.Fatal("provider effort changed the desired disabled setting")
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedStore := mustOpenTestSession(t, store.Dir())
	if reopenedStore.Meta().ChatSettings.Thinking == nil || *reopenedStore.Meta().ChatSettings.Thinking != "none" {
		t.Fatal("disabled Thinking was not persisted")
	}
	reopenedClient := &fakeClient{caps: client.caps, responses: []llm.Response{finalOutputItemResponse("resumed")}}
	reopened := mustNewTestEngine(t, reopenedStore, reopenedClient, tools.NewRegistry(), Config{
		Model: "gpt-6-sol", ThinkingLevel: *reopenedStore.Meta().ChatSettings.Thinking,
	})
	if _, err := reopened.SubmitUserMessage(t.Context(), "resumed"); err != nil {
		t.Fatal(err)
	}
	if len(reopenedClient.calls) != 1 || reopenedClient.calls[0].ReasoningEffort != "none" {
		t.Fatal("resumed request lost disabled Thinking")
	}
}

func requireDisabledThinkingUpdate(t *testing.T, items []llm.ResponseItem) {
	t.Helper()
	for _, item := range items {
		if item.Type == llm.ResponseItemTypeConfigurationUpdate && item.ConfigurationEffort != nil && *item.ConfigurationEffort == "none" {
			return
		}
	}
	t.Fatal("missing disabled Thinking update")
}

func TestDisabledThinkingReviewerSelection(t *testing.T) {
	client := &fakeClient{
		caps:      llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: true},
		responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value(`{"suggestions":[]}`)}}},
	}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, tools.NewRegistry(), Config{
		Model: "gpt-6-astra", ThinkingLevel: "high", Reviewer: ReviewerConfig{
			Model: "gpt-6-luna", ModelCapabilities: session.LockedModelCapabilities{SupportsReasoningEffort: true},
		},
	})
	if _, err := runReviewerSuggestionsTestActiveStep(t.Context(), engine, "disabled-reviewer", client); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 || client.calls[0].ReasoningEffort != "none" {
		t.Fatal("Reviewer request did not resolve its independent disabled effort")
	}
}

func TestDisabledThinkingUnsupportedNativeEffortFailsBeforeDispatch(t *testing.T) {
	client := &fakeClient{
		caps: llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: true},
	}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-astra"})
	if _, err := engine.SubmitUserMessage(t.Context(), "hello"); err == nil {
		t.Fatal("Astra accepted an unsupported disabled native effort")
	}
	if len(client.calls) != 0 {
		t.Fatal("invalid native effort reached the provider")
	}
}
