package runtime

import (
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/textutil"
)

func TestCompactionPreparationPersistsOutputOnlyOnce(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	var events []Event
	engine := mustNewTestEngine(t, store, &fakeCompactionClient{
		compactionResponses: []llm.CompactionResponse{remoteCompactionReplacement(1000, 100, 200000)},
	}, tools.NewRegistry(), Config{
		Model: "gpt-6-sol", CompactionMode: "native",
		OnEvent: func(event Event) { events = append(events, event) },
	})
	if err := steerTestActiveStep(engine, "seed", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal, steeringMessageEventNone, true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("seed")}},
	)); err != nil {
		t.Fatal(err)
	}
	if _, receipt, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{}); err != nil || !receipt.Committed {
		t.Fatalf("compaction: %+v %v", receipt, err)
	}
	if err := steerTestActiveStep(engine, "accepted", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal, steeringMessageEventDefault, true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("accepted after compaction")}},
	)); err != nil {
		t.Fatal(err)
	}
	userFact := func(engine *Engine) TranscriptCommittedRowFact {
		t.Helper()
		var users []TranscriptCommittedRowFact
		for _, fact := range TranscriptCommittedRowFactsFromSnapshot(mustEngineNewestSegmentPage(t, engine).Snapshot) {
			if fact.Kind == TranscriptCommittedRowFactUser {
				users = append(users, fact)
			}
		}
		if len(users) != 1 {
			t.Fatalf("accepted user message projected %d times, want once", len(users))
		}
		return users[0]
	}
	accepted := userFact(engine)
	request := buildActiveTurnRequestForTest(t, engine, nil, true)
	if got := userFact(engine); !reflect.DeepEqual(got, accepted) {
		t.Fatalf("preparation changed accepted user provenance: got %+v, want %+v", got, accepted)
	}
	deliveries := 0
	for _, event := range events {
		for _, fact := range TranscriptCommittedRowFactsFromEvent(event) {
			if fact.Kind == TranscriptCommittedRowFactUser && fact.Locator == accepted.Locator {
				deliveries++
			}
		}
	}
	if deliveries != 1 {
		t.Fatalf("accepted user row delivered %d times, want once", deliveries)
	}
	window, err := mustMaterializeTestEventLog(t, store).ReadRecentRecords(32)
	if err != nil {
		t.Fatal(err)
	}
	checkpoints := 0
	for _, record := range window.Records {
		replacement, ok := mustSessionEventPayload(record).(session.HistoryReplacementRecord)
		if !ok {
			continue
		}
		items := replacement.Items
		if replacement.CompactedOutput != nil {
			items = replacement.CompactedOutput.Summary
		}
		for _, item := range items {
			if item.Type == session.ProviderHistoryItemTypeCompaction {
				checkpoints++
			}
		}
	}
	if checkpoints != 1 {
		t.Fatalf("durable compaction outputs = %d, want one immutable output", checkpoints)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	restored := buildActiveTurnRequestForTest(t, reopened, nil, true)
	assertCompactionRequestItemsEqual(t, request.Items, restored.Items)
	if got := userFact(reopened); !reflect.DeepEqual(got, accepted) {
		t.Fatalf("reopen changed accepted user provenance: got %+v, want %+v", got, accepted)
	}
}
