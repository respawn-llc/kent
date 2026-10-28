package runtime

import (
	"encoding/json"
	"testing"

	"core/server/llm"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestHostedAnswerWithoutPhaseFinishesTurn(t *testing.T) {
	client := &fakeClient{responses: []llm.Response{{
		Assistant:     llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("answer")},
		ProviderPhase: llm.AbsentProviderPhase(),
		OutputItems: []llm.ResponseItem{{
			Type: llm.ResponseItemTypeOther,
			Raw:  json.RawMessage(`{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"kent"}}`),
		}},
		Usage: llm.Usage{WindowTokens: 500000},
	}}}
	client.caps = llm.ProviderCapabilities{ProviderID: "grok-oauth-api", SupportsResponsesAPI: true, SupportsNativeWebSearch: true}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{
		Model: "grok-4.7", ThinkingLevel: "high", WebSearchMode: "native", EnabledTools: []toolspec.ID{toolspec.ToolWebSearch},
	})
	message, err := engine.SubmitUserMessage(t.Context(), "search")
	if err != nil {
		t.Fatal(err)
	}
	if messageContent(message) != "answer" || len(client.calls) != 1 {
		t.Fatalf("answer=%+v requests=%d", message, len(client.calls))
	}
}
