package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"builder/server/llm"
	"builder/server/tools"
	"builder/shared/toolspec"
)

func TestExecuteToolCallsCanonicalizesEditAliases(t *testing.T) {
	store := mustCreateTestSession(t)
	var events []Event
	eng, err := New(store, &fakeClient{}, tools.NewRegistry(capturingTool{name: toolspec.ToolEdit}), Config{
		Model:        "claude",
		EnabledTools: []toolspec.ID{toolspec.ToolEdit},
		OnEvent: func(evt Event) {
			events = append(events, evt)
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	results, err := eng.executeToolCalls(context.Background(), "step", []llm.ToolCall{{
		ID:    "call-replace",
		Name:  "replace",
		Input: json.RawMessage(`{"path":"a.go","old_string":"old","new_string":"new"}`),
	}})
	if err != nil {
		t.Fatalf("execute tool calls: %v", err)
	}
	if len(results) != 1 || results[0].Name != toolspec.ToolEdit {
		t.Fatalf("results = %+v, want canonical edit", results)
	}
	var started *llm.ToolCall
	for _, evt := range events {
		if evt.Kind == EventToolCallStarted && evt.ToolCall != nil && evt.ToolCall.ID == "call-replace" {
			started = evt.ToolCall
		}
	}
	if started == nil {
		t.Fatalf("events = %+v, want started event", events)
	}
	meta := transcriptToolCallMeta(*started, store.Meta().WorkspaceRoot)
	if got := meta.ToolName; got != string(toolspec.ToolEdit) {
		t.Fatalf("started tool name = %q, want edit", got)
	}
	if got := meta.Command; got != "a.go" {
		t.Fatalf("started text = %q, want a.go", got)
	}
}

func TestExecuteToolCallsAcceptsCustomEditJSONAndRejectsPlainText(t *testing.T) {
	store := mustCreateTestSession(t)
	eng, err := New(store, &fakeClient{}, tools.NewRegistry(capturingTool{name: toolspec.ToolEdit}), Config{Model: "claude", EnabledTools: []toolspec.ID{toolspec.ToolEdit}})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	okResults, err := eng.executeToolCalls(context.Background(), "step-json", []llm.ToolCall{{
		ID:          "call-json",
		Name:        "edit",
		Custom:      true,
		CustomInput: `{"path":"a.go","old_string":"old","new_string":"new"}`,
	}})
	if err != nil {
		t.Fatalf("execute json custom tool call: %v", err)
	}
	if len(okResults) != 1 || okResults[0].IsError {
		t.Fatalf("json custom results = %+v, want success", okResults)
	}

	badResults, err := eng.executeToolCalls(context.Background(), "step-text", []llm.ToolCall{{
		ID:          "call-text",
		Name:        "edit",
		Custom:      true,
		CustomInput: "not json",
	}})
	if err != nil {
		t.Fatalf("execute text custom tool call: %v", err)
	}
	if len(badResults) != 1 || !badResults[0].IsError {
		t.Fatalf("plain custom results = %+v, want error", badResults)
	}
}

type capturingTool struct {
	name toolspec.ID
}

func (t capturingTool) Name() toolspec.ID { return t.name }

func (t capturingTool) Call(_ context.Context, c tools.Call) (tools.Result, error) {
	var payload map[string]any
	if err := json.Unmarshal(c.Input, &payload); err != nil || payload == nil {
		out, _ := json.Marshal("Edit failed: expected JSON object input.")
		// Tool-logic failures are returned in Result.IsError, not as a Go error.
		//nolint:nilerr
		return tools.Result{CallID: c.ID, Name: c.Name, Output: out, IsError: true, Summary: "Edit failed: expected JSON object input."}, nil
	}
	out, _ := json.Marshal("ok")
	return tools.Result{CallID: c.ID, Name: c.Name, Output: out}, nil
}
