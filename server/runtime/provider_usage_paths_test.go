package runtime

import (
	"context"
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/modelcontract"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestProviderUsageRecordsReviewerOperations(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{providerUsageTestResponse(11)}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-5", Reviewer: ReviewerConfig{Model: "gpt-5"},
	})
	if _, err := runReviewerSuggestionsTestActiveStep(context.Background(), engine, "reviewer-usage", client); err != nil {
		t.Fatalf("run Reviewer: %v", err)
	}
	if records := providerUsageTestRecords(t, store); len(records) != 1 {
		t.Fatalf("Reviewer usage records = %+v", records)
	}
}

func TestProviderUsageRecordsOperationIdentity(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{providerUsageTestResponse(11)}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})
	if _, err := generateTestActiveStep(
		context.Background(),
		engine,
		"provider-identity",
		client,
		providerUsageTestRequest(store.Meta().SessionID, false),
	); err != nil {
		t.Fatalf("generate provider response: %v", err)
	}
	records := providerUsageTestObservationRecords(t, store)
	if len(records) != 1 {
		t.Fatalf("provider observations = %+v, want one", records)
	}
	record := records[0]
	if record.OperationID == nil || record.SessionID == nil || record.Purpose == nil || record.ObservedAt == nil {
		t.Fatalf("provider observation identity = %+v, want complete identity", record)
	}
	if _, err := runtimeids.ParseCanonicalUUIDv4(*record.OperationID, "provider operation ID"); err != nil {
		t.Fatalf("provider operation ID = %q: %v", *record.OperationID, err)
	}
	if *record.SessionID != store.Meta().SessionID {
		t.Fatalf("provider Session ID = %q, want %q", *record.SessionID, store.Meta().SessionID)
	}
	if *record.Purpose != modelcontract.ProviderOperationPurposeGeneration {
		t.Fatalf("provider purpose = %q, want generation", *record.Purpose)
	}
	if record.ObservedAt.IsZero() {
		t.Fatal("provider observation time is zero")
	}
}

func TestProviderUsageRecordsLocalCompaction(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{providerUsageTestResponse(13)}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5", CompactionMode: "local"})
	if err := steerTestActiveStep(engine, "local-compaction-input", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal, steeringMessageEventNone, true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("compact locally")}},
	)); err != nil {
		t.Fatalf("persist local compaction input: %v", err)
	}
	if _, receipt, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{}); err != nil || !receipt.Committed {
		t.Fatalf("local compaction: receipt=%+v error=%v", receipt, err)
	}
	if records := providerUsageTestRecords(t, store); len(records) != 1 {
		t.Fatalf("local compaction usage records = %+v", records)
	}
}

func TestProviderUsageHistorySurvivesCompactionAndReopen(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeCompactionClient{
		responses:           []llm.Response{providerUsageTestMixedResponse(2), providerUsageTestMixedResponse(5)},
		compactionResponses: []llm.CompactionResponse{{Checkpoint: remoteCompactionReplacement(9, 4, 200_000).Checkpoint, Usage: llm.Usage{InputTokens: 9, OutputTokens: 4}, ProviderEvidence: providerUsageTestMixedResponse(7).ProviderEvidence}},
	}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5", CompactionMode: "native"})
	request := providerUsageTestRequest(store.Meta().SessionID, false)
	for index := 0; index < 2; index++ {
		if _, err := generateTestActiveStep(context.Background(), engine, "before-compaction", client, request); err != nil {
			t.Fatalf("generate response %d: %v", index, err)
		}
	}
	beforeCompactionRecords := providerUsageTestRecords(t, store)
	if len(beforeCompactionRecords) != 2 {
		t.Fatalf("provider usage records before compaction = %d, want 2", len(beforeCompactionRecords))
	}
	if err := steerTestActiveStep(engine, "compaction-input", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("compact retained usage")}},
	)); err != nil {
		t.Fatalf("persist compaction input: %v", err)
	}
	if _, receipt, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{}); err != nil || !receipt.Committed {
		t.Fatalf("compact usage history: receipt=%+v error=%v", receipt, err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close session before reopen: %v", err)
	}
	reopened, err := runtimeTestSessionPersistence.Open(store.Dir())
	if err != nil {
		t.Fatalf("reopen session: %v", err)
	}
	records := providerUsageTestRecords(t, reopened)
	if len(records) != 3 {
		t.Fatalf("provider usage records after compaction/reopen = %d, want 3", len(records))
	}
	if !reflect.DeepEqual(records[:2], beforeCompactionRecords) {
		t.Fatalf("ordinary provider records changed across compaction/reopen: got=%+v want=%+v", records[:2], beforeCompactionRecords)
	}
	for index, outputTokens := range []int{2, 5, 7} {
		expectedEvidence := providerUsageTestMixedResponse(outputTokens).ProviderEvidence
		if !reflect.DeepEqual(records[index], expectedEvidence) {
			t.Fatalf("record %d mixed evidence = %+v, want %+v", index, records[index], expectedEvidence)
		}
	}
}

