package workflowrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"core/server/launch"
	"core/server/llm"
	"core/server/session"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowstore"
	"core/shared/config"
	"core/shared/toolspec"
)

func TestLazyCompactAndContinueUsesOutgoingConfiguration(t *testing.T) {
	client := newLazyCompactionClient([]llm.CompactionResponse{workflowPostCompletionCompactionResponse("lazy-source")})
	f, approval := prepareLazyCompactionApproval(t, client)
	if calls := client.CompactionCalls(); len(calls) != 0 {
		t.Fatalf("source was pre-compacted: %d calls", len(calls))
	}
	if _, err := f.controller.ApplyPendingApproval(context.Background(), approval.ID); err != nil {
		t.Fatalf("apply target Approval: %v", err)
	}
	requests := f.waitForModelRequests(t, 3)
	if requests[1].Model != "workflow-coder" || requests[1].ReasoningEffort != "low" {
		t.Fatalf("source configuration = %q/%q, want workflow-coder/low", requests[1].Model, requests[1].ReasoningEffort)
	}
	if requests[2].Model != "workflow-reviewer" || requests[2].ReasoningEffort != "high" {
		t.Errorf("target configuration = %q/%q, want workflow-reviewer/high", requests[2].Model, requests[2].ReasoningEffort)
	}
	compactions := client.CompactionCalls()
	if len(compactions) != 1 {
		t.Fatalf("lazy compactions = %d, want one", len(compactions))
	}
	if compactions[0].Model != requests[1].Model || compactions[0].ReasoningEffort != requests[1].ReasoningEffort {
		t.Errorf("compaction configuration = %q/%q, want outgoing %q/%q",
			compactions[0].Model, compactions[0].ReasoningEffort, requests[1].Model, requests[1].ReasoningEffort)
	}
	requireLazyCompactionPreservesRequestPrefix(t, requests[1], compactions[0])
}

func TestDirectCompactAndContinueTaskInterruptPreservesOutgoingConfiguration(t *testing.T) {
	client := &heldLazyCompactionClient{
		compactingScriptedClient: NewCompactingScriptedClient(
			llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true, SupportsResponsesCompact: true},
			[]llm.CompactionResponse{workflowPostCompletionCompactionResponse("direct-interrupt")},
			ScriptedToolBatch("complete source", llm.ToolCall{
				ID: "complete-source", Name: string(toolspec.ToolCompleteNode),
				Input: json.RawMessage(`{"transition":"next","commentary":"done"}`),
			}),
		),
		started: make(chan context.Context, 1),
		release: make(chan struct{}),
	}
	f := newCurrentNodeRunnerFixtureWithClient(t, client)
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNative
	configureCompactionThinking(f)
	var once sync.Once
	unblock := func() { once.Do(func() { close(client.release) }) }
	t.Cleanup(unblock)
	workflowID := createCurrentNodeTwoStepWorkflow(t, f.store, "Direct interrupt",
		workflow.ContextModeCompactAndContinueSession,
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "coder", prompt: "Complete."},
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "reviewer", prompt: "Review."},
	)
	task := f.createTask(t, workflowID)
	source := f.startTask(t, task)
	var compactionContext context.Context
	select {
	case compactionContext = <-client.started:
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("direct continuation did not start compaction")
	}
	interrupted := make(chan error, 1)
	go func() {
		interrupted <- f.controller.Interrupt(t.Context(), workflowexecution.InterruptSelector{TaskID: task.ID})
	}()
	select {
	case <-compactionContext.Done():
	case err := <-interrupted:
		t.Fatalf("Task Interrupt returned without canceling direct compaction: %v", err)
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("Task Interrupt did not cancel direct compaction")
	}
	unblock()
	select {
	case err := <-interrupted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("Task Interrupt did not finish")
	}
	f.waitForTaskQuiescence(t, task.ID)
	nodes := f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
		return len(nodes) == 1 && !nodes[0].Reference.Equal(source) &&
			nodes[0].Scheduling != nil && nodes[0].Scheduling.Interruption != nil
	})
	if len(client.Requests()) != 1 || nodes[0].SessionID == nil {
		t.Fatal("interrupted direct continuation generated target input or lost the source Session")
	}
	meta := lazyCompactionSourceMeta(t, f, source)
	settings, err := launch.ResolveReadOnlySessionContextSettings(f.starter.cfg, meta, false)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Settings.Model != "workflow-coder" || settings.Settings.ThinkingLevel != "low" {
		t.Fatal("interrupted direct compaction changed outgoing model or Thinking")
	}
}

