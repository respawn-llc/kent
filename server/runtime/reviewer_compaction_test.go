package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"core/server/llm"
	"core/shared/textutil"
)

type reviewerCompactionReply struct {
	response llm.Response
	err      error
}

type reviewerCompactionExchange struct {
	request llm.Request
	reply   chan reviewerCompactionReply
}

type reviewerCompactionClient chan reviewerCompactionExchange

func (reviewerCompactionClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true}, nil
}

func (c reviewerCompactionClient) Generate(ctx context.Context, request llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	exchange := reviewerCompactionExchange{request: request, reply: make(chan reviewerCompactionReply, 1)}
	select {
	case c <- exchange:
	case <-ctx.Done():
		return llm.Response{}, context.Cause(ctx)
	}
	select {
	case result := <-exchange.reply:
		return result.response, result.err
	case <-ctx.Done():
		return llm.Response{}, context.Cause(ctx)
	}
}

func TestSupervisorFeedbackProcessesAfterSpeculativeCompactionSuccess(t *testing.T) {
	testSupervisorFeedbackAfterSpeculativeCompaction(t, nil)
}

func TestSupervisorFeedbackProcessesAfterSpeculativeCompactionFailure(t *testing.T) {
	testSupervisorFeedbackAfterSpeculativeCompaction(t, &llm.ProviderAPIError{ProviderID: "openai", StatusCode: 503, Code: llm.UnifiedErrorCodeUnknown})
}

func testSupervisorFeedbackAfterSpeculativeCompaction(t *testing.T, compactionErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	main := make(reviewerCompactionClient, 8)
	reviewer := make(reviewerCompactionClient, 8)
	accepted := make(chan error, 1)
	var engine *Engine
	engine = mustNewTestEngine(t, mustCreateTestSession(t), main, newTestToolRegistry(t), Config{
		Model: "gpt-6-sol", ContextWindowTokens: 200_000, AutoCompactTokenLimit: 199_000,
		MaxTokens: 1, CompactionMode: "local",
		Reviewer: ReviewerConfig{Frequency: "all", Model: "gpt-6-sol", Client: reviewer},
		SubmitAgentSteer: func(ctx context.Context, steer AgentSteer) error {
			_, err := engine.QueueAgentSteer(ctx, steer, nil)
			accepted <- err
			return err
		},
	})
	next := func(client reviewerCompactionClient) reviewerCompactionExchange {
		t.Helper()
		select {
		case exchange := <-client:
			return exchange
		case <-ctx.Done():
			t.Fatal("timed out waiting for provider operation")
			return reviewerCompactionExchange{}
		}
	}
	final := func(content string) llm.Response {
		return llm.Response{Assistant: llm.Message{
			Role: llm.RoleAssistant, Content: textutil.Value(content), Phase: textutil.Value(llm.MessagePhaseFinal),
		}, Usage: llm.Usage{WindowTokens: 200_000}}
	}
	done := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(ctx, "original input")
		done <- err
	}()
	original := next(main)
	answer := final("completed uncompacted answer")
	answer.Usage.ContextUsage = &llm.ContextUsage{Tokens: 176_000, MeasurementPoint: llm.ContextMeasurementCompletedResponse}
	original.reply <- reviewerCompactionReply{response: answer}
	review := next(reviewer)
	compaction := next(main)
	if err := <-done; err != nil {
		t.Fatalf("completed answer: %v", err)
	}
	reviewMessages := llm.MessagesFromItems(review.request.Items)
	wantReviewed := formatReviewerChatEntry(ChatEntry{Role: string(llm.RoleAssistant), Text: *answer.Assistant.Content})
	if len(reviewMessages) == 0 || messageContent(reviewMessages[len(reviewMessages)-1]) != wantReviewed {
		t.Fatalf("Supervisor did not capture the completed uncompacted answer: %+v", reviewMessages)
	}
	feedback := []string{"Follow up on the completed task."}
	payload := reviewerSuggestionsPayload{Suggestions: feedback}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	review.reply <- reviewerCompactionReply{response: final(string(encoded))}
	select {
	case err := <-accepted:
		if err != nil {
			t.Fatalf("ordinary Supervisor steering: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Supervisor feedback was not accepted during compaction")
	}
	select {
	case <-main:
		t.Fatal("main agent processed Supervisor feedback before compaction completed")
	default:
	}
	summary := "compacted completed task"
	compaction.reply <- reviewerCompactionReply{response: final(summary), err: compactionErr}
	followup := next(main)
	wantContext := summary
	if compactionErr != nil {
		wantContext = *answer.Assistant.Content
	}
	contextSeen, feedbackSeen := false, false
	for _, message := range llm.MessagesFromItems(followup.request.Items) {
		contextSeen = contextSeen || messageContent(message) == wantContext
		feedbackSeen = feedbackSeen || messageContent(message) == messageContent(supervisorSteer(feedback).Message())
	}
	if !contextSeen || !feedbackSeen {
		t.Fatalf("follow-up context=%t feedback=%t", contextSeen, feedbackSeen)
	}
	followup.reply <- reviewerCompactionReply{response: final("feedback addressed")}
	if err := engine.WaitForScheduledQueuedUserWork(ctx); err != nil {
		t.Fatalf("process Supervisor feedback: %v", err)
	}
	waitEngineLifecycleTasks(t, engine)
	select {
	case <-main:
		t.Fatal("unexpected provider operation after feedback was processed")
	default:
	}
}
