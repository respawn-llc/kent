package llm_test

import (
	"encoding/json"
	"strings"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/shared/config"
	"core/shared/textutil"
)

func TestDefaultTokenEstimatorUTF8AndItemRounding(t *testing.T) {
	estimator := llm.DefaultTokenEstimator{}
	text := "éabc"
	if got := estimator.EstimateText(text); got != 2 {
		t.Fatalf("UTF-8 text estimate = %d, want 2", got)
	}
	items := []llm.ResponseItem{
		{Type: llm.ResponseItemTypeMessage, Content: &text},
		{Type: llm.ResponseItemTypeMessage, Content: &text},
	}
	if got := llm.EstimateItemsTokens(estimator, items); got != 4 {
		t.Fatalf("collection estimate = %d, want 4", got)
	}
}

func TestConnectionEstimatorUsesActualTransport(t *testing.T) {
	payload := strings.Repeat("a", 4000)
	item := llm.ResponseItem{Type: llm.ResponseItemTypeReasoning, EncryptedContent: &payload}
	for _, test := range []struct {
		name       string
		connection config.ProviderConnection
		want       int
	}{
		{"OpenAI endpoint", config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://api.openai.com/v1"),
			Capabilities: config.ProviderCapabilitiesOverride{ProviderID: "compatible", SupportsResponsesAPI: true}}, 588},
		{"ChatGPT", config.ProviderConnection{Protocol: config.ConnectionChatGPT}, 588},
		{"compatible", config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://localhost:1234/v1"),
			Capabilities: config.ProviderCapabilitiesOverride{ProviderID: "openai", IsOpenAIFirstParty: true, SupportsResponsesAPI: true}}, 1000},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := config.ConnectionID("selected")
			settings := config.Settings{Connection: &id, Model: "claude-model-alias",
				Connections: map[config.ConnectionID]config.ProviderConnection{id: test.connection}}
			locked := &session.LockedContract{ProviderContract: session.LockedProviderCapabilities{ProviderID: "anthropic"}}
			if _, err := llm.ResolveEffectiveProviderCapabilities(locked, settings); err != nil {
				t.Fatal(err)
			}
			estimator, err := llm.ResolveConnectionTokenEstimator(test.connection)
			if err != nil {
				t.Fatal(err)
			}
			if got := estimator.EstimateItem(item); got != test.want {
				t.Fatalf("actual transport estimate = %d, want %d", got, test.want)
			}
		})
	}
}

func TestOpenAITokenEstimatorEncryptedReasoning(t *testing.T) {
	estimator := llm.OpenAITokenEstimator{}
	for _, test := range []struct {
		encodedBytes int
		want         int
	}{
		{866, 0},
		{868, 1},
		{4000, 588},
	} {
		payload := strings.Repeat("a", test.encodedBytes)
		for _, kind := range []llm.ResponseItemType{llm.ResponseItemTypeReasoning, llm.ResponseItemTypeCompaction} {
			item := llm.ResponseItem{Type: kind, EncryptedContent: &payload}
			if got := estimator.EstimateItem(item); got != test.want {
				t.Fatalf("%s encrypted estimate = %d, want %d", kind, got, test.want)
			}
			if got := (llm.DefaultTokenEstimator{}).EstimateItem(item); got != (test.encodedBytes+3)/4 {
				t.Fatalf("compatible plaintext estimate = %d", got)
			}
		}
	}
	plaintext := "abcde"
	if got := estimator.EstimateItem(llm.ResponseItem{Type: llm.ResponseItemTypeReasoning, Content: &plaintext}); got != 2 {
		t.Fatalf("unencrypted reasoning estimate = %d, want 2", got)
	}
}

func TestDefaultTokenEstimatorPlainReasoningAndInvisibleItems(t *testing.T) {
	text, summary, metadata := "abc", "d", "not-visible"
	estimator := llm.DefaultTokenEstimator{}
	for _, kind := range []llm.ResponseItemType{llm.ResponseItemTypeReasoning, llm.ResponseItemTypeCompaction} {
		item := llm.ResponseItem{Type: kind, Content: &text, ReasoningSummary: []llm.ReasoningEntry{{Text: summary}}}
		if got := estimator.EstimateItem(item); got != 1 {
			t.Fatalf("%s plaintext estimate = %d, want 1", kind, got)
		}
		item.Content = nil
		item.EncryptedContent = &text
		if got := estimator.EstimateItem(item); got != 1 {
			t.Fatalf("%s compatible opaque text estimate = %d, want 1", kind, got)
		}
	}
	for _, kind := range []llm.ResponseItemType{llm.ResponseItemTypeConfigurationUpdate, llm.ResponseItemTypeOther} {
		item := llm.ResponseItem{Type: kind, Content: &metadata, ConfigurationEffort: &metadata}
		if got := estimator.EstimateItem(item); got != 0 {
			t.Fatalf("%s non-visible estimate = %d, want 0", kind, got)
		}
	}
}

func TestDefaultTokenEstimatorStructuredImagesAndFiles(t *testing.T) {
	estimator := llm.DefaultTokenEstimator{}
	for _, test := range []struct {
		output string
		want   int
	}{
		{`[{"type":"input_text","text":"abc"},{"type":"input_image","image_url":"data:image/png;base64,AAAA","detail":"high"}]`, 1844},
		{`[{"type":"input_image","image_url":"https://example.org/image.png","detail":"low"}]`, 1844},
		{`[{"type":"input_file","file_data":"data:application/pdf;base64,AAAA","filename":"abc"}]`, 513},
		{`[{"type":"input_file","file_id":"abc","file_url":"https://example.org/a","filename":"d"}]`, 519},
	} {
		item := llm.ResponseItem{Type: llm.ResponseItemTypeFunctionCallOutput, Output: json.RawMessage(test.output)}
		if got := estimator.EstimateItem(item); got != test.want {
			t.Fatalf("structured estimate = %d, want %d", got, test.want)
		}
	}
}

func TestDefaultTokenEstimatorDecodedOutputs(t *testing.T) {
	callID, name := "ab", "cd"
	estimator := llm.DefaultTokenEstimator{}
	for _, kind := range []llm.ResponseItemType{llm.ResponseItemTypeFunctionCallOutput, llm.ResponseItemTypeCustomToolOutput} {
		item := llm.ResponseItem{Type: kind, CallID: &callID, Name: &name, Output: json.RawMessage(`"\u00e9\n"`)}
		if got := estimator.EstimateItem(item); got != 2 {
			t.Fatalf("%s decoded output estimate = %d, want 2", kind, got)
		}
		item.Output = json.RawMessage(`{"value":1}`)
		if got := estimator.EstimateItem(item); got != 4 {
			t.Fatalf("%s object output estimate = %d, want 4", kind, got)
		}
	}
}

func TestDefaultTokenEstimatorToolInputsAndMetadata(t *testing.T) {
	name, input, metadata := "ab", "cd", "transport-metadata"
	for _, item := range []llm.ResponseItem{
		{Type: llm.ResponseItemTypeFunctionCall, Name: &name, Arguments: json.RawMessage(input)},
		{Type: llm.ResponseItemTypeCustomToolCall, Name: &name, CustomInput: &input},
	} {
		estimator := llm.DefaultTokenEstimator{}
		if got := estimator.EstimateItem(item); got != 4 {
			t.Fatalf("tool input with implicit namespace estimate = %d, want 4", got)
		}
		item.ID, item.CallID, item.ConfigurationEffort = &metadata, &metadata, &metadata
		if got := estimator.EstimateItem(item); got != 4 {
			t.Fatalf("metadata changed tool input estimate to %d", got)
		}
	}
}
