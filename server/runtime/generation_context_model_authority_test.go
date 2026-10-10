package runtime

import (
	"context"
	"errors"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/tools"
	"core/shared/textutil"
)

func TestCompactionPreparationFailureKeepsModelContract(t *testing.T) {
	t.Parallel()
	gate := sessiontest.NewPersistenceGate(runtimeTestSessionPersistence)
	store := mustCreateTestSessionAt(t, t.TempDir(), session.WithPersistenceObserver(gate))
	client := &fakeCompactionClient{
		compactionResponses: []llm.CompactionResponse{remoteCompactionReplacement(1_000, 100, 200_000)},
	}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-6-sol", CompactionMode: "native",
	})
	if err := steerTestActiveStep(engine, "seed", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal, steeringMessageEventNone, true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("seed")}},
	)); err != nil {
		t.Fatal(err)
	}
	if _, receipt, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{}); err != nil || !receipt.Committed {
		t.Fatalf("compaction: receipt=%+v error=%v", receipt, err)
	}
	observerErr := errors.New("post-commit observation failure")
	compactionSequence := store.Meta().LastSequence
	gate.FailWhen(func(snapshot session.PersistedStoreSnapshot) bool {
		return snapshot.Meta.LastSequence > compactionSequence
	}, observerErr)
	err := withActiveTestRun(t, engine, ActiveKindUserTurn, func(ctx context.Context, stepID string) error {
		_, err := engine.buildActiveTurnDispatchRequest(ctx, stepID, nil, true)
		return err
	})
	if !errors.Is(err, observerErr) {
		t.Fatalf("preparation error=%v, want committed observer failure", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedStore := mustOpenTestSession(t, store.Dir())
	locked := reopenedStore.Meta().Locked
	if locked == nil || locked.Model != "gpt-6-sol" {
		t.Fatalf("prepared context lost its model contract: %+v", locked)
	}
	target, err := session.ChatSettingsStateFromCompleteSettings("reviewer", session.ChatSettings{
		Supervisor: "off", Thinking: "high", Questions: true, AutoCompaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt, err := reopenedStore.CommitChatSettingsState(target); !errors.Is(err, session.ErrChatAgentLocked) || receipt.Committed {
		t.Fatalf("changing the prepared context's Agent: receipt=%+v error=%v", receipt, err)
	}
	nextClient := &fakeClient{responses: []llm.Response{finalOutputItemResponse("done")}}
	next := mustNewTestEngine(t, reopenedStore, nextClient, tools.NewRegistry(), Config{Model: "gpt-6.1-sol"})
	if _, err := next.SubmitUserMessage(t.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(nextClient.calls) != 1 || nextClient.calls[0].Model != locked.Model {
		t.Fatalf("retry did not use prepared context's model contract: %+v", nextClient.calls)
	}
}
