package runtime

import (
	"builder/server/llm"
	"builder/server/session"
	"builder/server/tools"
	"builder/shared/cachewarn"
	"builder/shared/config"
	"context"
	"encoding/json"
	"testing"
)

func TestGenerateWithRetryClient_RestoreSkipsDigestVersionMismatch(t *testing.T) {
	store := mustCreateTestSession(t)
	legacyRequest := persistedCacheRequestObserved{
		DigestVersion: 999,
		CacheKey:      "cache-key-1",
		Scope:         cachewarn.ScopeConversation,
		ChunkCount:    1,
		TerminalHash:  "legacy-hash",
	}
	legacyResponse := persistedCacheResponseObserved{
		DigestVersion:        999,
		CacheKey:             "cache-key-1",
		Scope:                cachewarn.ScopeConversation,
		ChunkCount:           1,
		TerminalHash:         "legacy-hash",
		HasCachedInputTokens: true,
		CachedInputTokens:    42,
	}
	if _, err := store.AppendEvent("legacy-request", sessionEventCacheRequestObserved, legacyRequest); err != nil {
		t.Fatalf("append legacy request: %v", err)
	}
	if _, err := store.AppendEvent("legacy-response", sessionEventCacheResponseObserved, legacyResponse); err != nil {
		t.Fatalf("append legacy response: %v", err)
	}

	reopened, err := session.Open(store.Dir())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	client := &fakeClient{responses: []llm.Response{{Usage: llm.Usage{InputTokens: 12}}}}
	eng := mustNewTestEngine(t, reopened, client, tools.NewRegistry(), Config{Model: "gpt-5", CacheWarningMode: config.CacheWarningModeDefault})

	if _, err := eng.generateWithRetryClient(context.Background(), "step-1", client, testPromptCacheRequest("cache-key-1", "beta"), nil, nil, nil); err != nil {
		t.Fatalf("generate after reopen: %v", err)
	}

	warnings := persistedCacheWarnings(t, reopened)
	if len(warnings) != 0 {
		t.Fatalf("warning count = %d, want 0", len(warnings))
	}
}

func TestGenerateWithRetryClient_DoesNotInventCompactionCauseWithoutPriorLineageOnReopen(t *testing.T) {
	store := mustCreateTestSession(t)
	if _, err := store.AppendEvent("legacy-compact", "history_replaced", historyReplacementPayload{
		Engine: "local",
		Mode:   string(compactionModeManual),
		Items:  llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleAssistant, MessageType: llm.MessageTypeCompactionSummary, Content: "summary"}}),
	}); err != nil {
		t.Fatalf("append history_replaced: %v", err)
	}

	reopened, err := session.Open(store.Dir())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	client := &fakeClient{responses: []llm.Response{{Usage: llm.Usage{InputTokens: 12}}}}
	eng := mustNewTestEngine(t, reopened, client, tools.NewRegistry(), Config{Model: "gpt-5", CacheWarningMode: config.CacheWarningModeVerbose})

	if _, err := eng.generateWithRetryClient(context.Background(), "step-1", client, testPromptCacheRequest(reopened.Meta().SessionID, "beta"), nil, nil, nil); err != nil {
		t.Fatalf("generate after reopen: %v", err)
	}

	warnings := persistedCacheWarnings(t, reopened)
	if len(warnings) != 0 {
		t.Fatalf("warning count = %d, want 0", len(warnings))
	}
}

func testPromptCacheRequest(cacheKey string, messages ...string) llm.Request {
	items := make([]llm.ResponseItem, 0, len(messages))
	for _, message := range messages {
		items = append(items, llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: message}})...)
	}
	return llm.Request{
		Model:            "gpt-5",
		SystemPrompt:     "system",
		PromptCacheKey:   cacheKey,
		PromptCacheScope: cachewarn.ScopeConversation,
		Items:            items,
	}
}

func testReviewerPromptCacheRequest(cacheKey string, messages ...string) llm.Request {
	request := testPromptCacheRequest(cacheKey, messages...)
	request.PromptCacheScope = cachewarn.ScopeReviewer
	return request
}

func mustMarshalOpenAIResponsesPayloadForLineage(t *testing.T, request llm.Request, options llm.OpenAIResponsesPayloadOptions) []byte {
	t.Helper()
	data, err := llm.MarshalOpenAIResponsesRequestJSON(request, options)
	if err != nil {
		t.Fatalf("marshal openai responses payload: %v", err)
	}
	return data
}

func mustDecodeJSONMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode json map: %v", err)
	}
	return decoded
}

func mustMarshalCanonicalJSONForLineage(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal canonical json: %v", err)
	}
	return data
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func persistedCacheWarnings(t *testing.T, store *session.Store) []cachewarn.Warning {
	t.Helper()
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	warnings := make([]cachewarn.Warning, 0, len(events))
	for _, evt := range events {
		if evt.Kind != sessionEventCacheWarning {
			continue
		}
		var warning cachewarn.Warning
		if err := json.Unmarshal(evt.Payload, &warning); err != nil {
			t.Fatalf("decode warning: %v", err)
		}
		warnings = append(warnings, warning)
	}
	return warnings
}

func persistedCacheWarningEventCount(t *testing.T, store *session.Store) int {
	t.Helper()
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	count := 0
	for _, evt := range events {
		if evt.Kind == sessionEventCacheWarning {
			count++
		}
	}
	return count
}
