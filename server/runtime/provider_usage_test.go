package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

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
			for index, record := range records {
				assertProviderUsageOutputTokens(t, record, []int{2, 5, 7}[index])
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
		Assistant:        llm.Message{Role: llm.RoleAssistant, Phase: textutil.Value(llm.MessagePhaseFinal), Content: textutil.Value("retained")},
		Usage:            llm.Usage{InputTokens: 1, OutputTokens: outputTokens},
		ProviderEvidence: modelcontract.ProviderUsageEvidence{ProviderID: textutil.Value("test-provider"), Usage: &raw},
	}
}

func providerUsageTestMixedResponse(outputTokens int) llm.Response {
	response := providerUsageTestResponse(outputTokens)
	usage := json.RawMessage(fmt.Sprintf(
		`{"input_tokens":11,"input_tokens_details":{"cached_tokens":4,"cache_write_tokens":6},"output_tokens":%d,"output_tokens_details":{"reasoning_tokens":3},"total_tokens":%d,"provider_extension":{"units":"3.5"}}`,
		outputTokens,
		outputTokens+11,
	))
	usageMetadata := json.RawMessage(`{"amount":"0.42","provider":"codex"}`)
	hostedUsage := json.RawMessage(`{"searches":2}`)
	hostedOptions := json.RawMessage(`{"search_context_size":"high"}`)
	createdAt := time.Unix(1_720_000_000+int64(outputTokens), 0).UTC()
	response.ProviderEvidence = modelcontract.ProviderUsageEvidence{
		ProviderID:           textutil.Value("mixed-provider"),
		EndpointOrigin:       textutil.Value("https://api.example.test"),
		RequestedModel:       "gpt-5",
		ServedModel:          textutil.Value("gpt-5-served"),
		RequestedServiceTier: textutil.Value("priority"),
		ServedServiceTier:    textutil.Value("default"),
		ResponseID:           textutil.Value(fmt.Sprintf("mixed-response-%d", outputTokens)),
		ResponseCreatedAt:    &createdAt,
		Usage:                &usage,
		UsageMetadata:        &usageMetadata,
		HostedTools: []modelcontract.HostedToolUsageEvidence{{
			ID:         textutil.Value(fmt.Sprintf("mixed-web-%d", outputTokens)),
			Type:       textutil.Value("web_search_call"),
			Status:     textutil.Value("completed"),
			ActionKind: textutil.Value("search"),
			Usage:      &hostedUsage,
		}},
		RequestedHostedTools: []modelcontract.HostedToolConfiguration{{
			Type:    "web_search",
			Options: hostedOptions,
		}},
	}
	return response
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
