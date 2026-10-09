package runtime

import (
	"context"
	"strings"
	"testing"

	"core/server/llm"
	"core/shared/textutil"
)

func TestCompletedResponseContextMeasurementAnchorsAcceptedOutput(t *testing.T) {
	for _, count := range []int{0, 100} {
		engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, newTestToolRegistry(t), Config{
			Model: "grok-4.7", ContextWindowTokens: 2_000,
		})
		requestItems := llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("request")}})
		output := llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleAssistant, Content: textutil.Value(strings.Repeat("response ", 100))}})
		if err := engine.steer(runtimeTestStepID("context-anchor"), steerMessagesWithPersistenceIntent(
			steeringPriorityNormal, steeringMessageEventNone, true,
			[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("request")}, {Role: llm.RoleAssistant, Content: textutil.Value(strings.Repeat("response ", 100))}},
		)); err != nil {
			t.Fatal(err)
		}
		candidate := newSuccessfulRequestCandidate(engine.cfg.TokenEstimator, llm.Request{Model: "grok-4.7", Items: requestItems}, llm.Response{
			OutputItems: output,
			Usage: llm.Usage{InputTokens: textutil.Value(12), OutputTokens: textutil.Value(8), ContextUsage: &llm.ContextUsage{
				Tokens: count, MeasurementPoint: llm.ContextMeasurementCompletedResponse,
			}},
		})
		if _, err := engine.commitAcceptedResponseCandidate("accepted", candidate, false); err != nil {
			t.Fatal(err)
		}
		if got := engine.ContextUsage().UsedTokens; got != count {
			t.Fatalf("context = %d, want provider measurement %d", got, count)
		}
		later := llm.Message{Role: llm.RoleUser, Content: textutil.Value("later request")}
		if err := engine.steer(runtimeTestStepID("context-growth"), steerMessagesWithPersistenceIntent(
			steeringPriorityNormal, steeringMessageEventNone, true, []llm.Message{later},
		)); err != nil {
			t.Fatal(err)
		}
		want := count + llm.EstimateItemsTokens(engine.cfg.TokenEstimator, llm.ItemsFromMessages([]llm.Message{later}))
		if got := engine.ContextUsage().UsedTokens; got != want {
			t.Fatalf("grown context = %d, want %d without repeating accepted output", got, want)
		}
		reopened := mustNewTestEngine(t, mustOpenTestSession(t, engine.store.Dir()), &fakeClient{}, newTestToolRegistry(t), Config{
			Model: "grok-4.7", ContextWindowTokens: 2_000,
		})
		if got := reopened.ContextUsage().UsedTokens; got != want {
			t.Fatalf("reopened context = %d, want %d", got, want)
		}
	}
}

func TestMissingContextMeasurementUsesEstimateInsteadOfBilling(t *testing.T) {
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, newTestToolRegistry(t), Config{
		Model: "grok-4.7", ContextWindowTokens: 2_000,
	})
	if err := engine.steer(runtimeTestStepID("context-fallback"), steerMessagesWithPersistenceIntent(
		steeringPriorityNormal, steeringMessageEventNone, true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("request without context measurement")}},
	)); err != nil {
		t.Fatal(err)
	}
	items := engine.transcriptRuntimeState().SnapshotItems()
	candidate := newSuccessfulRequestCandidate(engine.cfg.TokenEstimator, llm.Request{Model: "grok-4.7", Items: items}, llm.Response{
		Usage: llm.Usage{InputTokens: textutil.Value(1900), OutputTokens: textutil.Value(100)},
	})
	if _, err := engine.commitAcceptedResponseCandidate("accepted", candidate, false); err != nil {
		t.Fatal(err)
	}
	if got, want := engine.ContextUsage().UsedTokens, llm.EstimateItemsTokens(engine.cfg.TokenEstimator, items); got != want {
		t.Fatalf("context = %d, want estimate %d", got, want)
	}
}

