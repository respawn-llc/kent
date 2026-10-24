package runtime

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"core/server/tools"
	"core/shared/runtimeids"
)

func TestClientInputIdentitySurvivesDeliveryAndReopen(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var first sync.Once
	var mu sync.Mutex
	var live []TranscriptCommittedRowFact
	store := mustCreateTestSession(t)
	client := &hookClient{
		response: finalTextResponse("done"),
		beforeReturn: func() error {
			first.Do(func() { close(started); <-release })
			return nil
		},
	}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-6-sol",
		OnEvent: func(event Event) {
			facts := TranscriptCommittedRowFactsFromEvent(event)
			mu.Lock()
			for _, fact := range facts {
				if fact.User != nil && len(fact.User.QueueItemIDs) > 0 {
					live = append(live, fact)
				}
			}
			mu.Unlock()
		},
	})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	done := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "start")
		done <- err
	}()
	pendingWorkTestWait(t, started, "held provider")
	ids := []string{runtimeids.NewQueueItemID().String(), runtimeids.NewQueueItemID().String()}
	for _, id := range ids {
		item, accepted, err := engine.QueueUserMessageForActiveRun(t.Context(), id, "identical", nil)
		pendingWorkTestNoError(t, err)
		if !accepted || item.ID != id {
			t.Fatalf("admission replaced caller identity: item=%+v accepted=%t", item, accepted)
		}
	}
	_, accepted, err := engine.QueueUserMessageForActiveRun(t.Context(), ids[0], "replacement", nil)
	if accepted || !errors.Is(err, errDuplicateQueuedUserMessageID) {
		t.Fatalf("duplicate active identity: accepted=%t err=%v", accepted, err)
	}
	_, err = engine.QueueUserInput(t.Context(), QueuedUserInput{
		ID: ids[0], ExecutionText: "replacement", CanonicalPresentation: "replacement",
	})
	if !errors.Is(err, errDuplicateQueuedUserMessageID) {
		t.Fatalf("Queue replaced active Steer identity: %v", err)
	}
	pending := pendingWorkTestSnapshot(t, engine)
	if len(pending.Items) != 2 || pending.Items[0].ID.String() != ids[0] || pending.Items[1].ID.String() != ids[1] {
		t.Fatalf("duplicate admission changed pending inputs: %+v", pending.Items)
	}
	unblock.Do(func() { close(release) })
	pendingWorkTestNoError(t, <-done)
	waitEngineLifecycleTasks(t, engine)
	mu.Lock()
	linked := slices.Clone(live)
	mu.Unlock()
	findInputRows := func(facts []TranscriptCommittedRowFact) []TranscriptCommittedRowFact {
		var found []TranscriptCommittedRowFact
		for _, fact := range facts {
			if fact.User != nil && len(fact.User.QueueItemIDs) == 1 && slices.Contains(ids, fact.User.QueueItemIDs[0]) {
				found = append(found, fact)
			}
		}
		return found
	}
	linked = findInputRows(linked)
	if len(linked) != 2 || linked[0].User.QueueItemIDs[0] != ids[0] || linked[1].User.QueueItemIDs[0] != ids[1] ||
		linked[0].Locator == linked[1].Locator {
		t.Fatalf("delivery lost distinct source identities: %+v", linked)
	}
	checkProjection := func(facts []TranscriptCommittedRowFact) {
		t.Helper()
		found := findInputRows(facts)
		if len(found) != len(linked) {
			t.Fatalf("hydrated delivery provenance differs: live=%+v hydrated=%+v", linked, found)
		}
		for index, fact := range found {
			if fact.Locator != linked[index].Locator || !slices.Equal(fact.User.QueueItemIDs, linked[index].User.QueueItemIDs) {
				t.Fatalf("hydrated delivery provenance differs: live=%+v hydrated=%+v", linked[index], fact)
			}
		}
	}
	checkProjection(TranscriptCommittedRowFactsFromSnapshot(mustEngineNewestSegmentPage(t, engine).Snapshot))
	pendingWorkTestNoError(t, engine.Close())
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	checkProjection(TranscriptCommittedRowFactsFromSnapshot(mustEngineNewestSegmentPage(t, reopened).Snapshot))
	checkProjection(hydrationSnapshot(t, reopened).CommittedRows)
}

func TestQueuedUserInputRejectsMissingOrMalformedIdentity(t *testing.T) {
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, tools.NewRegistry(), Config{})
	for _, id := range []string{"", "invalid", "018f3f8c-5b1a-7b72-8cb9-01af2c01a7e8", " " + runtimeids.NewQueueItemID().String(), "540A27AA-1E97-4696-8483-6D528FF8BBDD"} {
		_, err := engine.QueueUserInput(t.Context(), QueuedUserInput{ID: id, ExecutionText: "input", CanonicalPresentation: "input"})
		if err == nil {
			t.Fatalf("accepted invalid identity %q", id)
		}
	}
}
