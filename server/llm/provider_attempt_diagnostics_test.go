package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"core/shared/config"
	"core/shared/textutil"
)

type providerAttemptLog struct {
	Kind          string  `json:"kind"`
	AttemptID     string  `json:"attempt_id"`
	SessionID     string  `json:"session_id"`
	ConnectionID  string  `json:"connection_id"`
	AccountID     string  `json:"account_id"`
	Model         string  `json:"model"`
	CacheKey      string  `json:"cache_key"`
	Outcome       string  `json:"outcome"`
	UsageReported bool    `json:"usage_reported"`
	InputTokens   *int    `json:"input_tokens"`
	CachedTokens  *int    `json:"cached_input_tokens"`
	OutputTokens  *int    `json:"output_tokens"`
	HTTPStatus    *int    `json:"http_status"`
	ResponseID    *string `json:"response_id"`
	Operation     string  `json:"operation"`
}

func TestProviderAttemptDiagnosticsPreserveIdentityAndReportedUsage(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		event    string
		failed   bool
		hasUsage bool
		canceled bool
		compact  bool
	}{
		{
			name: "completed with known zero cache", status: http.StatusOK, hasUsage: true,
			event: `{"type":"response.completed","response":{"id":"response-1","model":"gpt-6-sol","usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens":4,"total_tokens":14},"output":[]}}`,
		},
		{
			name: "failed before usage", status: http.StatusServiceUnavailable, failed: true,
			event: `{"error":{"message":"overloaded","type":"server_error","code":"server_error"}}`,
		},
		{
			name: "incomplete with reported usage", status: http.StatusOK, failed: true, hasUsage: true,
			event: `{"type":"response.incomplete","response":{"id":"response-1","model":"gpt-6-sol","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens":4,"total_tokens":14},"output":[]}}`,
		},
		{
			name: "caller cancellation", status: http.StatusOK, failed: true, canceled: true,
			event: `{"type":"response.created","response":{"id":"response-1","model":"gpt-6-sol","output":[]}}`,
		},
		{
			name: "compaction with usage", status: http.StatusOK, hasUsage: true, compact: true,
			event: `{"type":"response.completed","response":{"id":"response-1","model":"gpt-6-sol","usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens":4,"total_tokens":14},"output":[{"type":"compaction","id":"compaction-1","encrypted_content":"encrypted-checkpoint"}]}}`,
		},
		{
			name: "failed compaction without usage", status: http.StatusServiceUnavailable, failed: true, compact: true,
			event: `{"error":{"message":"overloaded","type":"server_error","code":"server_error"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.status != http.StatusOK {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(test.status)
					_, _ = fmt.Fprint(w, test.event)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", test.event)
			}))
			t.Cleanup(server.Close)
			transport := newCanonicalOAuthTestTransport(t, server)
			transport.ConnectionID = textutil.Value(config.ConnectionID("personal"))
			request := testCodexResponsesRequest(newTestCodexDispatch(t))
			request.PromptCacheKey = *request.SessionID
			for range 2 {
				ctx, cancel := context.WithCancel(context.Background())
				var err error
				if test.compact {
					_, err = transport.Compact(ctx, request)
				} else {
					callbacks := StreamCallbacks{}
					if test.canceled {
						callbacks.OnStreamActivity = cancel
					}
					_, err = transport.Generate(ctx, request, callbacks)
				}
				cancel()
				if (err != nil) != test.failed {
					t.Fatalf("Generate error = %v, want failed=%t", err, test.failed)
				}
			}
			decoder := json.NewDecoder(&logs)
			attempts := map[string][]providerAttemptLog{}
			for decoder.More() {
				var record providerAttemptLog
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				if record.AttemptID != "" {
					attempts[record.AttemptID] = append(attempts[record.AttemptID], record)
				}
			}
			if len(attempts) != 2 {
				t.Fatalf("observed physical attempts = %d, want 2", len(attempts))
			}
			for _, records := range attempts {
				if len(records) != 2 || records[0].Kind != "started" || records[1].Kind != "finished" {
					t.Fatalf("attempt lifecycle = %+v, want started and finished", records)
				}
				finished := records[1]
				if finished.ConnectionID != "personal" || finished.AccountID != "acc-1" ||
					finished.SessionID != *request.SessionID || finished.Model != request.Model ||
					finished.CacheKey != request.PromptCacheKey {
					t.Fatalf("dispatch identity changed: %+v", finished)
				}
				outcome := "completed"
				if test.failed {
					outcome = "failed"
				}
				if test.canceled {
					outcome = "canceled"
				}
				if finished.Outcome != outcome || finished.HTTPStatus == nil || *finished.HTTPStatus != test.status {
					t.Fatalf("attempt outcome = %+v", finished)
				}
				operation := "generation"
				if test.compact {
					operation = "compaction"
				}
				if finished.Operation != operation {
					t.Fatalf("operation = %s, want %s", finished.Operation, operation)
				}
				if finished.UsageReported != test.hasUsage {
					t.Fatalf("usage reported = %t, want %t", finished.UsageReported, test.hasUsage)
				}
				if !test.hasUsage {
					if finished.InputTokens != nil || finished.CachedTokens != nil || finished.OutputTokens != nil {
						t.Fatal("unreported usage must remain unknown, not zero")
					}
					continue
				}
				if finished.InputTokens == nil || *finished.InputTokens != 10 ||
					finished.CachedTokens == nil || *finished.CachedTokens != 0 ||
					finished.OutputTokens == nil || *finished.OutputTokens != 4 ||
					finished.ResponseID == nil || *finished.ResponseID != "response-1" {
					t.Fatalf("reported usage = %+v", finished)
				}
			}
		})
	}
}