func TestShouldAutoCompactAccountsForMessagesAppendedAfterLastUsage(t *testing.T) {
	t.Parallel()
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, newTestToolRegistry(t), Config{
		Model:                 "gpt-6-sol",
		ContextWindowTokens:   2_000,
		AutoCompactTokenLimit: 300,
	})
	engine.setLastUsage(llm.Usage{InputTokens: textutil.Value(120), WindowTokens: 2_000, ContextUsage: &llm.ContextUsage{Tokens: 120, MeasurementPoint: llm.ContextMeasurementInput}})
	if err := engine.steer(runtimeTestStepID("active-tail"), steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value(strings.Repeat("tail ", 320))}},
	)); err != nil {
		t.Fatalf("persist active-tail message: %v", err)
	}
	if usage := engine.ContextUsage(); usage.UsedTokens < 300 {
		t.Fatalf("active-tail usage = %+v, want at least compaction threshold", usage)
	}
	if !engine.shouldAutoCompactWithContext(context.Background()) {
		t.Fatal("active-tail growth after the usage checkpoint did not trigger auto compaction")
	}
}

func TestShouldAutoCompactUsesModelVisibleEncryptedReasoningEstimate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		estimator llm.TokenEstimator
		tokens    int
		compact   bool
	}{
		{"first-party", llm.OpenAITokenEstimator{}, 1_488, false},
		{"compatible", llm.DefaultTokenEstimator{}, 1_900, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, newTestToolRegistry(t), Config{
				Model:                 "gpt-6-sol",
				TokenEstimator:        test.estimator,
				ContextWindowTokens:   2_000,
				AutoCompactTokenLimit: 1_500,
			})
			if err := engine.steer(runtimeTestStepID("reasoning-history"), steerMessagesWithPersistenceIntent(
				steeringPriorityNormal,
				steeringMessageEventNone,
				true,
				[]llm.Message{
					{
						Role:    llm.RoleAssistant,
						Content: textutil.Value("prior"),
						ReasoningItems: []llm.ReasoningItem{{
							ID:               "reasoning-1",
							EncryptedContent: strings.Repeat("e", 4_000),
						}},
					},
					{Role: llm.RoleUser, Content: textutil.Value("next")},
				},
			)); err != nil {
				t.Fatalf("persist reasoning history: %v", err)
			}

			request := llm.Request{Items: engine.transcriptRuntimeState().SnapshotItems()}
			candidate := newSuccessfulRequestCandidate(test.estimator, request, llm.Response{
				Usage: llm.Usage{InputTokens: textutil.Value(900), WindowTokens: 2_000, ContextUsage: &llm.ContextUsage{Tokens: 900, MeasurementPoint: llm.ContextMeasurementInput}},
			})
			if _, err := engine.commitAcceptedResponseCandidate("accepted-response", candidate, false); err != nil {
				t.Fatalf("commit accepted response: %v", err)
			}

			if usage := engine.ContextUsage(); usage.UsedTokens != test.tokens {
				t.Fatalf("context usage = %+v, want %d", usage, test.tokens)
			}
			if engine.shouldAutoCompactWithContext(context.Background()) != test.compact {
				t.Fatalf("compaction eligibility did not use selected provider estimate")
			}
		})
	}
}

func TestShouldCompactBeforeUserMessageUsesEstimatedPromptGrowth(t *testing.T) {
	t.Parallel()
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, newTestToolRegistry(t), Config{
		Model:                         "gpt-6-sol",
		ContextWindowTokens:           1_000,
		AutoCompactTokenLimit:         950,
		PreSubmitCompactionLeadTokens: 50,
	})
	if err := engine.steer(runtimeTestStepID("existing"), steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("existing")}},
	)); err != nil {
		t.Fatalf("persist existing input: %v", err)
	}

	shouldCompact, err := engine.ShouldCompactBeforeUserMessage(
		context.Background(),
		strings.Repeat("next ", 1_000),
	)
	if err != nil {
		t.Fatalf("evaluate pre-submit compaction: %v", err)
	}
	if !shouldCompact {
		t.Fatal("estimated pending prompt growth did not trigger pre-submit compaction")
	}
}

