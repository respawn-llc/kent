package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"core/server/llm"
	"core/server/tools"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type questionBatchTestHandler struct{}

type questionBatchCompletionHandler struct {
	pending chan struct{}
	release chan struct{}
}

func (h questionBatchCompletionHandler) QuestionsEnabled() bool { return true }

func (h questionBatchCompletionHandler) Call(ctx context.Context, call tools.Call) (tools.Result, error) {
	if call.ID == "pending" {
		close(h.pending)
		select {
		case <-h.release:
		case <-ctx.Done():
			return tools.Result{}, context.Cause(ctx)
		}
	}
	return tools.Result{CallID: call.ID, Name: call.Name, Output: json.RawMessage(`{}`)}, nil
}

func TestSkippedQuestionCandidatePublishesFinishedBeforeEarlierQuestionAnswer(t *testing.T) {
	handler := questionBatchCompletionHandler{pending: make(chan struct{}), release: make(chan struct{})}
	finished := make(chan string, 2)
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{},
		newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolAskQuestion, Handler: handler}),
		Config{Model: "gpt-6-sol", EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}, OnEvent: func(event Event) {
			if event.Kind == EventQuestionCandidateFinished && event.FinishedQuestionCandidate != nil {
				finished <- string(*event.FinishedQuestionCandidate)
			}
		}})
	stepID := runtimeTestStepID("skipped-question")
	restore := setTestActiveStep(engine, stepID)
	defer restore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := engine.executeToolCalls(ctx, stepID, []llm.ToolCall{
			{ID: "pending", Name: string(toolspec.ToolAskQuestion), Input: json.RawMessage(`{"question":"Proceed?"}`)},
			{ID: "skipped", Name: string(toolspec.ToolAskQuestion), Input: json.RawMessage(`{"question":"Proceed?"}`)},
		})
		done <- err
	}()
	<-handler.pending
	select {
	case id := <-finished:
		if id != "skipped" {
			t.Fatalf("finished candidate = %s, want skipped", id)
		}
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("skipped candidate readiness waited for earlier Question answer")
	}
	close(handler.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func (questionBatchTestHandler) Call(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{}, nil
}

func TestExecuteToolCallsMaterializesEveryQuestionBeforeAnyAnswer(t *testing.T) {
	broker := tools.NewAskQuestionBroker()
	pending := make(chan tools.AskQuestionRequest, 5)
	release := make(chan struct{})
	prepared := make(chan struct{})
	broker.SetLifecycleAskHandler(func(ctx context.Context, request tools.AskQuestionRequest) (tools.AskQuestionResolution, error) {
		select {
		case <-prepared:
		default:
			return nil, fmt.Errorf("Question handler ran before original batch preparation was published")
		}
		pending <- request
		select {
		case <-release:
			return tools.AskQuestionAnswer{Freeform: textutil.Value("answer")}, nil
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	})
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{},
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID: toolspec.ToolAskQuestion, Handler: tools.NewAskQuestionTool(broker, func() bool { return true }),
		}), Config{
			Model: "gpt-6-sol", EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
			OnEvent: func(event Event) {
				if event.PreparedQuestionBatch != nil {
					close(prepared)
				}
			},
		})
	stepID := runtimeTestStepID("batch-step")
	restore := setTestActiveStep(engine, stepID)
	defer restore()
	calls := make([]llm.ToolCall, 5)
	for index := range calls {
		calls[index] = llm.ToolCall{ID: fmt.Sprintf("ask-%d", index), Name: string(toolspec.ToolAskQuestion), Input: json.RawMessage(`{"question":"Proceed?"}`)}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		results, err := engine.executeToolCalls(ctx, stepID, calls)
		if err == nil && len(results) != len(calls) {
			err = fmt.Errorf("got %d results, want %d", len(results), len(calls))
		}
		done <- err
	}()
	seen := make(map[string]bool)
	for range calls {
		select {
		case request := <-pending:
			seen[request.ToolCallID] = true
			if request.QuestionBatch == nil || request.QuestionBatch.PreparedPromptCount != len(calls) {
				t.Fatalf("Question has no original batch metadata: %+v", request)
			}
		case <-time.After(3 * time.Second):
			cancel()
			<-done
			t.Fatalf("only %d of 5 Questions materialized before any answer", len(seen))
		}
	}
	select {
	case err := <-done:
		t.Fatalf("executor completed before Questions answered: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("execute batch: %v", err)
	}
}

func (questionBatchTestHandler) QuestionsEnabled() bool {
	return true
}

