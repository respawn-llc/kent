package runtimecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"core/internal/testharness/requestfixture"
	"core/server/agentruntime"
	"core/server/llm"
	"core/server/sessionstore"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestGoalQuestionRemainsInterruptibleAcrossCurrentTurnCompletion(t *testing.T) {
	client := make(steeringExchangeClient)
	prompts := &runtimeControlPromptFeed{pending: make(chan struct{}, 1)}
	store, engine, service := newRuntimeControlTestServiceWithFeeds(t, client, nil, runtime.Config{
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
	}, nil, prompts)
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Error(err)
		}
	})
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := session.NewOpenSessionDescriptor(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitUserTurn(t.Context(), runtimeControlUserTurnRequest(store, "before-goal", "finish this turn")); err != nil {
		t.Fatal(err)
	}
	original := nextSteeringExchange(t, client)
	_, ok := service.authority.SessionExecution(sessionID)
	if !ok {
		t.Fatal("ordinary model request has no execution")
	}

	entered := make(chan struct{})
	proceed := make(chan struct{}, 1)
	defer close(proceed)
	admissionCtx, cancelAdmission := context.WithCancel(t.Context())
	defer cancelAdmission()
	admitted := make(chan error, 1)
	go func() {
		admitted <- service.authority.RunCurrentTurn(admissionCtx, descriptor,
			func(commit func() (bool, error)) (bool, error) { return commit() },
			func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
				close(entered)
				select {
				case <-proceed:
				case <-ctx.Done():
					return context.Cause(ctx)
				}
				_, err := accept(func() (bool, error) {
					result, err := engine.SetGoalAndStartLoop(ctx, "ask before proceeding", session.GoalActorUser)
					return goalResultAccepted(result), err
				})
				return err
			})
	}()
	<-entered
	original.reply <- llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
		Usage:     llm.Usage{WindowTokens: 200000},
	}
	waitForRuntimeControlIdle(t, engine)
	// Either retaining this execution or replacing it is valid; the newly
	// accepted Goal must still own a reachable, interruptible Question.
	proceed <- struct{}{}
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
	exchange := nextSteeringExchange(t, client)
	exchange.reply <- llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant},
		ToolCalls: []llm.ToolCall{{
			ID: "boundary-goal-question", Name: string(toolspec.ToolAskQuestion),
			Input: json.RawMessage(`{"question":"Proceed?"}`),
		}},
		Usage: llm.Usage{WindowTokens: 200000},
	}
	<-prompts.pending
	goalExecution, ok := service.authority.SessionExecution(sessionID)
	if !ok {
		t.Fatal("published Goal Question has no interruptible execution")
	}
	if _, err := service.LiveStop(t.Context(), &runtimepb.LiveStopRequest{SessionId: requestfixture.Selector(t, sessionID.String())}); err != nil {
		t.Fatal(err)
	}
	if _, err := goalExecution.Wait(t.Context()); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	waitForRuntimeControlIdle(t, engine)
}
