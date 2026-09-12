package runtimecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"core/server/llm"
	"core/server/promptcontrol"
	"core/server/runtime"
	"core/server/session"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestGoalStartedQuestionRemainsReachableForStop(t *testing.T) {
	exerciseGoalQuestion(t, false, goalQuestionStop)
}

type goalQuestionAction uint8

const (
	goalQuestionStop goalQuestionAction = iota
	goalQuestionAnswer
	goalQuestionDecline
	goalQuestionInterrupt
)

func TestGoalQuestionResolution(t *testing.T) {
	for _, start := range []struct {
		name   string
		resume bool
	}{{name: "set"}, {name: "resume", resume: true}} {
		t.Run(start.name, func(t *testing.T) {
			for _, action := range []struct {
				name   string
				action goalQuestionAction
			}{
				{name: "answer", action: goalQuestionAnswer},
				{name: "decline", action: goalQuestionDecline},
				{name: "interrupt", action: goalQuestionInterrupt},
				{name: "stop", action: goalQuestionStop},
			} {
				t.Run(action.name, func(t *testing.T) {
					exerciseGoalQuestion(t, start.resume, action.action)
				})
			}
		})
	}
}

func exerciseGoalQuestion(t *testing.T, resume bool, action goalQuestionAction) {
	t.Helper()
	client := make(steeringExchangeClient)
	prompts := &runtimeControlPromptFeed{pending: make(chan struct{}, 1)}
	store, engine, service := newRuntimeControlTestServiceWithFeeds(t, client, nil, runtime.Config{
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
	}, nil, prompts)
	// Also release an orphan broker wait when exercising the broken implementation.
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Error(err)
		}
	})
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if resume {
		if _, err := engine.SetGoal(t.Context(), "ask before proceeding", session.GoalActorUser); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.SetGoalStatus(t.Context(), session.GoalStatusPaused, session.GoalActorUser); err != nil {
			t.Fatal(err)
		}
		_, err = service.ResumeGoal(t.Context(), &runtimepb.GoalMutationRequest{SessionId: sessionID.String(), Actor: "user"})
	} else {
		_, err = service.SetGoal(t.Context(), &runtimepb.GoalSetRequest{
			SessionId: sessionID.String(), Objective: "ask before proceeding", Actor: "user",
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	exchange := nextSteeringExchange(t, client)
	exchange.reply <- llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant},
		ToolCalls: []llm.ToolCall{{
			ID: "goal-question", Name: string(toolspec.ToolAskQuestion),
			Input: json.RawMessage(`{"question":"Proceed?"}`),
		}},
		Usage: llm.Usage{WindowTokens: 200000},
	}
	select {
	case <-prompts.pending:
	case <-time.After(time.Second):
		stopped, err := service.LiveStop(t.Context(), &runtimepb.LiveStopRequest{SessionId: store.Meta().SessionID})
		t.Fatalf("model asked a Question but it never reached the Session prompt owner; stop=%+v error=%v", stopped, err)
	}
	execution, ok := service.authority.SessionExecution(sessionID)
	if !ok {
		t.Fatal("pending Question has no interruptible execution")
	}
	switch action {
	case goalQuestionAnswer, goalQuestionDecline:
		active := engine.ActiveRun()
		if active == nil {
			t.Fatal("pending Question has no active model Step")
		}
		stepID, err := runtimeids.ParseStepID(active.StepID)
		if err != nil {
			t.Fatal(err)
		}
		entry := &promptpb.AnswerBatchEntry{ToolCallId: "goal-question", Answer: &promptpb.AnswerBatchEntry_Declined{Declined: &promptpb.Declined{}}}
		if action == goalQuestionAnswer {
			entry.Answer = &promptpb.AnswerBatchEntry_QuestionAnswer{QuestionAnswer: &promptpb.QuestionAnswer{Freeform: textutil.Value("proceed")}}
		}
		response, err := promptcontrol.NewPromptControlService(service.authority).AnswerPromptBatch(t.Context(), &promptpb.AnswerBatchRequest{
			SessionId: sessionID.String(), StepId: stepID.String(), Entries: []*promptpb.AnswerBatchEntry{entry},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Results) != 1 || response.Results[0].Outcome != promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_RESOLVED {
			t.Fatalf("resolve Goal Question = %+v", response)
		}
		// The model must actually continue, not merely acknowledge delivery.
		nextSteeringExchange(t, client)
	case goalQuestionInterrupt:
		if _, err := service.Interrupt(t.Context(), &runtimepb.InterruptRequest{SessionId: sessionID.String()}); err != nil {
			t.Fatal(err)
		}
	case goalQuestionStop:
		stopped, err := service.LiveStop(t.Context(), &runtimepb.LiveStopRequest{SessionId: sessionID.String()})
		if err != nil {
			t.Fatal(err)
		}
		if stopped.Status != runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_STOPPED {
			t.Fatalf("stop waiting Question = %+v", stopped)
		}
	}
	if action == goalQuestionAnswer || action == goalQuestionDecline {
		if _, err := service.LiveStop(t.Context(), &runtimepb.LiveStopRequest{SessionId: sessionID.String()}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = execution.Wait(t.Context())
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	waitForRuntimeControlIdle(t, engine)
	if _, ok := service.authority.SessionExecution(sessionID); ok {
		t.Fatal("stopped Goal kept an active execution")
	}
	// Completion of the old execution must leave normal human input usable.
	if _, err := service.SubmitUserTurn(t.Context(), runtimeControlUserTurnRequest(store, "after-goal-question", "continue")); err != nil {
		t.Fatal(err)
	}
	nextSteeringExchange(t, client)
	if _, err := service.LiveStop(t.Context(), &runtimepb.LiveStopRequest{SessionId: sessionID.String()}); err != nil {
		t.Fatal(err)
	}
}
