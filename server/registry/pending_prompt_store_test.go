package registry

import (
	"context"
	"slices"
	"testing"
	"time"

	"core/server/runtime"
	askquestion "core/server/tools"
	"core/shared/clientui"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
)

func TestPendingPromptsRetainOriginalBatchReadinessAcrossExternalAnswers(t *testing.T) {
	store := &pendingPromptStore{}
	resource := registryTestResourceRef("batch-session")
	scope := runtimeids.NewExecutionScopeID()
	ids := []string{"first", "second", "third", "fourth", "fifth"}
	if err := store.Prepare(resource, registryTestStepID, ids); err != nil {
		t.Fatal(err)
	}
	request := func(index int) askquestion.AskQuestionRequest {
		return askquestion.AskQuestionRequest{
			ToolCallID: ids[index], StepID: registryTestStepID, RunID: registryTestRunID,
			Question: "Proceed?", Origin: askquestion.AskQuestionOriginModelTool,
			QuestionBatch: &askquestion.AskQuestionBatchMetadata{
				Origin: askquestion.AskQuestionOriginModelTool, RunID: registryTestRunID, StepID: registryTestStepID,
				ToolCallID: ids[index], BatchToolCallIDs: ids, CandidateOrdinal: index, PreparedPromptCount: len(ids),
			},
		}
	}
	check := func(want uint32) {
		t.Helper()
		for _, prompt := range store.List("batch-session") {
			if prompt.Batch == nil || prompt.Batch.UnmaterializedCount != want || !slices.Equal(prompt.Batch.ToolCallIDs, ids) {
				t.Fatalf("batch = %+v, want original IDs %v and %d unmaterialized", prompt.Batch, ids, want)
			}
		}
	}
	store.Begin("batch-session", resource, scope, askquestion.AskQuestionRequest{
		ToolCallID: "approval", StepID: registryTestStepID, Approval: true,
	}, time.Now())
	check(5)
	store.Begin("batch-session", resource, scope, request(0), time.Now())
	check(4)
	store.Complete("batch-session", resource, scope, ids[0])
	check(4)
	for index := 1; index < len(ids); index++ {
		store.Begin("batch-session", resource, scope, request(index), time.Now().Add(-time.Duration(index)*time.Second))
		check(uint32(len(ids) - index - 1))
		store.Complete("batch-session", resource, scope, ids[index])
		check(uint32(len(ids) - index - 1))
	}
	pending := store.List("batch-session")
	if len(pending) != 1 || pending[0].Request.ToolCallID != "approval" {
		t.Fatalf("pending = %+v, want only Approval", pending)
	}
	if pending[0].Batch.UnmaterializedCount != 0 {
		t.Fatal("externally answered Questions prevented remaining Approval from becoming ready")
	}
}