func TestProviderUsageHistoryContinuesFromOldSessionAndPreservesForkIdentity(t *testing.T) {
	t.Run("old session continuation", func(t *testing.T) {
		store := mustCreateTestSession(t)
		if _, _, err := appendTestEvent(t, store, "old-session", llm.Message{
			Role:    llm.RoleUser,
			Content: textutil.Value("historical input"),
		}); err != nil {
			t.Fatalf("append old session input: %v", err)
		}
		reopened := mustOpenTestSession(t, store.Dir())
		client := &fakeClient{responses: []llm.Response{providerUsageTestResponse(17)}}
		engine := mustNewTestEngine(t, reopened, client, tools.NewRegistry(), Config{Model: "gpt-5"})
		if _, err := generateTestActiveStep(context.Background(), engine, "continued", client, providerUsageTestRequest(reopened.Meta().SessionID, false)); err != nil {
			t.Fatalf("continue old session: %v", err)
		}
		if records := providerUsageTestRecords(t, reopened); len(records) != 1 {
			t.Fatalf("continued old session usage records = %+v, want one new record", records)
		}
	})

	t.Run("fork preserves source evidence", func(t *testing.T) {
		parent := mustCreateTestSession(t)
		sourceClient := &fakeClient{responses: []llm.Response{providerUsageTestResponse(19)}}
		sourceEngine := mustNewTestEngine(t, parent, sourceClient, tools.NewRegistry(), Config{Model: "gpt-5", ThinkingLevel: "medium"})
		if _, err := generateTestActiveStep(context.Background(), sourceEngine, "fork-source", sourceClient, providerUsageTestRequest(parent.Meta().SessionID, false)); err != nil {
			t.Fatalf("generate source usage: %v", err)
		}
		if err := sourceEngine.Close(); err != nil {
			t.Fatalf("close source session: %v", err)
		}
		child, err := session.CloneSession(
			mustMaterializeTestEventLog(t, parent),
			"forked continuation",
			sessioncontract.SessionCategoryMain,
			session.ForkThinking{Desired: sourceEngine.ThinkingLevel(), PreserveNativeUpdates: false},
		)
		if err != nil {
			t.Fatalf("fork session: %v", err)
		}
		parentRecords := providerUsageTestRecords(t, parent)
		childRecords := providerUsageTestRecords(t, child)
		if len(parentRecords) != 1 || len(childRecords) != 1 || !reflect.DeepEqual(childRecords, parentRecords) {
			t.Fatalf("source/copy usage records = %d/%d, want identical one/one", len(parentRecords), len(childRecords))
		}
		client := &fakeClient{responses: []llm.Response{providerUsageTestResponse(23)}}
		engine := mustNewTestEngine(t, child, client, tools.NewRegistry(), Config{Model: "gpt-5"})
		if _, err := generateTestActiveStep(context.Background(), engine, "forked-call", client, providerUsageTestRequest(child.Meta().SessionID, false)); err != nil {
			t.Fatalf("generate in fork: %v", err)
		}
		childRecords = providerUsageTestRecords(t, child)
		if len(childRecords) != 2 {
			t.Fatalf("forked new usage records = %+v", childRecords)
		}
		assertProviderUsageOutputTokens(t, childRecords[1], 23)
	})
}
