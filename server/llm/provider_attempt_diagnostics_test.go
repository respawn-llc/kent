package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"core/shared/config"
	"core/shared/textutil"
)

func TestAssistantConflictDiagnosticsBoundContentAndPreserveFailure(t *testing.T) {
	for _, debug := range []bool{false, true} {
		for _, test := range []struct {
			name              string
			prefix            string
			streamed          string
			completed         string
			commonPrefixBytes int
		}{
			{name: "early", streamed: "s", completed: "c"},
			{name: "late", prefix: strings.Repeat("p", 2048), streamed: "s", completed: "c", commonPrefixBytes: 2048},
			{name: "split UTF-8", streamed: "é", completed: "ê", commonPrefixBytes: 1},
			{name: "completed empty", streamed: "s"},
			{name: "completed shorter", prefix: strings.Repeat("p", 2048), streamed: "s", commonPrefixBytes: 2048},
		} {
			t.Run(fmt.Sprintf("debug=%t/%s", debug, test.name), func(t *testing.T) {
				streamed := test.prefix + test.streamed + strings.Repeat("s", 2048)
				completed := test.prefix + test.completed
				if test.completed != "" {
					completed += strings.Repeat("c", 2048)
				}
				var logs bytes.Buffer
				previous := slog.Default()
				slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
				t.Cleanup(func() { slog.SetDefault(previous) })
				event := func(value any) string {
					raw, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					return string(raw)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string {
						return []string{
							`{"type":"response.created","response":{"id":"diagnostic-response","output":[]}}`,
							event(map[string]any{"type": "response.output_text.delta", "output_index": 1, "delta": streamed}),
							event(map[string]any{"type": "response.output_item.done", "output_index": 1, "item": map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": completed}}}}),
						}
					})
				}))
				t.Cleanup(server.Close)
				transport := newCanonicalOAuthTestTransport(t, server)
				transport.Debug = debug
				var delivered string
				_, err := transport.Generate(t.Context(), testCodexResponsesRequest(newTestCodexDispatch(t)), StreamCallbacks{
					OnAssistantDelta:          func(delta AssistantDelta) { delivered += delta.Text },
					OnAssistantSourceComplete: func(AssistantSource) { t.Error("conflicting source completed") },
				})
				var providerErr *ProviderAPIError
				if !errors.As(err, &providerErr) || providerErr.Code != UnifiedErrorCodeProviderContract {
					t.Fatalf("conflict failure changed: %v", err)
				}
				if delivered != streamed {
					t.Fatal("diagnostics changed streamed content")
				}
				decoder := json.NewDecoder(&logs)
				found := 0
				for decoder.More() {
					var record struct {
						Kind              string `json:"kind"`
						SessionID         string `json:"session_id"`
						ResponseID        string `json:"response_id"`
						OutputIndex       int64  `json:"output_index"`
						StreamedBytes     int    `json:"streamed_bytes"`
						CompletedBytes    int    `json:"completed_bytes"`
						CommonPrefixBytes int    `json:"common_prefix_bytes"`
						ExcerptOffset     int    `json:"excerpt_offset"`
						StreamedExcerpt   []byte `json:"streamed_excerpt"`
						CompletedExcerpt  []byte `json:"completed_excerpt"`
					}
					if err := decoder.Decode(&record); err != nil {
						t.Fatal(err)
					}
					if record.Kind != "assistant_text_conflict" {
						continue
					}
					found++
					if record.SessionID != "session-1" || record.ResponseID != "diagnostic-response" || record.OutputIndex != 1 {
						t.Fatalf("lost source identity: %+v", record)
					}
					if record.StreamedBytes != len(streamed) || record.CompletedBytes != len(completed) || record.CommonPrefixBytes != test.commonPrefixBytes {
						t.Fatalf("incorrect conflict coordinates: %+v", record)
					}
					if len(record.StreamedExcerpt) > 256 || len(record.CompletedExcerpt) > 256 {
						t.Fatal("unbounded private text logged")
					}
					if !bytes.Equal(record.StreamedExcerpt, []byte(streamed[record.ExcerptOffset:record.ExcerptOffset+len(record.StreamedExcerpt)])) || !bytes.Equal(record.CompletedExcerpt, []byte(completed[record.ExcerptOffset:record.ExcerptOffset+len(record.CompletedExcerpt)])) {
						t.Fatal("excerpt bytes changed")
					}
					if record.ExcerptOffset > record.CommonPrefixBytes || record.ExcerptOffset+len(record.StreamedExcerpt) <= record.CommonPrefixBytes {
						t.Fatal("excerpt omits mismatch")
					}
				}
				want := 0
				if debug {
					want = 1
				}
				if found != want {
					t.Fatalf("conflict diagnostics = %d, want %d", found, want)
				}
				if !debug && logs.Len() != 0 {
					t.Fatal("debug-disabled content logging")
				}
			})
		}
	}
}

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
		name        string
		status      int
		event       string
		failed      bool
		hasUsage    bool
		canceled    bool
		compact     bool
		debugOff    bool
		errorStatus *int
	}{
		{
			name: "generation without debug", status: http.StatusOK, debugOff: true,
			event: `{"type":"response.completed","response":{"id":"response-1","model":"gpt-6-sol","output":[]}}`,
		},
		{
			name: "compaction without debug", status: http.StatusOK, debugOff: true, compact: true,
			event: `{"type":"response.completed","response":{"id":"response-1","model":"gpt-6-sol","output":[{"type":"compaction","id":"compaction-1","encrypted_content":"encrypted-checkpoint"}]}}`,
		},
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
			name: "protocol rejection without status", status: http.StatusOK, failed: true,
			event: `{"type":"error","error":{"code":"insufficient_quota","type":"allowance","message":"unavailable"}}`,
		},
		{
			name: "protocol rate limit", status: http.StatusOK, failed: true, errorStatus: textutil.Value(429),
			event: `{"type":"error","status":429,"error":{"code":"rate_limit_exceeded","type":"rate_limit","message":"retry later"}}`,
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
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.status != http.StatusOK {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(test.status)
					_, _ = fmt.Fprint(w, test.event)
					return
				}
				if !test.compact {
					serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string {
						return []string{test.event}
					})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if !test.failed {
					_, _ = fmt.Fprint(w, `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"compaction","id":"compaction-1","encrypted_content":"encrypted-checkpoint"}}`+"\n\n")
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", test.event)
			}))
			t.Cleanup(server.Close)
			transport := newCanonicalOAuthTestTransport(t, server)
			transport.Debug = !test.debugOff
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
			if test.debugOff {
				if len(attempts) != 0 {
					t.Fatalf("diagnostics emitted with debug disabled: %d attempts", len(attempts))
				}
				return
			}
			if len(attempts) != 2 {
				t.Fatalf("observed physical attempts = %d, want 2", len(attempts))
			}
			upgrades := 0
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
				if finished.Outcome != outcome {
					t.Fatalf("attempt outcome = %+v", finished)
				}
				if test.compact || test.status != http.StatusOK {
					if finished.HTTPStatus == nil || *finished.HTTPStatus != test.status {
						t.Fatalf("HTTP attempt status = %+v", finished.HTTPStatus)
					}
				} else if test.failed {
					if (finished.HTTPStatus == nil) != (test.errorStatus == nil) || (finished.HTTPStatus != nil && *finished.HTTPStatus != *test.errorStatus) {
						t.Fatalf("failure diagnostic status=%v expected=%v", finished.HTTPStatus, test.errorStatus)
					}
				} else if finished.HTTPStatus != nil {
					if *finished.HTTPStatus != http.StatusSwitchingProtocols {
						t.Fatalf("WebSocket handshake status = %d", *finished.HTTPStatus)
					}
					upgrades++
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
			if !test.compact && test.status == http.StatusOK {
				expectedUpgrades := 1
				if test.failed {
					expectedUpgrades = 0
				}
				if upgrades != expectedUpgrades {
					t.Fatalf("handshake attempts = %d, want %d", upgrades, expectedUpgrades)
				}
			}
		})
	}
}
