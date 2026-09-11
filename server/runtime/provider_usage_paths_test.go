package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/modelcontract"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestProviderUsageRecordsReviewerAndRemoteCompactionOperations(t *testing.T) {
	t.Run("Reviewer", func(t *testing.T) {
		store := mustCreateTestSession(t)
		client := &fakeClient{responses: []llm.Response{
			providerUsageTestResponse(11),
		}}
		engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
			Model: "gpt-5",
			Reviewer: ReviewerConfig{
				Model: "gpt-5",
			},
		})
		if _, err := runReviewerSuggestionsTestActiveStep(context.Background(), engine, "reviewer-usage", client); err != nil {
			t.Fatalf("run Reviewer: %v", err)
		}

		records := providerUsageTestRecords(t, store)
		if len(records) != 1 || records[0].Purpose != modelcontract.ProviderOperationPurposeReviewer {
			t.Fatalf("Reviewer usage records = %+v", records)
		}
	})

	t.Run("remote compaction", func(t *testing.T) {
		store := mustCreateTestSession(t)
		checkpoint := remoteCompactionReplacement(9, 4, 200_000)
		client := &fakeCompactionClient{
			compactionResponses: []llm.CompactionResponse{{
				Checkpoint:       checkpoint.Checkpoint,
				Usage:            llm.Usage{InputTokens: 9, OutputTokens: 4},
				ProviderEvidence: providerUsageTestResponse(4).ProviderEvidence,
			}},
		}
		engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
			Model:          "gpt-5",
			CompactionMode: "native",
		})
		if err := steerTestActiveStep(engine, "compaction-input", steerMessagesWithPersistenceIntent(
			steeringPriorityNormal,
			steeringMessageEventNone,
			true,
			[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("compact this")}},
		)); err != nil {
			t.Fatalf("persist compaction input: %v", err)
		}
		if _, receipt, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{}); err != nil || !receipt.Committed {
			t.Fatalf("remote compaction: receipt=%+v error=%v", receipt, err)
		}

		records := providerUsageTestRecords(t, store)
		if len(records) != 1 || records[0].Purpose != modelcontract.ProviderOperationPurposeCompaction {
			t.Fatalf("remote compaction usage records = %+v", records)
		}
	})
}

func TestProviderUsageRecordsLocalCompactionAsCompaction(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{
		providerUsageTestResponse(13),
	}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model:          "gpt-5",
		CompactionMode: "local",
	})
	if err := steerTestActiveStep(engine, "local-compaction-input", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("compact locally")}},
	)); err != nil {
		t.Fatalf("persist local compaction input: %v", err)
	}
	if _, receipt, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{}); err != nil || !receipt.Committed {
		t.Fatalf("local compaction: receipt=%+v error=%v", receipt, err)
	}

	records := providerUsageTestRecords(t, store)
	if len(records) != 1 || records[0].Purpose != modelcontract.ProviderOperationPurposeCompaction {
		t.Fatalf("local compaction usage records = %+v", records)
	}
}