func newLazyCompactionClient(responses []llm.CompactionResponse) *compactingScriptedClient {
	return NewCompactingScriptedClient(
		llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true, SupportsResponsesCompact: true, SupportsPromptCacheKey: true},
		responses,
		ScriptedToolBatch("first", llm.ToolCall{ID: "first", Name: string(toolspec.ToolCompleteNode), Input: json.RawMessage(`{"transition":"next_1","commentary":"done"}`)}),
		ScriptedToolBatch("second", llm.ToolCall{ID: "second", Name: string(toolspec.ToolCompleteNode), Input: json.RawMessage(`{"transition":"next_2","commentary":"done"}`)}),
		ScriptedFinalAnswer(`{"commentary":"reviewed"}`),
	)
}

func prepareLazyCompactionApproval(t *testing.T, client currentNodeRunnerClient) (*currentNodeRunnerFixture, workflow.PendingApproval) {
	t.Helper()
	f := newCurrentNodeRunnerFixtureWithClient(t, client)
	// Enable compaction only after outgoing completion to exercise an
	// un-precompacted Session at the lazy continuation boundary.
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNone
	configureCompactionThinking(f)
	task := f.createTask(t, createCurrentNodeThreeStepWorkflow(
		t, f.store, "Lazy compaction configuration",
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "coder", prompt: "First."},
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "coder", prompt: "Second."},
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "reviewer", prompt: "Review."},
	))
	f.startTask(t, task)
	approval := f.waitForPendingApproval(t, task.ID)
	f.waitForTaskQuiescence(t, task.ID)
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNative
	return f, approval
}

func configureCompactionThinking(f *currentNodeRunnerFixture) {
	for role, effort := range map[string]string{"coder": "low", "reviewer": "high"} {
		settings := f.starter.cfg.Settings.Subagents[role]
		settings.Settings.ThinkingLevel = effort
		settings.Settings.ModelCapabilities.SupportsReasoningEffort = true
		settings.Sources["thinking_level"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}

		settings.Sources["model_capabilities"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model_capabilities"}}

		f.starter.cfg.Settings.Subagents[role] = settings
	}
}

func TestLazyCompactAndContinueFailureRetainsOutgoingConfiguration(t *testing.T) {
	client := newLazyCompactionClient(nil)
	f, approval := prepareLazyCompactionApproval(t, client)
	before := lazyCompactionSourceMeta(t, f, approval.Source)
	if calls := client.CompactionCalls(); len(calls) != 0 {
		t.Fatalf("source was pre-compacted: %d calls", len(calls))
	}
	if _, err := f.controller.ApplyPendingApproval(context.Background(), approval.ID); !errors.Is(err, ErrScriptedRuntime) {
		t.Fatalf("apply target Approval error = %v, want failed compaction", err)
	}
	f.waitForTaskQuiescence(t, approval.Source.TaskID)
	if calls := client.CompactionCalls(); len(calls) == 0 {
		t.Fatal("failed compaction was not attempted")
	}
	requireLazyCompactionRetainsOutgoingConfiguration(t, f, approval.Source, before)
}

type heldLazyCompactionClient struct {
	*compactingScriptedClient
	started chan context.Context
	release chan struct{}
}

func (c *heldLazyCompactionClient) Compact(ctx context.Context, request llm.CompactionRequest) (llm.CompactionResponse, error) {
	c.started <- ctx
	<-c.release
	return c.compactingScriptedClient.Compact(ctx, request)
}

func TestLazyCompactAndContinueTaskInterruptRetainsOutgoingConfiguration(t *testing.T) {
	client := &heldLazyCompactionClient{
		compactingScriptedClient: newLazyCompactionClient([]llm.CompactionResponse{workflowPostCompletionCompactionResponse("lazy-interrupt")}),
		started:                  make(chan context.Context, 1),
		release:                  make(chan struct{}),
	}
	f, approval := prepareLazyCompactionApproval(t, client)
	runLazyCompactionTaskInterrupt(t, f, approval, client, func() error {
		_, err := f.controller.ApplyPendingApproval(context.Background(), approval.ID)
		return err
	})
}

func runLazyCompactionTaskInterrupt(t *testing.T, f *currentNodeRunnerFixture, approval workflow.PendingApproval, client *heldLazyCompactionClient, apply func() error) {
	t.Helper()
	before := lazyCompactionSourceMeta(t, f, approval.Source)
	var release sync.Once
	unblock := func() { release.Do(func() { close(client.release) }) }
	t.Cleanup(unblock)
	applied := make(chan error, 1)
	go func() {
		applied <- apply()
	}()
	var compactionContext context.Context
	select {
	case compactionContext = <-client.started:
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("lazy compaction did not start")
	}
	target := approval.Branches[0].Target.CurrentNode.Reference.NodeID
	f.waitForCurrentNode(t, approval.Source.TaskID, func(nodes []workflow.CurrentNode) bool {
		return len(nodes) == 1 && nodes[0].Reference.NodeID == target
	})
	interrupted := make(chan error, 1)
	go func() {
		interrupted <- f.controller.Interrupt(context.Background(), workflowexecution.InterruptSelector{TaskID: approval.Source.TaskID})
	}()
	select {
	case <-compactionContext.Done():
	case err := <-interrupted:
		t.Fatalf("Task Interrupt returned before canceling compaction: %v", err)
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("Task Interrupt did not cancel compaction")
	}
	unblock()
	select {
	case err := <-interrupted:
		if err != nil {
			t.Fatalf("Task Interrupt: %v", err)
		}
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("Task Interrupt did not finish after compaction released")
	}
	select {
	case err := <-applied:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("apply target Approval: %v", err)
		}
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("Approval did not finish after compaction released")
	}
	f.waitForTaskQuiescence(t, approval.Source.TaskID)
	f.waitForCurrentNode(t, approval.Source.TaskID, func(nodes []workflow.CurrentNode) bool {
		return len(nodes) == 1 && nodes[0].Reference.NodeID == target &&
			nodes[0].Scheduling != nil && nodes[0].Scheduling.Interruption != nil
	})
	requireLazyCompactionRetainsOutgoingConfiguration(t, f, approval.Source, before)
}

