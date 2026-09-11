package runtime

import (
	"bytes"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/textutil"
)

func TestConfigurationHistoryReopensWithoutChatRows(t *testing.T) {
	t.Run("ordinary", func(t *testing.T) { testConfigurationHistoryReopen(t, false) })
	t.Run("replacement", func(t *testing.T) { testConfigurationHistoryReopen(t, true) })
}

func testConfigurationHistoryReopen(t *testing.T, replacement bool) {
	store := mustCreateTestSession(t)
	item := llm.PrepareOpenAIInputItems([]llm.ResponseItem{{
		Type: llm.ResponseItemTypeConfigurationUpdate, ConfigurationEffort: textutil.Value("low"),
	}})[0]
	history, err := sessionProviderHistoryItemFromLLM(0, item)
	if err != nil {
		t.Fatal(err)
	}
	engine := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(), Config{})
	log := engine.eventLog
	var payload session.EventRecordPayload = session.ConfigurationUpdateRecord{Item: history}
	if replacement {
		payload = session.HistoryReplacementRecord{
			Engine: "remote", Mode: session.CompactionModeManual, Items: []session.ProviderHistoryItem{history},
		}
	}
	if _, _, err := log.AppendRecord(textutil.Value("configuration-step"), payload); err != nil {
		t.Fatal(err)
	}
	if _, _, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: textutil.Value("after")}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), &fakeClient{}, tools.NewRegistry(), Config{})
	request, err := reopened.buildRequest(t.Context(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i, got := range request.Items {
		if got.Type != llm.ResponseItemTypeConfigurationUpdate {
			continue
		}
		found = true
		if !bytes.Equal(got.Raw, item.Raw) || got.ConfigurationEffort == nil || *got.ConfigurationEffort != "low" ||
			i+1 >= len(request.Items) || request.Items[i+1].Role == nil || *request.Items[i+1].Role != llm.RoleUser {
			t.Fatalf("configuration history did not round trip: %+v", request.Items)
		}
	}
	if !found {
		t.Fatal("configuration update missing from reopened request")
	}
	messages := reopened.transcriptRuntimeState().SnapshotMessages()
	if len(messages) != 1 || messages[0].Role != llm.RoleUser {
		t.Fatalf("non-message configuration projected as message: %+v", messages)
	}
}

func TestThinkingReestablishesAfterCheckpointWithSynthesizedToolOutput(t *testing.T) {
	store := mustCreateTestSession(t)
	log := mustMaterializeTestEventLog(t, store)
	items := llm.PrepareOpenAIInputItems([]llm.ResponseItem{
		{Type: llm.ResponseItemTypeFunctionCall, CallID: textutil.Value("call"), Name: textutil.Value("tool"), Arguments: []byte(`{}`)},
		{Type: llm.ResponseItemTypeConfigurationUpdate, ConfigurationEffort: textutil.Value("low")},
	})
	var history []session.ProviderHistoryItem
	for i, item := range items {
		converted, err := sessionProviderHistoryItemFromLLM(i, item)
		if err != nil {
			t.Fatal(err)
		}
		history = append(history, converted)
	}
	for _, payload := range []session.EventRecordPayload{
		session.HistoryReplacementRecord{Engine: "remote", Mode: session.CompactionModeManual, Items: history},
		session.ToolCompletionRecord{CallID: "call", Name: "tool", OutputKind: session.ToolOutputKindFunction, Output: []byte(`"done"`)},
		session.MessageRecord{Role: session.MessageRoleUser, Content: textutil.Value("continued")},
	} {
		if _, _, err := log.AppendRecord(textutil.Value("source-step"), payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AdoptOriginalThinkingEffort("high"); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{caps: llm.ProviderCapabilities{
		ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: true,
	}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-6-astra", ThinkingLevel: "low"})
	request, err := PrepareInspectionRequest(t.Context(), engine, false)
	if err != nil {
		t.Fatal(err)
	}
	var updates, outputs int
	for _, item := range request.Items {
		if item.Type == llm.ResponseItemTypeConfigurationUpdate {
			updates++
		}
		if item.Type == llm.ResponseItemTypeFunctionCallOutput {
			outputs++
		}
	}
	if updates != 2 || outputs != 1 {
		t.Fatalf("checkpoint request has %d updates and %d tool outputs, want 2 and 1", updates, outputs)
	}
}
