package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"core/shared/textutil"
)

func TestGenerateRetainsProviderUsageEvidence(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.completed","response":{"id":"resp_usage_1","created_at":1720000000,"model":"gpt-served","service_tier":"priority","usage":{"input_tokens":11,"input_tokens_details":{"cached_tokens":4,"cache_write_tokens":2},"output_tokens":7,"output_tokens_details":{"reasoning_tokens":3},"total_tokens":18,"provider_extension":{"units":"3.5"}},"usage_metadata":{"amount":"0.42","provider":"codex"},"output":[{"type":"web_search_call","id":"web_1","status":"completed","action":{"type":"search"}},{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done"}]}]}}`,
		`[DONE]`,
	)
	transport.ProviderCapabilitiesOverride = &ProviderCapabilities{
		ProviderID:              "openai",
		SupportsResponsesAPI:    true,
		SupportsNativeWebSearch: true,
		IsOpenAIFirstParty:      true,
	}
	response, err := transport.Generate(context.Background(), OpenAIRequest{
		Model:                 "gpt-requested",
		FastMode:              true,
		EnableNativeWebSearch: true,
		SessionID:             textutil.Value("test-session"),
		ToolChoiceMode:        ToolChoiceModeAutomatic,
	}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	evidence := response.ProviderEvidence
	if evidence.ProviderID == nil || *evidence.ProviderID != "openai" {
		t.Fatalf("provider ID = %v, want openai", evidence.ProviderID)
	}
	if evidence.RequestedModel != "gpt-requested" {
		t.Fatalf("requested model = %q, want gpt-requested", evidence.RequestedModel)
	}
	if evidence.RequestedServiceTier == nil || *evidence.RequestedServiceTier != "priority" {
		t.Fatalf("requested service tier = %v, want priority", evidence.RequestedServiceTier)
	}
	if evidence.ServedModel == nil || *evidence.ServedModel != "gpt-served" {
		t.Fatalf("served model = %v, want gpt-served", evidence.ServedModel)
	}
	if evidence.ServedServiceTier == nil || *evidence.ServedServiceTier != "priority" {
		t.Fatalf("served service tier = %v, want priority", evidence.ServedServiceTier)
	}
	if evidence.EndpointOrigin == nil {
		t.Fatal("endpoint origin is absent")
	}
	if evidence.ResponseID == nil || *evidence.ResponseID != "resp_usage_1" {
		t.Fatalf("response ID = %v, want resp_usage_1", evidence.ResponseID)
	}
	if evidence.ResponseCreatedAt == nil || evidence.ResponseCreatedAt.Unix() != 1720000000 {
		t.Fatalf("response created at = %v, want Unix 1720000000", evidence.ResponseCreatedAt)
	}
	assertJSONField(t, evidence.Usage, "provider_extension", `{"units":"3.5"}`)
	assertJSONField(t, evidence.UsageMetadata, "amount", `"0.42"`)
	if len(evidence.HostedTools) != 1 {
		t.Fatalf("hosted tool evidence count = %d, want 1", len(evidence.HostedTools))
	}
	tool := evidence.HostedTools[0]
	if tool.ID == nil || *tool.ID != "web_1" || tool.Type == nil || *tool.Type != "web_search_call" ||
		tool.Status == nil || *tool.Status != "completed" || tool.ActionKind == nil || *tool.ActionKind != "search" {
		t.Fatalf("hosted tool evidence = %+v", tool)
	}
	if len(evidence.RequestedHostedTools) != 1 || evidence.RequestedHostedTools[0].Type != "web_search" {
		t.Fatalf("requested hosted tools = %+v", evidence.RequestedHostedTools)
	}
}

func TestProviderUsageEvidencePreservesNullAndZeroUsage(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		transport := newOpenAIStreamTestTransport(t,
			`{"type":"response.completed","response":{"model":"gpt-5","output":[]}}`,
			`[DONE]`,
		)
		response, err := transport.Generate(context.Background(), OpenAIRequest{
			Model: "gpt-5", SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic,
		}, StreamCallbacks{})
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}
		if response.ProviderEvidence.Usage != nil {
			t.Fatalf("usage = %s, want null", *response.ProviderEvidence.Usage)
		}
	})

	t.Run("explicit zero", func(t *testing.T) {
		transport := newOpenAIStreamTestTransport(t,
			`{"type":"response.completed","response":{"model":"gpt-5","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0},"output":[]}}`,
			`[DONE]`,
		)
		response, err := transport.Generate(context.Background(), OpenAIRequest{
			Model: "gpt-5", SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic,
		}, StreamCallbacks{})
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}
		if response.ProviderEvidence.Usage == nil {
			t.Fatal("usage is absent, want explicit zero object")
		}
		var usage map[string]json.RawMessage
		if err := json.Unmarshal(*response.ProviderEvidence.Usage, &usage); err != nil {
			t.Fatalf("decode usage: %v", err)
		}
		if string(usage["input_tokens"]) != "0" || string(usage["output_tokens"]) != "0" || string(usage["total_tokens"]) != "0" {
			t.Fatalf("usage = %s, want explicit zero counts", *response.ProviderEvidence.Usage)
		}
	})
}

func TestGenerateRejectsMalformedHostedToolEvidence(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.completed","response":{"model":"gpt-5","output":[{"type":"web_search_call","id":"web_1","status":"completed","action":{"type":1}}]}}`,
		`[DONE]`,
	)
	_, err := transport.Generate(context.Background(), OpenAIRequest{
		Model: "gpt-5", SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic,
	}, StreamCallbacks{})
	if err == nil {
		t.Fatal("Generate succeeded with malformed hosted-tool evidence")
	}
}

func TestCompactRetainsProviderUsageEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: " +
				`{"type":"response.completed","response":{"id":"compact_1","created_at":1720000001,"model":"gpt-compacted","service_tier":"default","usage":{"input_tokens":9,"output_tokens":4,"total_tokens":13,"input_tokens_details":{"cache_write_tokens":6},"output_tokens_details":{"reasoning_tokens":1}},"output":[{"type":"compaction","id":"checkpoint_1","encrypted_content":"enc_1"}]}}` +
				"\n\ndata: [DONE]\n\n",
		))
	}))
	t.Cleanup(server.Close)
	transport := NewHTTPTransport(staticAuthHeader{})
	transport.Client = newRewritingHTTPClient(t, server)
	response, err := transport.Compact(context.Background(), OpenAIRequest{
		Model:          "gpt-requested",
		SessionID:      textutil.Value("test-session"),
		ToolChoiceMode: ToolChoiceModeAutomatic,
	})
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if response.ProviderEvidence.ResponseID == nil || *response.ProviderEvidence.ResponseID != "compact_1" {
		t.Fatalf("response ID = %v, want compact_1", response.ProviderEvidence.ResponseID)
	}
	if response.ProviderEvidence.ServedModel == nil || *response.ProviderEvidence.ServedModel != "gpt-compacted" {
		t.Fatalf("served model = %v, want gpt-compacted", response.ProviderEvidence.ServedModel)
	}
	assertJSONField(t, response.ProviderEvidence.Usage, "output_tokens", "4")
}

func assertJSONField(t *testing.T, raw *json.RawMessage, key string, want string) {
	t.Helper()
	if raw == nil {
		t.Fatalf("JSON object is absent, want field %q", key)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(*raw, &fields); err != nil {
		t.Fatalf("decode JSON object: %v", err)
	}
	if !bytes.Equal(fields[key], []byte(want)) {
		t.Fatalf("field %q = %s, want %s", key, fields[key], want)
	}
}
