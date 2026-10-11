package runtime

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcript"
)

func TestFailedAutoCompactionPersistsOneErrorAndPublishesFailureStatus(t *testing.T) {
	client := &fakeCompactionClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("working")},
		ToolCalls: []llm.ToolCall{{
			ID: "call_1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`),
		}},
		Usage: llm.Usage{InputTokens: 390_000, WindowTokens: 400_000},
	}}}
	var events []Event
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t, tools.HandlerRegistration{
		ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand},
	}), Config{
		Model: "gpt-6-sol", CompactionMode: "local",
		OnEvent: func(event Event) { events = append(events, event) },
	})

	if _, err := engine.SubmitUserMessage(t.Context(), "run tools"); err == nil {
		t.Fatal("empty local compaction summary succeeded")
	}
	assertSingleDurableCompactionFailure(t, engine, events, compactionModeAuto)
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 2 {
		t.Fatalf("model calls = %d, want generation and one failed local compaction", len(client.calls))
	}
}

func TestFailedManualCompactionPersistsOneErrorAndPublishesFailureStatus(t *testing.T) {
	client := &fakeCompactionClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
	}}}
	var events []Event
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{
		Model: "gpt-6-sol", CompactionMode: "local",
		OnEvent: func(event Event) { events = append(events, event) },
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "input"); err != nil {
		t.Fatalf("complete initial Agent Step: %v", err)
	}
	scheduleManualCompactionAndWait(t, engine)
	assertSingleDurableCompactionFailure(t, engine, events, compactionModeManual)
}

func TestFailedEagerCompactionPersistsOneErrorWithoutFailingSuccessfulTurn(t *testing.T) {
	client := &fakeCompactionClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
		Usage:     llm.Usage{InputTokens: 390_000, WindowTokens: 400_000},
	}}}
	var eventsMu sync.Mutex
	var events []Event
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{
		Model: "gpt-6-sol", CompactionMode: "local",
		OnEvent: func(event Event) {
			eventsMu.Lock()
			defer eventsMu.Unlock()
			events = append(events, event)
		},
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "input"); err != nil {
		t.Fatalf("preceding successful turn failed: %v", err)
	}
	waitEngineLifecycleTasks(t, engine)
	eventsMu.Lock()
	completedEvents := slices.Clone(events)
	eventsMu.Unlock()
	assertSingleDurableCompactionFailure(t, engine, completedEvents, compactionModeAuto)
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 2 {
		t.Fatalf("model calls = %d, want generation and one failed eager compaction", len(client.calls))
	}
}

func TestFailedManualCompactionPublishesStatusWhenErrorPersistenceFails(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &compactionCallbackClient{fakeCompactionClient: &fakeCompactionClient{
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
		}},
		compactionErr: llm.ErrInvalidRequest,
	}}
	var events []Event
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), Config{
		Model: "gpt-6-sol", CompactionMode: "native",
		OnEvent: func(event Event) { events = append(events, event) },
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "input"); err != nil {
		t.Fatalf("complete initial Agent Step: %v", err)
	}
	client.onCompact = func() {
		blocker := mustBlockTestEventLogAppends(t, store)
		t.Cleanup(func() {
			if err := blocker.Restore(); err != nil {
				t.Errorf("restore event-log appends: %v", err)
			}
		})
	}
	scheduleManualCompactionAndWait(t, engine)
	failures := 0
	for _, event := range events {
		if event.Kind == EventCompactionFailed {
			failures++
			if event.Compaction == nil || event.Compaction.Error == "" {
				t.Fatalf("missing failure details: %+v", event)
			}
		}
	}
	if failures != 1 {
		t.Fatalf("compaction failure events = %d, want one", failures)
	}
	if engine.ChatSnapshot().StreamingError == "" {
		t.Fatal("failed error persistence left no operator-visible diagnostic")
	}
}

func TestFailedStandaloneCompactionPersistsOneErrorAndPublishesFailureStatus(t *testing.T) {
	for _, test := range []struct {
		name string
		mode compactionMode
		run  func(*Engine) error
	}{
		{
			name: "pre-submit", mode: compactionModeManual,
			run: func(engine *Engine) error {
				return engine.CompactContextForPreSubmit(t.Context(), strings.Repeat("next ", 400_000))
			},
		},
		{
			name: "workflow", mode: compactionModeWorkflowPostCompletion,
			run: func(engine *Engine) error {
				_, err := engine.CompactContextForWorkflowPostCompletion(t.Context())
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeCompactionClient{responses: []llm.Response{{
				Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
			}}}
			var events []Event
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{
				Model: "gpt-6-sol", CompactionMode: "local",
				OnEvent: func(event Event) { events = append(events, event) },
			})
			if _, err := engine.SubmitUserMessage(t.Context(), "input"); err != nil {
				t.Fatalf("complete initial Agent Step: %v", err)
			}
			if err := test.run(engine); err == nil {
				t.Fatal("empty local compaction summary succeeded")
			}
			assertSingleDurableCompactionFailure(t, engine, events, test.mode)
		})
	}
}

func assertSingleDurableCompactionFailure(t *testing.T, engine *Engine, events []Event, mode compactionMode) {
	t.Helper()
	failures := 0
	for _, event := range events {
		if event.Kind != EventCompactionFailed {
			continue
		}
		failures++
		if event.Compaction == nil || event.Compaction.Mode != string(mode) || event.Compaction.Error == "" {
			t.Fatalf("missing typed compaction failure details: %+v", event)
		}
	}
	if failures != 1 {
		t.Fatalf("compaction failure status events = %d, want one", failures)
	}
	errors := 0
	for _, row := range mustTranscriptHydrationSnapshot(t, engine).CommittedRows {
		if row.Notice == nil || row.Notice.Severity != transcript.NoticeSeverityError {
			continue
		}
		errors++
		if row.Notice.DiagnosticDetail == "" && (row.Notice.LegacyText == nil || *row.Notice.LegacyText == "") {
			t.Fatalf("durable error has no diagnostic: %+v", row)
		}
	}
	if errors != 1 {
		t.Fatalf("durable compaction errors = %d, want one", errors)
	}
}
