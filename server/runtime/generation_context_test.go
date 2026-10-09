package runtime

import (
	"fmt"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/textutil"
)

func TestCompactionAllowsConcurrentContextUsageReads(t *testing.T) {
	t.Parallel()
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeCompactionClient{
		compactionResponses: []llm.CompactionResponse{remoteCompactionReplacement(1_000, 100, 200_000)},
	}, tools.NewRegistry(), Config{Model: "gpt-6-sol", CompactionMode: "native"})
	if err := steerTestActiveStep(engine, "seed", steerMessagesWithPersistenceIntent(
		steeringPriorityNormal, steeringMessageEventNone, true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("seed")}},
	)); err != nil {
		t.Fatal(err)
	}
	started, stop, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		_ = engine.ContextUsage()
		close(started)
		for {
			select {
			case <-stop:
				return
			default:
				_ = engine.ContextUsage()
			}
		}
	}()
	<-started
	defer func() {
		close(stop)
		<-stopped
	}()
	if _, receipt, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{}); err != nil || !receipt.Committed {
		t.Fatalf("compaction: receipt=%+v error=%v", receipt, err)
	}
	buildActiveTurnRequestForTest(t, engine, nil, false)
	if engine.CompactionCount() != 1 {
		t.Fatal("concurrent status reads changed the compaction lifecycle")
	}
}

func TestPreparedCompactionRetainsHeadlessTransitionOnReopen(t *testing.T) {
	t.Parallel()
	for _, headless := range []bool{false, true} {
		t.Run(fmt.Sprint(headless), func(t *testing.T) {
			t.Parallel()
			store := mustCreateTestSession(t)
			if err := store.SetHeadlessActive(!headless); err != nil {
				t.Fatal(err)
			}
			engine := mustNewTestEngine(t, store, &fakeCompactionClient{
				compactionResponses: []llm.CompactionResponse{remoteCompactionReplacement(1_000, 100, 200_000)},
			}, tools.NewRegistry(), Config{Model: "gpt-6-sol", HeadlessMode: headless})
			if err := steerTestActiveStep(engine, "seed", steerMessagesWithPersistenceIntent(
				steeringPriorityNormal, steeringMessageEventNone, true,
				[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("seed")}},
			)); err != nil {
				t.Fatal(err)
			}
			if _, receipt, err := compactNowInActiveTestRun(t, engine, compactionModeManual, compactionInstructionsInput{}); err != nil || !receipt.Committed {
				t.Fatalf("compaction: receipt=%+v error=%v", receipt, err)
			}
			buildActiveTurnRequestForTest(t, engine, nil, false)
			if err := engine.Close(); err != nil {
				t.Fatal(err)
			}
			client := &fakeClient{responses: []llm.Response{finalOutputItemResponse("done")}}
			reopened := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), Config{
				Model: "gpt-6-sol", HeadlessMode: !headless,
			})
			if _, err := reopened.SubmitUserMessage(t.Context(), "continue"); err != nil {
				t.Fatal(err)
			}
			want := llm.MessageTypeHeadlessMode
			if headless {
				want = llm.MessageTypeHeadlessModeExit
			}
			found := false
			for _, item := range client.calls[0].Items {
				if item.MessageType != nil && *item.MessageType == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("reopening omitted required mode transition %s", want)
			}
		})
	}
}

func TestPreparingCompactionCountsSavedOutputOnce(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"native", "local"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			client := &fakeCompactionClient{
				responses: []llm.Response{finalOutputItemResponse("saved summary")},
				compactionResponses: []llm.CompactionResponse{
					remoteCompactionReplacement(1_000, 100, 200_000),
				},
			}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
				Model: "gpt-6-sol", CompactionMode: mode,
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
			buildActiveTurnRequestForTest(t, engine, nil, false)
			want := llm.EstimateItemsTokens(engine.cfg.TokenEstimator, engine.transcriptRuntimeState().SnapshotItems())
			if got := engine.ContextUsage().UsedTokens; got != want {
				t.Fatalf("prepared context usage = %d, want %d without counting saved output twice", got, want)
			}
		})
	}
}

