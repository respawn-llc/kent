package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/modelcontract"
	"core/shared/textutil"
	"core/shared/transcript"
)

func TestProviderUsageObservationsSurviveSessionReopen(t *testing.T) {
	t.Parallel()
	for _, withPromptCache := range []bool{true, false} {
		t.Run(fmt.Sprintf("prompt-cache=%t", withPromptCache), func(t *testing.T) {
			store := mustCreateTestSession(t)
			client := &fakeClient{responses: []llm.Response{
				providerUsageTestResponse(2),
				providerUsageTestResponse(5),
			}}
			engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})

			request := providerUsageTestRequest(store.Meta().SessionID, withPromptCache)
			for index := range client.responses {
				if _, err := generateTestActiveStep(context.Background(), engine, fmt.Sprintf("usage-%d", index), client, request); err != nil {
					t.Fatalf("generate response %d: %v", index, err)
				}
			}
			if err := engine.Close(); err != nil {
				t.Fatalf("close first engine: %v", err)
			}

			reopened, err := runtimeTestSessionPersistence.Open(store.Dir())
			if err != nil {
				t.Fatalf("reopen session: %v", err)
			}
			reopenedClient := &fakeClient{responses: []llm.Response{providerUsageTestResponse(7)}}
			reopenedEngine := mustNewTestEngine(t, reopened, reopenedClient, tools.NewRegistry(), Config{Model: "gpt-5"})
			if _, err := generateTestActiveStep(context.Background(), reopenedEngine, "usage-2", reopenedClient, request); err != nil {
				t.Fatalf("generate response after reopen: %v", err)
			}

			records := providerUsageTestRecords(t, reopened)
			if len(records) != 3 {
				t.Fatalf("provider usage records = %d, want 3", len(records))
			}
			operationIDs := make(map[string]struct{}, len(records))
			for index, record := range records {
				if record.Evidence.RequestedModel != "gpt-5" {
					t.Fatalf("record %d requested model = %q, want gpt-5", index, record.Evidence.RequestedModel)
				}
				if record.SessionID != reopened.Meta().SessionID {
					t.Fatalf("record %d session ID = %q, want %q", index, record.SessionID, reopened.Meta().SessionID)
				}
				if record.Purpose != modelcontract.ProviderOperationPurposeGeneration {
					t.Fatalf("record %d purpose = %q, want generation", index, record.Purpose)
				}
				if record.ObservedAt.IsZero() {
					t.Fatalf("record %d has no observation time", index)
				}
				if _, exists := operationIDs[record.OperationID]; exists {
					t.Fatalf("operation ID %q was reused", record.OperationID)
				}
				operationIDs[record.OperationID] = struct{}{}
				if record.Evidence.Usage == nil {
					t.Fatalf("record %d usage is absent", index)
				}
				var usage struct {
					OutputTokens int `json:"output_tokens"`
				}
				if err := json.Unmarshal(*record.Evidence.Usage, &usage); err != nil {
					t.Fatalf("decode record %d usage: %v", index, err)
				}
				if got, want := usage.OutputTokens, []int{2, 5, 7}[index]; got != want {
					t.Fatalf("record %d output tokens = %d, want %d", index, got, want)
				}
			}
		})
	}
}

func providerUsageTestRequest(sessionID string, withPromptCache bool) llm.Request {
	request := llm.Request{
		Model:          "gpt-5",
		ToolChoiceMode: llm.ToolChoiceModeAutomatic,
		Items:          llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("retain usage")}}),
	}
	if withPromptCache {
		request.PromptCacheKey = sessionID
		request.PromptCacheScope = transcript.CacheWarningScopeConversation
	}
	return request
}

func providerUsageTestResponse(outputTokens int) llm.Response {
	raw := json.RawMessage(fmt.Sprintf(
		`{"input_tokens":1,"output_tokens":%d,"total_tokens":%d}`,
		outputTokens,
		outputTokens+1,
	))
	return llm.Response{
		Assistant: llm.Message{
			Role:    llm.RoleAssistant,
			Phase:   textutil.Value(llm.MessagePhaseFinal),
			Content: textutil.Value("retained"),
		},
		Usage: llm.Usage{InputTokens: 1, OutputTokens: outputTokens},
		ProviderEvidence: modelcontract.ProviderUsageEvidence{
			ProviderID: textutil.Value("test-provider"),
			Usage:      &raw,
		},
	}
}

func providerUsageTestRecords(t *testing.T, store *session.Store) []session.ProviderUsageRecord {
	t.Helper()
	window, err := mustMaterializeTestEventLog(t, store).ReadRecentRecords(64)
	if err != nil {
		t.Fatalf("read recent event records: %v", err)
	}
	records := make([]session.ProviderUsageRecord, 0, len(window.Records))
	for _, event := range window.Records {
		payload, err := event.Payload()
		if err != nil {
			t.Fatalf("decode event payload: %v", err)
		}
		record, ok := payload.(session.ProviderUsageRecord)
		if ok {
			records = append(records, record)
		}
	}
	return records
}
