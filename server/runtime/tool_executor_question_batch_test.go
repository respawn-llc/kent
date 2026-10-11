package runtime

import (
	"context"
	"encoding/json"
	"fmt"
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
	broker.SetAskHandler(func(ctx context.Context, request tools.AskQuestionRequest) (tools.AskQuestionResolution, error) {
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

func TestPrepareExecutorToolCallsAssignsQuestionBatchOutsideWorkflow(t *testing.T) {
	engine := &Engine{
		registry: newTestToolRegistry(t, tools.HandlerRegistration{
			ID:      toolspec.ToolAskQuestion,
			Handler: questionBatchTestHandler{},
		}),
	}
	prepared, err := prepareExecutorToolCalls(engine, "step-1", "run-1", false, []llm.ToolCall{
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