func TestCompactionKindsLeaveContextPendingUntilLiveDispatch(t *testing.T) {
	t.Parallel()
	for _, providerMode := range []string{"native", "local"} {
		for _, mode := range []compactionMode{
			compactionModeManual,
			compactionModeAuto,
			compactionModeHandoff,
			compactionModeWorkflowPostCompletion,
		} {
			t.Run(providerMode+"/"+string(mode), func(t *testing.T) {
				t.Parallel()
				client := &fakeCompactionClient{
					responses: []llm.Response{finalOutputItemResponse("done")},
					compactionResponses: []llm.CompactionResponse{
						remoteCompactionReplacement(1_000, 100, 200_000),
					},
				}
				if providerMode == "local" {
					client.responses = append([]llm.Response{finalOutputItemResponse("summary")}, client.responses...)
				}
				engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
					Model: "gpt-6-sol", CompactionMode: providerMode,
				})
				if err := steerTestActiveStep(engine, "seed", steerMessagesWithPersistenceIntent(
					steeringPriorityNormal, steeringMessageEventNone, true,
					[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("seed")}},
				)); err != nil {
					t.Fatal(err)
				}
				if _, receipt, err := compactNowInActiveTestRun(t, engine, mode, compactionInstructionsInput{}); err != nil || !receipt.Committed {
					t.Fatalf("compaction: receipt=%+v error=%v", receipt, err)
				}
				if err := engine.ensureMetaContextForRequest(t.Context(), runtimeTestStepID("offline")); err != nil {
					t.Fatal(err)
				}
				if _, err := PrepareInspectionRequest(t.Context(), engine, false); err == nil {
					t.Fatal("inspection prepared pending context")
				}
				if usage := engine.ContextUsage(); usage.UsedTokens <= 0 {
					t.Fatal("pending summary omitted from context usage")
				}
				window, err := mustMaterializeTestEventLog(t, engine.store).ReadRecentRecords(16)
				if err != nil {
					t.Fatal(err)
				}
				compactedOutputs := 0
				for _, event := range window.Records {
					if replacement, ok := mustSessionEventPayload(event).(session.HistoryReplacementRecord); ok {
						if replacement.CompactedOutput == nil || len(replacement.Items) != 0 {
							t.Fatal("compaction persisted prepared context before live dispatch")
						}
						compactedOutputs++
					}
				}
				if compactedOutputs != 1 {
					t.Fatalf("pending outputs = %d, want one", compactedOutputs)
				}
				if _, err := engine.SubmitUserMessage(t.Context(), "continue"); err != nil {
					t.Fatal(err)
				}
				request := client.calls[len(client.calls)-1]
				summaries, preservedUsers, continuations := 0, 0, 0
				for _, item := range request.Items {
					if item.Type == llm.ResponseItemTypeCompaction ||
						item.MessageType != nil && *item.MessageType == llm.MessageTypeCompactionSummary {
						summaries++
					}
					if item.MessageType != nil && *item.MessageType == llm.MessageTypeCompactionPreservedUserMessage {
						preservedUsers++
					}
					if item.Role != nil && *item.Role == llm.RoleUser && item.Content != nil && *item.Content == "continue" {
						continuations++
					}
				}
				wantCalls := 1
				if providerMode == "local" {
					wantCalls++
				}
				if summaries != 1 || preservedUsers != 1 || continuations != 1 ||
					engine.CompactionCount() != 1 || len(client.calls) != wantCalls {
					t.Fatalf("summary/preserved/continuation/count/calls = %d/%d/%d/%d/%d",
						summaries, preservedUsers, continuations, engine.CompactionCount(), len(client.calls))
				}
			})
		}
	}
}
