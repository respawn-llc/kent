package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"core/server/llm"
	"core/server/tools"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestSuccessfulModelRequestRecordsActivity(t *testing.T) {
	store := mustCreateTestSession(t)
	engine := mustNewTestEngine(t, store, &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
	}}}, newTestToolRegistry(t), Config{Model: "gpt-6-sol"})
	before := time.Now()
	if _, err := engine.SubmitUserMessage(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if activity := store.ContextFacts().LastModelToolActivityAt; activity == nil || activity.Before(before) {
		t.Fatalf("successful model activity = %v", activity)
	}
}

func TestOrdinaryToolCompletionRecordsActivity(t *testing.T) {
	for _, handlerErr := range []error{nil, errors.New("execution failed")} {
		t.Run(fmt.Sprintf("error=%v", handlerErr != nil), func(t *testing.T) {
			store := mustCreateTestSession(t)
			var handler tools.Handler = fakeTool{name: toolspec.ToolExecCommand}
			if handlerErr != nil {
				handler = failingToolHandler{err: handlerErr}
			}
			engine := mustNewTestEngine(t, store, &fakeClient{}, newTestToolRegistry(t, tools.HandlerRegistration{
				ID: toolspec.ToolExecCommand, Handler: handler,
			}), Config{Model: "gpt-6-sol"})
			stepID := runtimeTestStepID("activity")
			restore := setTestActiveStep(engine, stepID)
			defer restore()
			before := time.Now()
			_, err := engine.executeToolCalls(context.Background(), stepID, []llm.ToolCall{{
				ID: "work", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`),
			}})
			if !errors.Is(err, handlerErr) {
				t.Fatal(err)
			}
			if activity := store.ContextFacts().LastModelToolActivityAt; activity == nil || activity.Before(before) {
				t.Fatalf("completed tool activity = %v", activity)
			}
		})
	}
}

func TestModelActivityIncludesSilentResponsesAndExcludesFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		response llm.Response
		err      error
	}{
		{name: "tool calls", response: llm.Response{Assistant: llm.Message{Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{ID: "call", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`)}}}}},
		{name: "reasoning", response: llm.Response{Reasoning: []llm.ReasoningEntry{{Text: "working"}}}},
		{name: "failed", err: errors.New("failed")},
		{name: "canceled", err: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			prior := time.Now().Add(-2 * time.Hour)
			if err := store.RecordModelToolActivity(prior); err != nil {
				t.Fatal(err)
			}
			client := &fakeClient{responses: []llm.Response{test.response}, errors: []error{test.err}}
			engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), Config{Model: "gpt-6-sol"})
			stepID := runtimeTestStepID(test.name)
			restore := setTestActiveStep(engine, stepID)
			defer restore()
			_, err := engine.generateWithRetryPolicy(t.Context(), stepID, engine.connectionSnapshot().client,
				llm.Request{Model: "gpt-6-sol"}, modelRequestSingleAttempt, llm.StreamCallbacks{}, nil)
			if (err != nil) != (test.err != nil) {
				t.Fatalf("generation error = %v", err)
			}
			activity := store.ContextFacts().LastModelToolActivityAt
			if activity == nil || (test.err != nil && !activity.Equal(prior)) ||
				(test.err == nil && !activity.After(prior)) {
				t.Fatalf("activity after %s = %v, prior %v", test.name, activity, prior)
			}
		})
	}
}

func TestActivityWriteFailureSurfacesWithoutReplayingTool(t *testing.T) {
	writer := &failingContextFactWriter{delegate: runtimeTestSessionPersistence, activityErr: errors.New("database failed")}
	engine, store := newContextFactTestEngine(t, writer, &fakeClient{}, newTestToolRegistry(t,
		tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand}}))
	diagnostics := 0
	completions := 0
	engine.cfg.OnEvent = func(event Event) {
		if event.Kind == EventContextFactsPersistFailed {
			diagnostics++
		}
		if event.Kind == EventToolCallCompleted {
			completions++
		}
	}
	stepID := runtimeTestStepID("activity failure")
	restore := setTestActiveStep(engine, stepID)
	defer restore()
	results, err := engine.executeToolCalls(t.Context(), stepID, []llm.ToolCall{{
		ID: "completed", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].IsError || completions != 1 || diagnostics != 1 {
		t.Fatalf("results=%v completions=%d diagnostics=%d", results, completions, diagnostics)
	}
	if store.ContextFacts().LastModelToolActivityAt != nil {
		t.Fatal("failed activity write published")
	}
}