func TestLazyCompactAndContinueManualMovePreservesRequestPrefix(t *testing.T) {
	client := newLazyCompactionClient([]llm.CompactionResponse{workflowPostCompletionCompactionResponse("lazy-move")})
	f, approval := prepareLazyCompactionApproval(t, client)
	apply := prepareLazyCompactionManualMove(t, f, approval)
	if err := apply(); err != nil {
		t.Fatalf("Manual Move: %v", err)
	}
	requests := f.waitForModelRequests(t, 3)
	compactions := client.CompactionCalls()
	if len(compactions) != 1 {
		t.Fatalf("Manual Move compactions = %d, want one", len(compactions))
	}
	requireLazyCompactionPreservesRequestPrefix(t, requests[1], compactions[0])
	if requests[2].Model != "workflow-reviewer" || requests[2].ReasoningEffort != "high" {
		t.Errorf("Manual Move target configuration = %q/%q, want workflow-reviewer/high", requests[2].Model, requests[2].ReasoningEffort)
	}
}

func TestLazyCompactAndContinueManualMoveTaskInterrupt(t *testing.T) {
	client := &heldLazyCompactionClient{
		compactingScriptedClient: newLazyCompactionClient([]llm.CompactionResponse{workflowPostCompletionCompactionResponse("lazy-move-interrupt")}),
		started:                  make(chan context.Context, 1),
		release:                  make(chan struct{}),
	}
	f, approval := prepareLazyCompactionApproval(t, client)
	runLazyCompactionTaskInterrupt(t, f, approval, client, prepareLazyCompactionManualMove(t, f, approval))
}

func TestLazyCompactAndContinueManualMoveFailureKeepsTarget(t *testing.T) {
	client := newLazyCompactionClient(nil)
	f, approval := prepareLazyCompactionApproval(t, client)
	before := lazyCompactionSourceMeta(t, f, approval.Source)
	if err := prepareLazyCompactionManualMove(t, f, approval)(); err != nil && !errors.Is(err, ErrScriptedRuntime) {
		t.Fatalf("Manual Move: %v", err)
	}
	target := approval.Branches[0].Target.CurrentNode.Reference.NodeID
	f.waitForCurrentNode(t, approval.Source.TaskID, func(nodes []workflow.CurrentNode) bool {
		return len(nodes) == 1 && nodes[0].Reference.NodeID == target &&
			nodes[0].Scheduling != nil && nodes[0].Scheduling.Interruption != nil
	})
	f.waitForTaskQuiescence(t, approval.Source.TaskID)
	if len(client.CompactionCalls()) == 0 {
		t.Fatal("Manual Move did not attempt compaction")
	}
	requireLazyCompactionRetainsOutgoingConfiguration(t, f, approval.Source, before)
}

