package runtime

import (
	"context"
	"reflect"
	"testing"
	"time"

	"core/server/llm"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/workflow"
	"core/shared/config"
	"core/shared/textutil"
)

func TestWorkflowAssignmentAppliesThinkingInItsRuntimeFIFOPosition(t *testing.T) {
	store := mustCreateTestSession(t)
	engine := mustNewExecTestEngine(t, store, &fakeClient{}, Config{
		Model:                   "workflow-thinking-model",
		ThinkingLevel:           "medium",
		SupportedThinkingValues: []string{"low", "medium"},
	})
	if err := engine.pauseRuntimeOperations(t.Context()); err != nil {
		t.Fatalf("pause Runtime FIFO: %v", err)
	}

	operatorDone := make(chan error, 1)
	go func() {
		operatorDone <- engine.SetThinkingLevel(t.Context(), "low")
	}()
	waitForPendingRuntimeOperation(t, engine)

	thinking, err := workflow.NewThinkingValue("max")
	if err != nil {
		t.Fatalf("NewThinkingValue: %v", err)
	}
	snapshot, err := NewWorkflowAssignmentSnapshot(workflowAssignmentForCompactionTest())
	if err != nil {
		t.Fatalf("NewWorkflowAssignmentSnapshot: %v", err)
	}
	steer, err := engine.SteerWorkflowAssignmentSnapshot(snapshot.WithThinkingMutation(workflow.SetThinking(thinking)))
	if err != nil {
		t.Fatalf("SteerWorkflowAssignmentSnapshot: %v", err)
	}

	if err := engine.drainRuntimeOperations(t.Context()); err != nil {
		t.Fatalf("drain Runtime FIFO: %v", err)
	}
	select {
	case err := <-operatorDone:
		if err != nil {
			t.Fatalf("SetThinkingLevel: %v", err)
		}
	case <-time.After(runtimeTestSynchronizationTimeout):
		t.Fatal("timed out waiting for operator Thinking mutation")
	}
	if _, err := steer.Wait(t.Context()); err != nil {
		t.Fatalf("wait Workflow assignment: %v", err)
	}
	if got := engine.ThinkingLevel(); got != "max" {
		t.Fatalf("ThinkingLevel = %q, want Workflow assignment value max", got)
	}
}

func TestWorkflowAssignmentResumeWaitsBehindEarlierRuntimeMutation(t *testing.T) {
	store := mustCreateTestSession(t)
	engine := mustNewExecTestEngine(t, store, &fakeClient{}, Config{
		Model: "workflow-assignment-order-model",
	})
	if err := engine.pauseRuntimeOperations(t.Context()); err != nil {
		t.Fatalf("pause Runtime FIFO: %v", err)
	}

	previous := workflowAssignmentForCompactionTest()
	previous.Prompt.Identity = "workflow-current-node/previous"
	previousSnapshot, err := NewWorkflowAssignmentSnapshot(previous)
	if err != nil {
		t.Fatalf("build previous assignment snapshot: %v", err)
	}
	previousSteer, err := engine.SteerWorkflowAssignmentSnapshot(previousSnapshot)
	if err != nil {
		t.Fatalf("queue previous assignment: %v", err)
	}

	current := workflowAssignmentForCompactionTest()
	current.Prompt.Identity = "workflow-current-node/current"
	currentSteer, err := engine.SteerWorkflowAssignmentResume(t.Context(), current)
	if err != nil {
		t.Fatalf("queue Resume assignment: %v", err)
	}

	if err := engine.drainRuntimeOperations(t.Context()); err != nil {
		t.Fatalf("drain Runtime FIFO: %v", err)
	}
	if receipt, err := previousSteer.Wait(t.Context()); err != nil || !receipt.Committed {
		t.Fatalf("previous assignment receipt = %+v, error=%v", receipt, err)
	}
	if receipt, err := currentSteer.Wait(t.Context()); err != nil || !receipt.Committed {
		t.Fatalf("Resume assignment receipt = %+v, error=%v", receipt, err)
	}
	projection, err := store.ActiveWorkflowAssignmentProjection()
	if err != nil {
		t.Fatalf("load final assignment projection: %v", err)
	}
	if projection == nil || projection.SourcePath == nil || *projection.SourcePath != current.Prompt.Identity {
		t.Fatalf("final assignment projection = %+v, want queued Resume assignment %q", projection, current.Prompt.Identity)
	}
	if got := workflowModeRecordCount(t, store); got != 2 {
		t.Fatalf("Workflow assignment records = %d, want earlier mutation followed by Resume", got)
	}
}