func TestFiveSlowQuestionsEmitOneReminderOnlyWhenBatchResumes(t *testing.T) {
	broker := tools.NewAskQuestionBroker()
	pending := make(chan tools.AskQuestionRequest, 5)
	finished := make(chan struct{}, 5)
	releases := make(map[string]chan struct{}, 5)
	calls := make([]llm.ToolCall, 5)
	for index := range calls {
		id := fmt.Sprintf("ask-%d", index)
		calls[index] = llm.ToolCall{ID: id, Name: string(toolspec.ToolAskQuestion), Input: json.RawMessage(`{"question":"Proceed?"}`)}
		releases[id] = make(chan struct{})
	}
	broker.SetLifecycleAskHandler(func(ctx context.Context, request tools.AskQuestionRequest) (tools.AskQuestionResolution, error) {
		pending <- request
		select {
		case <-releases[request.ToolCallID]:
			return tools.AskQuestionAnswer{Freeform: textutil.Value("answer")}, nil
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	})
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant}, ToolCalls: calls},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")}},
	}}
	var clock atomic.Int64
	start := time.Now().Add(-12 * time.Hour).Truncate(time.Millisecond)
	clock.Store(start.UnixNano())
	var reminders atomic.Int32
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t, tools.HandlerRegistration{
		ID: toolspec.ToolAskQuestion, Handler: tools.NewAskQuestionTool(broker, func() bool { return true }),
	}), Config{Model: "gpt-6-sol", EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}, IdleResumeReminderSeconds: textutil.Value(3600),
		OnEvent: func(event Event) {
			if event.Kind == EventQuestionCandidateFinished {
				finished <- struct{}{}
			}
			if event.Message.MessageType != nil && *event.Message.MessageType == llm.MessageTypeIdleResumeReminder {
				reminders.Add(1)
			}
		},
	})
	engine.transcriptRuntimeState().now = func() time.Time { return time.Unix(0, clock.Load()) }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := engine.SubmitUserMessage(ctx, "ask five questions"); done <- err }()
	for range calls {
		select {
		case request := <-pending:
			if request.QuestionBatch == nil || request.QuestionBatch.PreparedPromptCount != 5 {
				t.Fatal("Question missing batch identity")
			}
		case err := <-done:
			t.Fatalf("Question turn finished before batch wait: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("five Questions were not all waiting")
		}
	}
	for index, call := range calls {
		clock.Add(int64(2 * time.Hour))
		close(releases[call.ID])
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Fatal("Question answer was not delivered")
		}
		if index < 4 {
			if reminders.Load() != 0 {
				t.Fatal("reminder emitted for an individual answer")
			}
			if activity := store.ContextFacts().LastModelToolActivityAt; activity == nil || !activity.Equal(start) {
				t.Fatalf("answer changed model/tool activity: %v", activity)
			}
			client.mu.Lock()
			requestCount := len(client.calls)
			client.mu.Unlock()
			if requestCount != 1 {
				t.Fatal("model resumed before whole Question batch completed")
			}
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("model did not resume after batch completed")
	}
	if reminders.Load() != 1 || len(client.calls) != 2 || idleReminderCount(client.calls[1]) != 1 {
		t.Fatalf("batch continuation reminders=%d requests=%d", reminders.Load(), len(client.calls))
	}
}

func TestOrdinaryCompletionRecordsActivityBeforeSiblingQuestionAnswer(t *testing.T) {
	broker := tools.NewAskQuestionBroker()
	pending := make(chan struct{})
	release := make(chan struct{})
	broker.SetLifecycleAskHandler(func(ctx context.Context, _ tools.AskQuestionRequest) (tools.AskQuestionResolution, error) {
		close(pending)
		select {
		case <-release:
			return tools.AskQuestionAnswer{Freeform: textutil.Value("answer")}, nil
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	})
	store := mustCreateTestSession(t)
	engine := mustNewTestEngine(t, store, &fakeClient{}, newTestToolRegistry(t,
		tools.HandlerRegistration{ID: toolspec.ToolAskQuestion, Handler: tools.NewAskQuestionTool(broker, func() bool { return true })},
		tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand}},
	), Config{Model: "gpt-6-sol", EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion, toolspec.ToolExecCommand}})
	stepID := runtimeTestStepID("activity before human answer")
	restore := setTestActiveStep(engine, stepID)
	defer restore()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := engine.executeToolCalls(ctx, stepID, []llm.ToolCall{
			{ID: "ask", Name: string(toolspec.ToolAskQuestion), Input: json.RawMessage(`{"question":"Proceed?"}`)},
			{ID: "work", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`)},
		})
		done <- err
	}()
	<-pending
	deadline := time.After(3 * time.Second)
	for store.ContextFacts().LastModelToolActivityAt == nil {
		select {
		case <-deadline:
			t.Fatal("ordinary work activity waited for Question answer")
		case <-time.After(time.Millisecond):
		}
	}
	completedAt := *store.ContextFacts().LastModelToolActivityAt
	select {
	case err := <-done:
		t.Fatalf("batch finished before answer: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := store.ContextFacts().LastModelToolActivityAt; got == nil || !got.Equal(completedAt) {
		t.Fatalf("Question answer refreshed activity: %v, want %v", got, completedAt)
	}
}

func TestPrepareExecutorToolCallsAssignsQuestionBatchOutsideWorkflow(t *testing.T) {
	engine := &Engine{
		registry: newTestToolRegistry(t, tools.HandlerRegistration{
			ID:      toolspec.ToolAskQuestion,
			Handler: questionBatchTestHandler{},
		}),
	}
	prepared, err := prepareExecutorToolCalls(engine, "step-1", false, []llm.ToolCall{
		{ID: "ask-1", Name: string(toolspec.ToolAskQuestion), Input: json.RawMessage(`{"question":"First?"}`)},
		{ID: "ask-2", Name: string(toolspec.ToolAskQuestion), Input: json.RawMessage(`{"question":"Second?"}`)},
	})
	if err != nil {
		t.Fatalf("prepareExecutorToolCalls: %v", err)
	}
	for index, call := range prepared {
		if call.askQuestionBatch == nil {
			t.Fatalf("prepared call %d has no question batch", index)
		}
		if call.askQuestionBatch.StepID != "step-1" {
			t.Fatalf("prepared call %d step identity = %q, want step-1", index, call.askQuestionBatch.StepID)
		}
		if call.askQuestionBatch.CandidateOrdinal != index ||
			call.askQuestionBatch.PreparedPromptCount != 2 ||
			len(call.askQuestionBatch.BatchToolCallIDs) != 2 {
			t.Fatalf("prepared call %d batch = %+v", index, call.askQuestionBatch)
		}
		if call.askQuestionBatch.BatchToolCallIDs[index] != call.call.ID {
			t.Fatalf("prepared call %d prompt order = %v", index, call.askQuestionBatch.BatchToolCallIDs)
		}
	}
}