func prepareLazyCompactionManualMove(t *testing.T, f *currentNodeRunnerFixture, approval workflow.PendingApproval) func() error {
	t.Helper()
	if len(approval.Branches) != 1 {
		t.Fatalf("Approval branches = %d, want one", len(approval.Branches))
	}
	prepared, err := f.store.PrepareManualMove(context.Background(), workflowstore.ManualMoveRequest{
		TaskID:       approval.Source.TaskID,
		TargetNodeID: approval.Branches[0].Target.CurrentNode.Reference.NodeID,
	})
	if err != nil {
		t.Fatalf("prepare Manual Move: %v", err)
	}
	return func() error {
		_, err := f.controller.ApplyManualMove(context.Background(), prepared, nil)
		return err
	}
}

func requireLazyCompactionPreservesRequestPrefix(t *testing.T, outgoing, compaction llm.Request) {
	t.Helper()
	if compaction.Model != outgoing.Model || compaction.ReasoningEffort != outgoing.ReasoningEffort ||
		compaction.SystemPrompt != outgoing.SystemPrompt || compaction.FastMode != outgoing.FastMode {
		t.Error("compaction changed outgoing model, Thinking, system prompt, or fast mode")
	}
	if outgoing.PromptCacheKey == "" || compaction.PromptCacheKey != outgoing.PromptCacheKey {
		t.Error("compaction did not preserve the nonempty outgoing prompt cache key")
	}
	if !reflect.DeepEqual(compaction.Tools, outgoing.Tools) {
		t.Error("compaction changed outgoing tool definitions")
	}
	if len(outgoing.Items) == 0 || len(compaction.Items) < len(outgoing.Items) {
		t.Fatalf("compaction items = %d, cannot preserve outgoing prefix of %d items", len(compaction.Items), len(outgoing.Items))
	}
	rawItems := 0
	for index, item := range outgoing.Items {
		if len(item.Raw) > 0 {
			rawItems++
		}
		if !bytes.Equal(item.Raw, compaction.Items[index].Raw) || !reflect.DeepEqual(item, compaction.Items[index]) {
			t.Errorf("compaction changed previously dispatched input item %d (type %s)", index, item.Type)
		}
	}
	if rawItems == 0 {
		t.Fatal("outgoing fixture contains no Raw input bytes to verify")
	}
}

func requireLazyCompactionRetainsOutgoingConfiguration(t *testing.T, f *currentNodeRunnerFixture, source workflow.CurrentNodeReference, before session.Meta) {
	t.Helper()
	requests := f.client.Requests()
	if len(requests) != 2 {
		t.Fatalf("generation requests = %d, want only two outgoing turns", len(requests))
	}
	if requests[1].Model != "workflow-coder" || requests[1].ReasoningEffort != "low" {
		t.Fatalf("source configuration = %q/%q, want workflow-coder/low", requests[1].Model, requests[1].ReasoningEffort)
	}
	meta := lazyCompactionSourceMeta(t, f, source)
	if meta.Locked == nil || meta.Locked.Model != "workflow-coder" {
		t.Errorf("Session contract = %+v, want outgoing model workflow-coder", meta.Locked)
	}
	if !reflect.DeepEqual(meta.ChatSettings, before.ChatSettings) ||
		!reflect.DeepEqual(meta.Continuation, before.Continuation) {
		t.Errorf("outgoing Session configuration changed: before settings=%+v continuation=%+v; after settings=%+v continuation=%+v",
			before.ChatSettings, before.Continuation, meta.ChatSettings, meta.Continuation)
	}
}

func lazyCompactionSourceMeta(t *testing.T, f *currentNodeRunnerFixture, source workflow.CurrentNodeReference) session.Meta {
	t.Helper()
	sourceSession, err := f.store.LatestTaskSessionForNode(context.Background(), source)
	if err != nil {
		t.Fatalf("resolve outgoing Session: %v", err)
	}
	record, err := f.metadata.ResolvePersistedSession(context.Background(), sourceSession.SessionID.String())
	if err != nil || record.Meta == nil {
		t.Fatalf("resolve outgoing Session metadata: %+v, %v", record, err)
	}
	return *record.Meta
}
