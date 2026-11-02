package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
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
		{name: "tool calls", response: llm.Response{Assistant: llm.Message{Role: llm.RoleAssistant},
			ToolCalls: []llm.ToolCall{{ID: "call", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`)}}}},
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

func TestBackgroundCompletionUsesFinishTimeAndKeepsNewerActivity(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")}}}}
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), Config{Model: "gpt-6-sol", IdleResumeReminderSeconds: textutil.Value(3600)})
	finished := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	event := BackgroundShellEvent{Type: BackgroundShellEventCompleted, ID: "background", FinishedAt: &finished}
	if err := engine.RecordBackgroundShellUpdate(event); err != nil {
		t.Fatal(err)
	}
	if got := store.ContextFacts().LastModelToolActivityAt; got == nil || !got.Equal(finished) {
		t.Fatalf("background activity = %v, want finish %v", got, finished)
	}
	newer := finished.Add(time.Minute)
	if err := store.RecordModelToolActivity(newer); err != nil {
		t.Fatal(err)
	}
	if err := engine.RecordBackgroundShellUpdate(event); err != nil {
		t.Fatal(err)
	}
	if got := store.ContextFacts().LastModelToolActivityAt; got == nil || !got.Equal(newer) {
		t.Fatalf("delayed completion replaced newer activity: %v", got)
	}
	if err := engine.steerRuntime(steerMessagesWithPersistenceIntent(steeringPriorityNormal, steeringMessageEventDefault, true,
		[]llm.Message{{Role: llm.RoleAssistant, Content: textutil.Value("prior work")}})); err != nil {
		t.Fatal(err)
	}
	engine.transcriptRuntimeState().now = func() time.Time { return newer }
	if _, err := runStepLoopInActiveTestRun(t, t.Context(), engine); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 || idleReminderCount(client.calls[0]) != 0 {
		t.Fatal("background completion caused a false sleep reminder")
	}
}

type approvalActivityTool struct{ broker *tools.AskQuestionBroker }

func (h approvalActivityTool) Call(ctx context.Context, call tools.Call) (tools.Result, error) {
	resolution, err := h.broker.Ask(ctx, tools.AskQuestionRequest{
		Question: "Allow tool execution?",
		Approval: true, ToolCallID: call.ID, StepID: call.StepID,
		ApprovalOptions: []tools.AskQuestionApprovalOption{
			{Decision: tools.AskQuestionApprovalDecisionAllowOnce},
			{Decision: tools.AskQuestionApprovalDecisionDeny},
		},
	})
	if err != nil {
		return tools.ErrorResult(call, err.Error()), nil
	}
	if resolution.(tools.AskQuestionApproval).Decision == tools.AskQuestionApprovalDecisionDeny {
		return tools.ErrorResult(call, "denied"), nil
	}
	return tools.Result{Output: json.RawMessage(`{"ok":true}`)}, nil
}

func TestAccessApprovalWaitOnlyCountsSubsequentExecutedWork(t *testing.T) {
	for _, test := range []struct {
		name         string
		decision     tools.AskQuestionApprovalDecision
		err          error
		wantActivity bool
	}{
		{name: "denied", decision: tools.AskQuestionApprovalDecisionDeny},
		{name: "failed wait", err: errors.New("wait failed")},
		{name: "allowed", decision: tools.AskQuestionApprovalDecisionAllowOnce, wantActivity: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			broker := tools.NewAskQuestionBroker()
			broker.SetLifecycleAskHandler(func(context.Context, tools.AskQuestionRequest) (tools.AskQuestionResolution, error) {
				if test.err != nil {
					return nil, test.err
				}
				return tools.AskQuestionApproval{Decision: test.decision}, nil
			})
			store := mustCreateTestSession(t)
			engine := mustNewTestEngine(t, store, &fakeClient{}, newTestToolRegistry(t,
				tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: approvalActivityTool{broker}},
			), Config{Model: "gpt-6-sol"})
			stepID := runtimeTestStepID(test.name)
			restore := setTestActiveStep(engine, stepID)
			defer restore()
			if _, err := engine.executeToolCalls(t.Context(), stepID, []llm.ToolCall{{
				ID: "approval", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`),
			}}); err != nil {
				t.Fatal(err)
			}
			if got := store.ContextFacts().LastModelToolActivityAt != nil; got != test.wantActivity {
				t.Fatalf("has activity = %v, want %v", got, test.wantActivity)
			}
		})
	}
}

type longActivityTool struct{ clock *atomic.Int64 }

func (h longActivityTool) Call(context.Context, tools.Call) (tools.Result, error) {
	h.clock.Add(int64(6 * time.Hour))
	return tools.Result{Output: json.RawMessage(`{"ok":true}`)}, nil
}

func TestLongToolWorkAndAccessWaitsAtGenerationBoundary(t *testing.T) {
	for _, test := range []struct {
		name         string
		decision     *tools.AskQuestionApprovalDecision
		wantReminder int
	}{
		{name: "six hour ordinary tool"},
		{name: "allowed access", decision: textutil.Value(tools.AskQuestionApprovalDecisionAllowOnce)},
		{name: "denied access", decision: textutil.Value(tools.AskQuestionApprovalDecisionDeny), wantReminder: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var clock atomic.Int64
			start := time.Now().Add(-12 * time.Hour).Truncate(time.Millisecond)
			clock.Store(start.UnixNano())
			var handler tools.Handler = longActivityTool{&clock}
			if test.decision != nil {
				broker := tools.NewAskQuestionBroker()
				broker.SetLifecycleAskHandler(func(context.Context, tools.AskQuestionRequest) (tools.AskQuestionResolution, error) {
					clock.Add(int64(6 * time.Hour))
					return tools.AskQuestionApproval{Decision: *test.decision}, nil
				})
				handler = approvalActivityTool{broker}
			}
			client := &fakeClient{responses: []llm.Response{
				{Assistant: llm.Message{Role: llm.RoleAssistant}, ToolCalls: []llm.ToolCall{{ID: "work", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`)}}},
				{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")}},
			}}
			if test.wantReminder == 1 {
				client.responses[1] = llm.Response{Assistant: llm.Message{Role: llm.RoleAssistant}, ToolCalls: []llm.ToolCall{{
					ID: "second-wait", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`),
				}}}
				client.responses = append(client.responses, llm.Response{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")}})
			}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: handler}),
				Config{Model: "gpt-6-sol", IdleResumeReminderSeconds: textutil.Value(3600)})
			engine.transcriptRuntimeState().now = func() time.Time { return time.Unix(0, clock.Load()) }
			if _, err := engine.SubmitUserMessage(t.Context(), "work"); err != nil {
				t.Fatal(err)
			}
			if len(client.calls) != 2+test.wantReminder {
				t.Fatalf("requests=%d", len(client.calls))
			}
			if got := idleReminderCount(client.calls[1]); got != test.wantReminder {
				t.Fatalf("reminders=%d, want %d", got, test.wantReminder)
			}
			if test.wantReminder == 1 && idleReminderCount(client.calls[2]) != 2 {
				t.Fatal("an earlier reminder suppressed a new gap after intervening model work in the same live execution")
			}
		})
	}
}
