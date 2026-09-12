package runtime

import (
	"core/server/llm"
	"core/server/tools"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcript"
	"encoding/json"
	"strings"
	"testing"
)

func TestTranscriptEntriesFromEventBuildsToolCallFallbackWithoutPresentation(t *testing.T) {
	t.Parallel()
	entries := TranscriptEntriesFromEvent(Event{
		Kind: EventToolCallStarted,
		ToolCall: &llm.ToolCall{
			ID:    "call-1",
			Name:  string(toolspec.ToolExecCommand),
			Input: json.RawMessage(`{"command":"pwd"}`),
		},
	})
	if len(entries) != 1 {
		t.Fatalf("expected one transcript entry, got %+v", entries)
	}
	entry := entries[0]
	if entry.Role != "tool_call" {
		t.Fatalf("entry role = %q, want tool_call", entry.Role)
	}
	if entry.Text != "pwd" {
		t.Fatalf("entry text = %q, want pwd", entry.Text)
	}
	if entry.ToolCall == nil || !entry.ToolCall.IsShell {
		t.Fatalf("expected rebuilt shell tool metadata, got %+v", entry.ToolCall)
	}
	if entry.ToolCall.Command != "pwd" {
		t.Fatalf("tool metadata command = %q, want pwd", entry.ToolCall.Command)
	}
}

func TestNormalizeToolCallForTranscriptRepairsMalformedPresentation(t *testing.T) {
	t.Parallel()
	normalized := normalizeToolCallForTranscript(llm.ToolCall{
		ID:           "call-1",
		Name:         string(toolspec.ToolExecCommand),
		Presentation: json.RawMessage(`{"broken":`),
		Input:        json.RawMessage(`{"command":"pwd"}`),
	}, "/tmp")
	meta := decodeToolCallMeta(normalized)
	if meta == nil {
		t.Fatal("expected rebuilt tool presentation metadata")
	}
	if !meta.IsShell {
		t.Fatalf("expected rebuilt shell metadata, got %+v", meta)
	}
	if meta.Command != "pwd" {
		t.Fatalf("rebuilt command = %q, want pwd", meta.Command)
	}
}

func TestTranscriptEntriesFromEventEmitsVisibleToolCompletionEntriesForOrdinaryAndTriggerHandoffTools(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		result tools.Result
	}{
		{
			name: "ordinary shell result",
			result: tools.Result{
				CallID:        "call-shell-1",
				Name:          toolspec.ToolExecCommand,
				Output:        json.RawMessage(`{"output":"/tmp","exit_code":0,"truncated":false}`),
				CondensedText: textutil.Value("compact shell result"),
			},
		},
		{
			name: "trigger handoff synthetic success result",
			result: tools.Result{
				CallID: "call-handoff-1",
				Name:   toolspec.ToolTriggerHandoff,
				Output: json.RawMessage(`""`),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			entries := TranscriptEntriesFromEvent(Event{
				Kind:       EventToolCallCompleted,
				ToolResult: &tc.result,
			})
			if len(entries) != 1 {
				t.Fatalf("expected one visible transcript entry, got %+v", entries)
			}
			entry := entries[0]
			if entry.Role != "tool_result_ok" {
				t.Fatalf("entry role = %q, want tool_result_ok", entry.Role)
			}
			if entry.ToolCallID != tc.result.CallID {
				t.Fatalf("entry tool call id = %q, want %q", entry.ToolCallID, tc.result.CallID)
			}
			expectedCondensedText := ""
			if tc.result.CondensedText != nil {
				expectedCondensedText = strings.TrimSpace(*tc.result.CondensedText)
			}
			if entry.CondensedText != expectedCondensedText {
				t.Fatalf("entry condensed text = %q, want %v", entry.CondensedText, tc.result.CondensedText)
			}
		})
	}
}

func TestCustomToolCallOutputProjectsAsRegularToolResultEntry(t *testing.T) {
	t.Parallel()
	msg := llm.Message{
		Role:        llm.RoleTool,
		MessageType: textutil.Value(llm.MessageTypeCustomToolCallOutput),
		ToolCallID:  textutil.Value("call-patch-1"),
		Name:        textutil.Value(string(toolspec.ToolPatch)),
		Content:     textutil.Value(`"patched"`),
	}

	entries := VisibleChatEntriesFromMessage(msg)
	if len(entries) != 1 {
		t.Fatalf("custom tool output entries = %+v, want one tool result entry", entries)
	}
	entry := entries[0]
	if entry.Role != "tool_result_ok" || entry.Visibility != transcript.EntryVisibilityOngoingCollapsed {
		t.Fatalf("custom tool output entry role/visibility = %q/%q, want regular collapsed tool result", entry.Role, entry.Visibility)
	}
	if msg.ToolCallID == nil || entry.ToolCallID != *msg.ToolCallID {
		t.Fatalf("custom tool output call id = %q, want %v", entry.ToolCallID, msg.ToolCallID)
	}
}

func TestWebSearchCompletionContent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		output      string
		failed      bool
		wantFailure bool
		wantDetail  bool
	}{
		{"populated", `{"action":{"type":"search"},"results":[{"title":"Result","url":"https://example.com","snippet":"excluded"}]}`, false, false, true},
		{"absent", `{"action":{"type":"search"}}`, false, false, false},
		{"malformed", `{"action":{"type":"search"},"results":[{"url":42}]}`, false, true, false},
		{"provider failure", `{"error":"provider failure"}`, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := tools.Result{CallID: "search-1", Name: toolspec.ToolWebSearch, Output: json.RawMessage(tc.output), IsError: tc.failed}
			entries := TranscriptEntriesFromEvent(Event{Kind: EventToolCallCompleted, ToolResult: &result})
			if len(entries) != 1 {
				t.Fatalf("lost completion: %+v", entries)
			}
			entry := entries[0]
			if (entry.Role == "tool_result_error") != tc.wantFailure || (entry.WebSearch != nil) != tc.wantDetail {
				t.Fatalf("incorrect projection: %+v", entry)
			}
			if tc.wantFailure && entry.Text == "" {
				t.Fatal("failure diagnostic lost")
			}
			if !tc.wantFailure && entry.Text != "" {
				t.Fatalf("successful raw output exposed: %q", entry.Text)
			}
			if string(result.Output) != tc.output || result.IsError != tc.failed {
				t.Fatal("saved authority changed")
			}
		})
	}
}

func TestTranscriptEntriesFromEventOmitsPrePersistCompactionStatusRows(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		evt  Event
	}{
		{
			name: "compaction completed",
			evt: Event{
				Kind: EventCompactionCompleted,
				Compaction: &CompactionStatus{
					Mode:  "auto",
					Count: 1,
				},
			},
		},
		{
			name: "compaction failed",
			evt: Event{
				Kind: EventCompactionFailed,
				Compaction: &CompactionStatus{
					Mode:  "manual",
					Error: "quota exceeded",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if entries := TranscriptEntriesFromEvent(tc.evt); len(entries) != 0 {
				t.Fatalf("expected no transcript entries for pre-persist compaction status, got %+v", entries)
			}
		})
	}
}
