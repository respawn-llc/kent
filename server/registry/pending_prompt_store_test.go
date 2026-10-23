package registry

import (
	"slices"
	"testing"
	"time"

	"core/server/agentruntime"
	askquestion "core/server/tools"
	"core/shared/clientui"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
)

func TestPendingPromptStepGroupsAreChronological(t *testing.T) {
	oldStep := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	newStep := "11111111-1111-4111-8111-111111111111"
	pending := map[string]PendingPromptSnapshot{
		"old": {Request: askquestion.AskQuestionRequest{StepID: oldStep, ToolCallID: "old"}, CreatedAt: time.Unix(1, 0)},
		"new": {Request: askquestion.AskQuestionRequest{StepID: newStep, ToolCallID: "new"}, CreatedAt: time.Unix(2, 0)},
	}
	items := listPendingPrompts(pending)
	if items[0].Request.StepID != oldStep {
		t.Fatal("newer Step was ordered before older Step")
	}
}

func TestPendingPromptsRetainOriginalBatchReadinessAcrossExternalAnswers(t *testing.T) {
	store := &pendingPromptStore{}
	resource := registryTestResourceRef("22222222-2222-4222-8222-222222222222")
	scope := runtimeids.NewExecutionScopeID()
	ids := []string{"first", "second", "third", "fourth", "fifth"}
	if err := store.Prepare(resource, registryTestStepID, ids); err != nil {
		t.Fatal(err)
	}
	request := func(index int) askquestion.AskQuestionRequest {
		return askquestion.AskQuestionRequest{
			ToolCallID: ids[index], StepID: registryTestStepID,
			Question: "Proceed?", Origin: askquestion.AskQuestionOriginModelTool,
			QuestionBatch: &askquestion.AskQuestionBatchMetadata{
				Origin: askquestion.AskQuestionOriginModelTool, StepID: registryTestStepID,
				ToolCallID: ids[index], BatchToolCallIDs: ids, CandidateOrdinal: index, PreparedPromptCount: len(ids),
			},
		}
	}
	check := func(want uint32) {
		t.Helper()
		for _, prompt := range store.List("22222222-2222-4222-8222-222222222222") {
			if prompt.Batch == nil || prompt.Batch.UnmaterializedCount != want || !slices.Equal(prompt.Batch.ToolCallIDs, ids) {
				t.Fatalf("batch = %+v, want original IDs %v and %d unmaterialized", prompt.Batch, ids, want)
			}
		}
	}
	store.Begin("22222222-2222-4222-8222-222222222222", resource, scope, askquestion.AskQuestionRequest{
		ToolCallID: "approval", StepID: registryTestStepID, Approval: true,
	}, time.Now())
	check(5)
	store.Begin("22222222-2222-4222-8222-222222222222", resource, scope, request(0), time.Now())
	check(4)
	store.Complete("22222222-2222-4222-8222-222222222222", resource, scope, ids[0])
	check(4)
	for index := 1; index < len(ids); index++ {
		store.Begin("22222222-2222-4222-8222-222222222222", resource, scope, request(index), time.Now().Add(-time.Duration(index)*time.Second))
		check(uint32(len(ids) - index - 1))
		store.Complete("22222222-2222-4222-8222-222222222222", resource, scope, ids[index])
		check(uint32(len(ids) - index - 1))
	}
	pending := store.List("22222222-2222-4222-8222-222222222222")
	if len(pending) != 1 || pending[0].Request.ToolCallID != "approval" {
		t.Fatalf("pending = %+v, want only Approval", pending)
	}
	if pending[0].Batch.UnmaterializedCount != 0 {
		t.Fatal("externally answered Questions prevented remaining Approval from becoming ready")
	}
}

func TestQuestionBatchReadinessUpdatesExistingPendingPromptSubscriptions(t *testing.T) {
	registry := NewRuntimeRegistry(nil)
	engine := &runtime.Engine{}
	registerReady(t, registry, "11111111-1111-4111-8111-111111111111", engine)
	t.Cleanup(func() { closeRuntime(registry, "11111111-1111-4111-8111-111111111111", engine) })
	resource := registryTestResourceRef("11111111-1111-4111-8111-111111111111")
	entry := registry.authorityEntryByRef(resource)
	sub, err := entry.sessionFeed.broker.Subscribe(transcriptBrokerHydration(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	ctx := t.Context()
	if _, err := sub.Next(t.Context()); err != nil {
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
	snapshot, admitted := registry.pendingPrompts.Begin("11111111-1111-4111-8111-111111111111", resource, scope, askquestion.AskQuestionRequest{
		ToolCallID: "first", StepID: stepID, Question: "Proceed?", QuestionBatch: batch,
	}, time.Now())
	if !admitted {
		t.Fatal("prepared Question was not admitted")
	}
	publishPendingPrompt(entry.sessionFeed, "11111111-1111-4111-8111-111111111111", snapshot, pendingPromptEventPending)
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
	resource := registryTestResourceRef("22222222-2222-4222-8222-222222222222")
	scope := runtimeids.NewExecutionScopeID()
	ids := []string{"first", "second", "third"}
	if err := store.Prepare(resource, registryTestStepID, ids); err != nil {
		t.Fatal(err)
	}
	for index := len(ids) - 1; index >= 0; index-- {
		store.Begin("22222222-2222-4222-8222-222222222222", resource, scope, askquestion.AskQuestionRequest{
			ToolCallID: ids[index], StepID: registryTestStepID,
			QuestionBatch: &askquestion.AskQuestionBatchMetadata{BatchToolCallIDs: ids, CandidateOrdinal: index},
		}, time.Now())
	}
	for index, pending := range store.List("22222222-2222-4222-8222-222222222222") {
		if pending.Request.ToolCallID != ids[index] {
			t.Fatalf("Question %d = %s, want %s", index, pending.Request.ToolCallID, ids[index])
		}
	}
}

func TestPendingQuestionBatchCompletesSkippedCandidatesWithoutWaitingForPrompts(t *testing.T) {
	store := &pendingPromptStore{}
	resource := registryTestResourceRef("22222222-2222-4222-8222-222222222222")
	scope := runtimeids.NewExecutionScopeID()
	ids := []string{"skipped", "pending"}
	if err := store.Prepare(resource, registryTestStepID, ids); err != nil {
		t.Fatal(err)
	}
	store.Begin("22222222-2222-4222-8222-222222222222", resource, scope, askquestion.AskQuestionRequest{
		ToolCallID: "approval", StepID: registryTestStepID, Approval: true,
	}, time.Now())
	ids[0] = "mutated"
	if !store.Finished(resource, registryTestStepID, "skipped") {
		t.Fatal("skipped candidate was not recognized as no longer future")
	}
	first := store.List("22222222-2222-4222-8222-222222222222")[0]
	if first.Batch.UnmaterializedCount != 1 || first.Batch.ToolCallIDs[0] != "skipped" {
		t.Fatalf("batch after skipped candidate = %+v", first.Batch)
	}
	if store.Finished(resource, registryTestStepID, "skipped") {
		t.Fatal("duplicate completion changed readiness")
	}
	store.Finished(resource, registryTestStepID, "pending")
	if got := store.List("22222222-2222-4222-8222-222222222222")[0].Batch.UnmaterializedCount; got != 0 {
		t.Fatalf("unmaterialized count = %d, want 0", got)
	}
	first.Batch.ToolCallIDs[0] = "mutated snapshot"
	if got := store.List("22222222-2222-4222-8222-222222222222")[0].Batch.ToolCallIDs[0]; got != "skipped" {
		t.Fatalf("snapshot mutation changed original batch: %s", got)
	}
}