func TestWorkflowAssignmentClearsThinkingOverride(t *testing.T) {
	store := mustCreateTestSession(t)
	engine := mustNewExecTestEngine(t, store, &fakeClient{}, Config{
		Model: "workflow-thinking-model", ThinkingLevel: "high",
	})
	if err := engine.SetThinkingLevel(t.Context(), "low"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewWorkflowAssignmentSnapshot(workflowAssignmentForCompactionTest())
	if err != nil {
		t.Fatal(err)
	}
	steer, err := engine.SteerWorkflowAssignmentSnapshot(snapshot.WithThinkingMutation(workflow.ClearThinking()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := steer.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := engine.ThinkingLevel(); got != config.DefaultOnboardingSettings().ThinkingLevel {
		t.Fatalf("cleared Thinking = %q, want configured default", got)
	}
	if settings := store.Meta().ChatSettings; settings != nil && settings.Thinking != nil {
		t.Fatal("cleared Thinking remained pinned as an override")
	}
}

func TestDormantWorkflowAssignmentPersistsThinkingMutation(t *testing.T) {
	value, err := workflow.NewThinkingValue("max")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []workflow.ThinkingMutation{workflow.SetThinking(value), workflow.ClearThinking()} {
		store := mustCreateTestSession(t)
		if err := store.SetThinkingOverride(textutil.Value("low")); err != nil {
			t.Fatal(err)
		}
		log := mustMaterializeTestEventLog(t, store)
		if _, _, err := log.AppendRecord(nil, session.MessageRecord{
			Role: session.MessageRoleUser, Content: textutil.Value("existing input"),
		}); err != nil {
			t.Fatal(err)
		}
		steer, err := SteerPersistedWorkflowAssignment(store, workflowAssignmentForCompactionTest(), PersistedWorkflowAssignmentContext{
			ThinkingMutation: mutation,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := steer.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		settings := mustOpenTestSession(t, store.Dir()).Meta().ChatSettings
		if mutation.Kind() == workflow.ThinkingMutationClear {
			if settings != nil && settings.Thinking != nil {
				t.Fatal("dormant assignment did not clear the persisted override")
			}
		} else if settings == nil || settings.Thinking == nil || *settings.Thinking != string(value) {
			t.Fatal("dormant assignment did not persist selected Thinking")
		}
	}
}

func TestDormantWorkflowAssignmentResumeUsesPersistedAssignmentIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		seedIdentity  *string
		wantRecordAdd int
	}{
		{name: "missing assignment", seedIdentity: nil, wantRecordAdd: 1},
		{name: "different assignment", seedIdentity: textutil.Value("run-previous"), wantRecordAdd: 1},
		{name: "exact assignment", seedIdentity: textutil.Value("run-current"), wantRecordAdd: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			if test.seedIdentity != nil {
				if _, _, err := appendTestEvent(t, store, "seed", llm.Message{
					Role:        llm.RoleDeveloper,
					MessageType: textutil.Value(llm.MessageTypeWorkflowMode),
					SourcePath:  test.seedIdentity,
					Content:     textutil.Value("existing assignment"),
				}); err != nil {
					t.Fatalf("seed assignment: %v", err)
				}
			}
			assignment := workflowAssignmentForCompactionTest()
			assignment.Prompt.Identity = "run-current"
			before := workflowModeRecordCount(t, store)

			steer, err := SteerPersistedWorkflowAssignmentForResume(
				store,
				assignment,
				PersistedWorkflowAssignmentContext{
					Model: "workflow-thinking-model",
				},
			)
			if err != nil {
				t.Fatalf("prepare dormant Resume assignment: %v", err)
			}
			receipt, err := steer.Wait(t.Context())
			if err != nil {
				t.Fatalf("wait dormant Resume assignment: %v", err)
			}
			if !receipt.Committed {
				t.Fatal("dormant Resume assignment was not committed")
			}
			after := workflowModeRecordCount(t, store)
			if got := after - before; got != test.wantRecordAdd {
				t.Fatalf("Workflow assignment record delta = %d, want %d", got, test.wantRecordAdd)
			}
			projection, err := store.ActiveWorkflowAssignmentProjection()
			if err != nil {
				t.Fatalf("load active assignment projection: %v", err)
			}
			if projection == nil || projection.SourcePath == nil ||
				(test.wantRecordAdd == 1 && *projection.SourcePath != assignment.Prompt.Identity) {
				t.Fatalf("active assignment projection = %+v, want restored identity %q", projection, assignment.Prompt.Identity)
			}
		})
	}
}

func workflowModeRecordCount(t *testing.T, store *session.Store) int {
	t.Helper()
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize Session event log: %v", err)
	}
	window, err := eventLog.ReadRecentRecords(100)
	if err != nil {
		t.Fatalf("read Session event log: %v", err)
	}
	count := 0
	for _, record := range window.Records {
		payload, err := record.Payload()
		if err != nil {
			t.Fatalf("read Session event payload: %v", err)
		}
		message, ok := payload.(session.MessageRecord)
		if ok && message.MessageType != nil && *message.MessageType == session.MessageTypeWorkflowMode {
			count++
		}
	}
	return count
}

func TestWorkflowThinkingSetterAcceptsStandardMaxAndCustomValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"high", "max", "provider-custom"} {
		t.Run(value, func(t *testing.T) {
			store := mustCreateTestSession(t)
			engine := mustNewExecTestEngine(t, store, &fakeClient{}, Config{
				Model: "workflow-thinking-model",
			})
			thinking, err := workflow.NewThinkingValue(value)
			if err != nil {
				t.Fatalf("NewThinkingValue: %v", err)
			}
			if err := engine.SetWorkflowThinkingValue(thinking); err != nil {
				t.Fatalf("SetWorkflowThinkingValue: %v", err)
			}
			if got := engine.ThinkingLevel(); got != value {
				t.Fatalf("ThinkingLevel = %q, want %q", got, value)
			}
		})
	}
}