func TestQuestionBatchReadinessUpdatesExistingPendingPromptSubscriptions(t *testing.T) {
	registry := NewRuntimeRegistry()
	engine := &runtime.Engine{}
	registerReady(t, registry, "session-1", engine)
	t.Cleanup(func() { closeRuntime(registry, "session-1", engine) })
	resource := registryTestResourceRef("session-1")
	entry := registry.authorityEntryByRef(resource)
	sub, err := entry.sessionFeed.broker.Subscribe(transcriptBrokerHydration(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := sub.Next(ctx); err != nil {
		t.Fatal(err)
	}
	stepID := registryTestStepID
	batch := &askquestion.AskQuestionBatchMetadata{StepID: stepID, BatchToolCallIDs: []string{"first", "skipped"}}
	if err := registry.PublishAuthorityRuntimeEvent(resource, runtime.Event{
		Kind: runtime.EventToolCallStarted, StepID: &stepID, PreparedQuestionBatch: batch,
	}); err != nil {
		t.Fatal(err)
	}
	scope := runtimeids.NewExecutionScopeID()
	snapshot, admitted := registry.pendingPrompts.Begin("session-1", resource, scope, askquestion.AskQuestionRequest{
		ToolCallID: "first", StepID: stepID, Question: "Proceed?", QuestionBatch: batch,
	}, time.Now())
	if !admitted {
		t.Fatal("prepared Question was not admitted")
	}
	publishPendingPrompt(entry.sessionFeed, "session-1", snapshot, pendingPromptEventPending)
	initial, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count := initial.Event.GetPrompt().GetQuestion().GetBatch().GetUnmaterializedCount(); count != 1 {
		t.Fatalf("initial readiness = %d, want 1", count)
	}
	callID := snapshot.Request.QuestionBatch.BatchToolCallIDs[1]
	finished := clientui.ToolCallID(callID)
	if err := registry.PublishAuthorityRuntimeEvent(resource, runtime.Event{
		Kind: runtime.EventQuestionCandidateFinished, StepID: &stepID, FinishedQuestionCandidate: &finished,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prompt := updated.Event.GetPrompt()
	if prompt == nil || prompt.Status != transcriptpb.PromptStatus_PROMPT_STATUS_PENDING ||
		prompt.GetQuestion().GetToolCallId() != "first" || prompt.GetQuestion().GetBatch().GetUnmaterializedCount() != 0 {
		t.Fatalf("readiness update = %+v, want same pending Question ready", prompt)
	}
}

func TestPendingPromptsOrderQuestionsByOriginalCallsNotRegistrationTime(t *testing.T) {
	store := &pendingPromptStore{}
	resource := registryTestResourceRef("batch-session")
	scope := runtimeids.NewExecutionScopeID()
	ids := []string{"first", "second", "third"}
	if err := store.Prepare(resource, registryTestStepID, ids); err != nil {
		t.Fatal(err)
	}
	for index := len(ids) - 1; index >= 0; index-- {
		store.Begin("batch-session", resource, scope, askquestion.AskQuestionRequest{
			ToolCallID: ids[index], StepID: registryTestStepID,
			QuestionBatch: &askquestion.AskQuestionBatchMetadata{BatchToolCallIDs: ids, CandidateOrdinal: index},
		}, time.Now())
	}
	for index, pending := range store.List("batch-session") {
		if pending.Request.ToolCallID != ids[index] {
			t.Fatalf("Question %d = %s, want %s", index, pending.Request.ToolCallID, ids[index])
		}
	}
}

func TestPendingQuestionBatchCompletesSkippedCandidatesWithoutWaitingForPrompts(t *testing.T) {
	store := &pendingPromptStore{}
	resource := registryTestResourceRef("batch-session")
	scope := runtimeids.NewExecutionScopeID()
	ids := []string{"skipped", "pending"}
	if err := store.Prepare(resource, registryTestStepID, ids); err != nil {
		t.Fatal(err)
	}
	store.Begin("batch-session", resource, scope, askquestion.AskQuestionRequest{
		ToolCallID: "approval", StepID: registryTestStepID, Approval: true,
	}, time.Now())
	ids[0] = "mutated"
	if !store.Finished(resource, registryTestStepID, "skipped") {
		t.Fatal("skipped candidate was not recognized as no longer future")
	}
	first := store.List("batch-session")[0]
	if first.Batch.UnmaterializedCount != 1 || first.Batch.ToolCallIDs[0] != "skipped" {
		t.Fatalf("batch after skipped candidate = %+v", first.Batch)
	}
	if store.Finished(resource, registryTestStepID, "skipped") {
		t.Fatal("duplicate completion changed readiness")
	}
	store.Finished(resource, registryTestStepID, "pending")
	if got := store.List("batch-session")[0].Batch.UnmaterializedCount; got != 0 {
		t.Fatalf("unmaterialized count = %d, want 0", got)
	}
	first.Batch.ToolCallIDs[0] = "mutated snapshot"
	if got := store.List("batch-session")[0].Batch.ToolCallIDs[0]; got != "skipped" {
		t.Fatalf("snapshot mutation changed original batch: %s", got)
	}
}