func TestProviderUsageHistorySurvivesCompactionAndReopen(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeCompactionClient{
		responses: []llm.Response{
			providerUsageTestResponse(2),
			providerUsageTestResponse(5),
		},
		compactionResponses: []llm.CompactionResponse{{
			Checkpoint:       remoteCompactionReplacement(9, 4, 200_000).Checkpoint,
			Usage:            llm.Usage{InputTokens: 9, OutputTokens: 4},
			ProviderEvidence: providerUsageTestResponse(7).ProviderEvidence,
		}},
	}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model:          "gpt-5",
		CompactionMode: "native",
	})
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
	for index, before := range beforeCompactionRecords {
		if records[index].OperationID != before.OperationID || records[index].SessionID != before.SessionID {
			t.Fatalf("record %d identity after compaction/reopen = %+v, want %+v", index, records[index], before)
		}
		if records[index].Evidence.Usage == nil || before.Evidence.Usage == nil ||
			string(*records[index].Evidence.Usage) != string(*before.Evidence.Usage) {
			t.Fatalf("record %d usage after compaction/reopen = %v, want %v", index, records[index].Evidence.Usage, before.Evidence.Usage)
		}
	}
	operationIDs := make(map[string]struct{}, len(records))
	for index, record := range records {
		if _, exists := operationIDs[record.OperationID]; exists {
			t.Fatalf("operation ID %q was reused", record.OperationID)
		}
		operationIDs[record.OperationID] = struct{}{}
		var usage struct {
			OutputTokens int `json:"output_tokens"`
		}
		if record.Evidence.Usage == nil {
			t.Fatalf("record %d usage is absent", index)
		}
		if err := json.Unmarshal(*record.Evidence.Usage, &usage); err != nil {
			t.Fatalf("decode record %d usage: %v", index, err)
		}
		if got, want := usage.OutputTokens, []int{2, 5, 7}[index]; got != want {
			t.Fatalf("record %d output tokens = %d, want %d", index, got, want)
		}
	}
	if records[0].Purpose != modelcontract.ProviderOperationPurposeGeneration ||
		records[1].Purpose != modelcontract.ProviderOperationPurposeGeneration ||
		records[2].Purpose != modelcontract.ProviderOperationPurposeCompaction {
		t.Fatalf("provider usage purposes after compaction/reopen = %+v", records)
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
		records := providerUsageTestRecords(t, reopened)
		if len(records) != 1 {
			t.Fatalf("continued old session usage records = %+v, want one new record", records)
		}
	})

	t.Run("fork preserves source identity", func(t *testing.T) {
		parent := mustCreateTestSession(t)
		if _, _, err := appendTestEvent(t, parent, "fork-input", llm.Message{
			Role:    llm.RoleUser,
			Content: textutil.Value("fork me"),
		}); err != nil {
			t.Fatalf("append fork input: %v", err)
		}
		sourceEvidence := providerUsageTestResponse(19).ProviderEvidence
		sourceEvidence.RequestedModel = "gpt-5"
		usageRecord, err := sessionProviderUsageRecordFromRuntime(
			parent.Meta().SessionID,
			modelcontract.ProviderOperationPurposeGeneration,
			sourceEvidence,
		)
		if err != nil {
			t.Fatalf("build source usage record: %v", err)
		}
		if _, _, err := mustMaterializeTestEventLog(t, parent).AppendRecord(nil, usageRecord); err != nil {
			t.Fatalf("append source usage record: %v", err)
		}

		child, err := session.CloneSession(
			mustMaterializeTestEventLog(t, parent),
			"forked continuation",
			sessioncontract.SessionCategoryMain,
		)
		if err != nil {
			t.Fatalf("fork session: %v", err)
		}
		parentRecords := providerUsageTestRecords(t, parent)
		childRecords := providerUsageTestRecords(t, child)
		if len(parentRecords) != 1 || len(childRecords) != 1 {
			t.Fatalf("source/copy usage records = %d/%d, want one/one", len(parentRecords), len(childRecords))
		}
		if childRecords[0].OperationID != parentRecords[0].OperationID ||
			childRecords[0].SessionID != parentRecords[0].SessionID {
			t.Fatalf("forked usage identity = %+v, want source %+v", childRecords[0], parentRecords[0])
		}

		client := &fakeClient{responses: []llm.Response{providerUsageTestResponse(23)}}
		engine := mustNewTestEngine(t, child, client, tools.NewRegistry(), Config{Model: "gpt-5"})
		if _, err := generateTestActiveStep(context.Background(), engine, "forked-call", client, providerUsageTestRequest(child.Meta().SessionID, false)); err != nil {
			t.Fatalf("generate in fork: %v", err)
		}
		childRecords = providerUsageTestRecords(t, child)
		if len(childRecords) != 2 || childRecords[1].OperationID == childRecords[0].OperationID ||
			childRecords[1].SessionID != child.Meta().SessionID ||
			childRecords[1].SessionID == childRecords[0].SessionID {
			t.Fatalf("forked new usage records = %+v", childRecords)
		}
	})
}