func TestRestoringWorkflowAssignmentKeepsDesiredThinking(t *testing.T) {
	store := mustCreateTestSession(t)
	engine := mustNewExecTestEngine(t, store, &fakeClient{}, Config{ThinkingLevel: "medium"})
	if err := engine.SetThinkingLevel(t.Context(), "medium"); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := CapturePersistedWorkflowAssignment(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetThinkingLevel(t.Context(), "high"); err != nil {
		t.Fatal(err)
	}
	steer, err := engine.SteerWorkflowAssignmentSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := steer.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if engine.ThinkingLevel() != "high" {
		t.Fatalf("restored old Thinking: %s", engine.ThinkingLevel())
	}
}

func TestWorkflowThinkingProviderRejectionPropagates(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	client := &providerContractFailClient{}
	engine := mustNewExecTestEngine(t, store, client, Config{
		Model: "workflow-thinking-model",
	})
	thinking, err := workflow.NewThinkingValue("provider-custom")
	if err != nil {
		t.Fatalf("NewThinkingValue: %v", err)
	}
	if err := engine.SetWorkflowThinkingValue(thinking); err != nil {
		t.Fatalf("SetWorkflowThinkingValue: %v", err)
	}
	if _, err := engine.SubmitUserMessage(context.Background(), "run"); err == nil || !llm.IsNonRetriableModelError(err) {
		t.Fatalf("SubmitUserMessage error = %v, want non-retriable provider rejection", err)
	}
}

func TestWorkflowThinkingSetterPreservesCacheAndContractBoundaries(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSessionAt(t, t.TempDir(), sessiontest.NewPersistence().Options()...)
	engine := mustNewExecTestEngine(t, store, &fakeClient{
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
		}},
	}, Config{Model: "workflow-thinking-model"})
	if _, err := engine.SubmitUserMessage(context.Background(), "seed"); err != nil {
		t.Fatalf("seed SubmitUserMessage: %v", err)
	}
	before := store.Meta()
	if before.Locked == nil {
		t.Fatal("seed did not establish locked contract")
	}
	thinking, err := workflow.NewThinkingValue("max")
	if err != nil {
		t.Fatalf("NewThinkingValue: %v", err)
	}
	if err := engine.SetWorkflowThinkingValue(thinking); err != nil {
		t.Fatalf("SetWorkflowThinkingValue: %v", err)
	}
	after := store.Meta()
	if after.Locked == nil || !reflect.DeepEqual(after.Locked, before.Locked) {
		t.Fatalf("locked contract changed after thinking mutation: before=%+v after=%+v", before.Locked, after.Locked)
	}
}

func TestWorkflowThinkingClearPreservesCacheAndContractBoundaries(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSessionAt(t, t.TempDir(), sessiontest.NewPersistence().Options()...)
	engine := mustNewExecTestEngine(t, store, &fakeClient{
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
		}},
	}, Config{Model: "workflow-thinking-model", ThinkingLevel: "high"})
	if _, err := engine.SubmitUserMessage(context.Background(), "seed"); err != nil {
		t.Fatalf("seed SubmitUserMessage: %v", err)
	}
	before := store.Meta()
	if before.Locked == nil {
		t.Fatal("seed did not establish locked contract")
	}
	if err := engine.ClearWorkflowThinkingValue(); err != nil {
		t.Fatalf("ClearWorkflowThinkingValue: %v", err)
	}
	if got := engine.ThinkingLevel(); got != config.DefaultOnboardingSettings().ThinkingLevel {
		t.Fatalf("ThinkingLevel = %q, want configured default", got)
	}
	after := store.Meta()
	if after.Locked == nil || !reflect.DeepEqual(after.Locked, before.Locked) {
		t.Fatalf("locked contract changed after thinking clear: before=%+v after=%+v", before.Locked, after.Locked)
	}
}
