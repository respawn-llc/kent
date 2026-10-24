package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"core/shared/modelcontract"
	"core/shared/textutil"
)

func TestEmptyLocalCompactionDiagnostics(t *testing.T) {
	response, failures, _ := runLocalCompactionDiagnostic(t,
		[]json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"reasoning-1","summary":[]}`)},
		true, textutil.Value(modelcontract.ProviderOperationPurposeCompaction))
	if response.Assistant.Content != nil {
		t.Fatal("diagnostics changed empty provider output")
	}
	if len(failures) != 1 {
		t.Fatalf("empty compaction diagnostics = %d, want 1", len(failures))
	}
	record := failures[0]
	if record["response_id"] != "response-empty" || record["provider_status"] != "completed" ||
		record["provider_terminal_event"] != "response.completed" ||
		record["provider_text_bytes"] != float64(0) || record["extracted_summary_bytes"] != float64(0) ||
		record["output_tokens"] != float64(3526) || record["reasoning_tokens"] != float64(3520) {
		t.Fatalf("missing provider/extraction/usage evidence: %+v", record)
	}
}

func TestEmptyLocalCompactionDiagnosticsDistinguishSelectionLoss(t *testing.T) {
	privateText := "private provider commentary"
	privateRefusal := "private refusal reason"
	privateReasoning := "private reasoning summary"
	output := []json.RawMessage{
		json.RawMessage(`{"type":"reasoning","id":"reasoning-1","summary":[{"type":"summary_text","text":"` + privateReasoning + `"}]}`),
		json.RawMessage(`{"type":"message","id":"commentary-1","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"` + privateText + `"}]}`),
		json.RawMessage(`{"type":"message","id":"final-1","role":"assistant","phase":"final_answer","content":[{"type":"refusal","refusal":"` + privateRefusal + `"}]}`),
	}
	response, failures, logs := runLocalCompactionDiagnostic(t, output, true,
		textutil.Value(modelcontract.ProviderOperationPurposeCompaction))
	if response.Assistant.Content != nil || len(failures) != 1 {
		t.Fatalf("response content=%v, diagnostics=%d", response.Assistant.Content, len(failures))
	}
	record := failures[0]
	if record["provider_text_bytes"] != float64(len(privateText)) ||
		record["provider_refusal_present"] != true || record["transport_assistant_bytes"] != float64(0) ||
		record["normalized_assistant_bytes"] != float64(0) || record["extracted_summary_bytes"] != float64(0) {
		t.Fatalf("selection loss was not distinguished from provider-empty: %+v", record)
	}
	providerItems := record["provider_output_items"].([]any)
	commentary := providerItems[1].(map[string]any)
	if commentary["type"] != "message" || commentary["phase"] != "commentary" ||
		commentary["text_bytes"] != float64(len(privateText)) {
		t.Fatalf("provider item metadata = %+v", commentary)
	}
	normalizedItems := record["normalized_output_items"].([]any)
	normalizedCommentary := normalizedItems[1].(map[string]any)
	if normalizedCommentary["phase"] != "commentary" ||
		normalizedCommentary["text_bytes"] != float64(len(privateText)) {
		t.Fatalf("normalized output evidence = %+v", normalizedCommentary)
	}
	for _, secret := range []string{privateText, privateRefusal, privateReasoning} {
		if bytes.Contains(logs, []byte(secret)) {
			t.Fatal("private provider content leaked into diagnostics")
		}
	}
}