func TestPreSubmitCompactionRechecksEligibilityAgainstCurrentContext(t *testing.T) {
	t.Parallel()
	client := &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{
		remoteCompactionReplacement(100, 10, 1_000),
	}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{
		Model:                         "gpt-6-sol",
		ContextWindowTokens:           1_000,
		AutoCompactTokenLimit:         950,
		PreSubmitCompactionLeadTokens: 50,
	})
	if err := engine.steer(runtimeTestStepID("existing"), steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("existing")}},
	)); err != nil {
		t.Fatalf("persist existing input: %v", err)
	}
	prompt := strings.Repeat("next ", 100)
	engine.setLastUsage(llm.Usage{InputTokens: textutil.Value(900), WindowTokens: 1_000, ContextUsage: &llm.ContextUsage{Tokens: 900, MeasurementPoint: llm.ContextMeasurementInput}})
	eligible, err := engine.ShouldCompactBeforeUserMessage(t.Context(), prompt)
	if err != nil {
		t.Fatalf("initial pre-submit eligibility: %v", err)
	}
	if !eligible {
		t.Fatal("initial context did not require pre-submit compaction")
	}

	engine.setLastUsage(llm.Usage{InputTokens: textutil.Value(100), WindowTokens: 1_000, ContextUsage: &llm.ContextUsage{Tokens: 100, MeasurementPoint: llm.ContextMeasurementInput}})
	receipt, err := engine.CompactContextForPreSubmitWithActiveHook(t.Context(), prompt, nil)
	if err != nil {
		t.Fatalf("execute pre-submit compaction after context reduction: %v", err)
	}
	if receipt.Committed || len(client.compactionCalls) != 0 {
		t.Fatalf("stale pre-submit compaction committed=%t provider-calls=%d", receipt.Committed, len(client.compactionCalls))
	}
}

func TestShouldAutoCompactPrefersConfiguredThresholdOverResolvedContextWindow(t *testing.T) {
	t.Parallel()
	client := &contextWindowClient{contextWindow: 1_000}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{
		Model:                 "gpt-6-sol",
		ContextWindowTokens:   400_000,
		AutoCompactTokenLimit: 360_000,
	})
	if err := engine.steer(runtimeTestStepID("input"), steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("input")}},
	)); err != nil {
		t.Fatalf("persist input: %v", err)
	}
	if usage := engine.ContextUsage(); usage.WindowTokens != 400_000 {
		t.Fatalf("configured context window = %d, want 400000", usage.WindowTokens)
	}
	if engine.shouldAutoCompactWithContext(context.Background()) || client.resolveCalls != 0 {
		t.Fatalf("configured threshold unexpectedly resolved model context window: calls=%d", client.resolveCalls)
	}
}

func TestShouldAutoCompactAccountsForReservedOutputBudget(t *testing.T) {
	t.Parallel()
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, newTestToolRegistry(t), Config{
		Model:                 "gpt-6-sol",
		ContextWindowTokens:   2_000,
		AutoCompactTokenLimit: 900,
		MaxTokens:             100,
	})
	engine.setLastUsage(llm.Usage{InputTokens: textutil.Value(850), WindowTokens: 2_000, ContextUsage: &llm.ContextUsage{Tokens: 850, MeasurementPoint: llm.ContextMeasurementInput}})
	if !engine.shouldAutoCompactWithContext(context.Background()) {
		t.Fatal("reserved output budget did not trigger auto compaction")
	}
}

func TestShouldAutoCompactStaysFalseFarBelowThreshold(t *testing.T) {
	t.Parallel()
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, newTestToolRegistry(t), Config{
		Model:                 "gpt-6-sol",
		ContextWindowTokens:   400_000,
		AutoCompactTokenLimit: 100_000,
	})
	if err := engine.steer(runtimeTestStepID("input"), steerMessagesWithPersistenceIntent(
		steeringPriorityNormal,
		steeringMessageEventNone,
		true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("input")}},
	)); err != nil {
		t.Fatalf("persist input: %v", err)
	}
	if engine.shouldAutoCompactWithContext(context.Background()) {
		t.Fatal("small estimated context unexpectedly triggered auto compaction")
	}
}