func TestLocalCompactionDiagnosticExclusions(t *testing.T) {
	for _, test := range []struct {
		name    string
		debug   bool
		purpose *modelcontract.ProviderOperationPurpose
		output  string
	}{
		{name: "debug disabled", purpose: textutil.Value(modelcontract.ProviderOperationPurposeCompaction)},
		{name: "ordinary generation", debug: true, purpose: textutil.Value(modelcontract.ProviderOperationPurposeGeneration)},
		{name: "reviewer", debug: true, purpose: textutil.Value(modelcontract.ProviderOperationPurposeReviewer)},
		{name: "unspecified purpose", debug: true},
		{name: "successful summary", debug: true, purpose: textutil.Value(modelcontract.ProviderOperationPurposeCompaction),
			output: `{"type":"message","id":"final-1","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"summary"}]}`},
		{name: "tool call rejection", debug: true, purpose: textutil.Value(modelcontract.ProviderOperationPurposeCompaction),
			output: `{"type":"function_call","id":"tool-1","call_id":"call-1","name":"shell","arguments":"{}"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := []json.RawMessage{}
			if test.output != "" {
				output = append(output, json.RawMessage(test.output))
			}
			_, failures, logs := runLocalCompactionDiagnostic(t, output, test.debug, test.purpose)
			if len(failures) != 0 {
				t.Fatalf("unexpected empty compaction diagnostic: %+v", failures)
			}
			if !test.debug && len(logs) != 0 {
				t.Fatal("debug-disabled generation emitted diagnostics")
			}
		})
	}
}

func TestEmptyLocalCompactionDiagnosticsBoundItemsWithoutLosingTotals(t *testing.T) {
	output := make([]json.RawMessage, 0, 42)
	for index := range 40 {
		output = append(output, json.RawMessage(fmt.Sprintf(
			`{"type":"message","id":"commentary-%d","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"abc"}]}`, index)))
	}
	output = append(output, json.RawMessage(`{"type":"message","id":"final-1","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":""}]}`))
	_, failures, _ := runLocalCompactionDiagnostic(t, output, true,
		textutil.Value(modelcontract.ProviderOperationPurposeCompaction))
	if len(failures) != 1 {
		t.Fatalf("diagnostics = %d, want 1", len(failures))
	}
	record := failures[0]
	if record["provider_output_count"] != float64(len(output)) ||
		record["provider_text_bytes"] != float64(120) || record["provider_output_truncated"] != true ||
		record["normalized_output_truncated"] != true ||
		len(record["provider_output_items"].([]any)) > 32 || len(record["normalized_output_items"].([]any)) > 32 {
		t.Fatalf("unbounded or incomplete output evidence: %+v", record)
	}
}

func TestEmptyLocalCompactionDiagnosticsWhitespaceSummary(t *testing.T) {
	response, failures, _ := runLocalCompactionDiagnostic(t,
		[]json.RawMessage{json.RawMessage(`{"type":"message","id":"final-1","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":" \n\t"}]}`)},
		true, textutil.Value(modelcontract.ProviderOperationPurposeCompaction))
	if response.Assistant.Content == nil || strings.TrimSpace(*response.Assistant.Content) != "" || len(failures) != 1 {
		t.Fatalf("whitespace output changed or missing diagnostic: content=%v failures=%d", response.Assistant.Content, len(failures))
	}
	if failures[0]["normalized_assistant_bytes"] != float64(3) || failures[0]["extracted_summary_bytes"] != float64(0) {
		t.Fatalf("missing whitespace extraction evidence: %+v", failures[0])
	}
}

func runLocalCompactionDiagnostic(t *testing.T, output []json.RawMessage, debug bool, purpose *modelcontract.ProviderOperationPurpose) (Response, []map[string]any, []byte) {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	event, err := json.Marshal(map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": "response-empty", "status": "completed", "model": "gpt-6-sol",
			"usage": map[string]any{
				"input_tokens": 100, "output_tokens": 3526, "total_tokens": 3626,
				"output_tokens_details": map[string]any{"reasoning_tokens": 3520},
			},
			"output": output,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string {
			return []string{string(event)}
		})
	}))
	t.Cleanup(server.Close)
	transport := newCanonicalOAuthTestTransport(t, server)
	transport.Debug = debug
	client := NewResponsesClient(transport, nil)
	response, err := client.Generate(context.Background(), Request{
		Model: "gpt-6-sol", ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high",
		SessionID: textutil.Value("session-1"), CodexDispatch: newTestCodexDispatch(t),
		Purpose: purpose,
	}, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("diagnostics changed provider attempt count: %d", requests.Load())
	}
	decoder := json.NewDecoder(&logs)
	var failures []map[string]any
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["kind"] == "empty_local_compaction" {
			failures = append(failures, record)
		}
	}
	return response, failures, logs.Bytes()
}
