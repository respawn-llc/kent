package workflowrunner

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"core/internal/testharness/workflowfixture"
	"core/server/llm"
	agentruntime "core/server/runtime"
	"core/server/session"
	"core/server/sessionruntime"
	"core/server/tools"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestTransitionSelectedQuestionsSurviveRetainedToolsAndCompaction(t *testing.T) {
	f, input := newMaterializedRoleSelectionStart(t)
	client := NewCompactingScriptedClient(
		llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true, SupportsResponsesCompact: true},
		[]llm.CompactionResponse{workflowPostCompletionCompactionResponse("questions summary")},
		ScriptedToolBatch("question before", llm.ToolCall{ID: "question-before", Name: string(toolspec.ToolAskQuestion), Input: json.RawMessage(`{"question":"Continue?"}`)}),
		ScriptedCancellation(),
		ScriptedToolBatch("question after", llm.ToolCall{ID: "question-after", Name: string(toolspec.ToolAskQuestion), Input: json.RawMessage(`{"question":"Continue?"}`)}),
		ScriptedFinalAnswer(`{"commentary":"done"}`),
	)
	f.client = client
	unrestricted := f.starter.cfg
	var err error
	f.starter.cfg, err = config.ApplyLoadOptionsToSnapshot(f.starter.cfg, config.LoadOptions{Tools: "exec_command"})
	if err != nil {
		t.Fatal(err)
	}
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNative
	prepared, err := f.starter.PrepareCurrentNode(t.Context(), input, workflowruntime.TaskPromptDeliveryResume)
	if err != nil {
		t.Fatal(err)
	}
	steer := prepared.Assignment
	if err := steer.(workflowexecution.CurrentNodeAssignmentPreparation).Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	handle, err := f.starter.StartAgentCurrentNode(t.Context(), input.CurrentNode.Reference, workflowruntime.TaskPromptDeliveryAssignment, steer, nil, f.controller)
	if err != nil {
		t.Fatal(err)
	}
	association, err := f.store.LatestTaskSessionForNode(t.Context(), input.CurrentNode.Reference)
	if err != nil {
		t.Fatal(err)
	}
	id := association.SessionID
	answer := func() {
		t.Helper()
		deadline := time.Now().Add(currentNodeRunnerWait)
		for time.Now().Before(deadline) {
			pending := f.runtimes.ListPendingPrompts(id.String())
			if len(pending) != 0 {
				stepID, err := runtimeids.ParseStepID(pending[0].Request.StepID)
				if err != nil {
					t.Fatal(err)
				}
				results, err := f.authority.ResolvePromptBatch(t.Context(), id, stepID, []sessionruntime.PromptAnswerCommand{{
					ToolCallID: clientui.ToolCallID(pending[0].Request.ToolCallID),
					Payload:    sessionruntime.PromptQuestionAnswerCommand{Answer: tools.AskQuestionAnswer{Freeform: textutil.Value("continue")}},
				}})
				if err != nil || len(results) != 1 || results[0].Outcome != sessionruntime.PromptAnswerOutcomeResolved {
					t.Fatalf("Question was not accepted: %v %v", results, err)
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("Transition-selected Agent did not expose a pending Question")
	}
	answer()
	_, _ = handle.Wait(t.Context())
	f.waitForTaskQuiescence(t, input.Task.ID)
	before, err := f.metadata.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if before.Meta.RetainedToolSelection == nil || !reflect.DeepEqual(before.Meta.RetainedToolSelection.Tools, []toolspec.ID{toolspec.ToolExecCommand}) {
		t.Fatal("execution-required Questions changed the original saved list")
	}
	attachment := f.openRetainedRuntime(t, id)
	if err := f.authority.WithCurrentRuntime(t.Context(), id, func(ctx context.Context, engine *agentruntime.Engine) error {
		if err := engine.CompactContext(ctx, ""); err != nil {
			return err
		}
		deadline := time.Now().Add(currentNodeRunnerWait)
		for (engine.CompactionCount() == 0 || engine.ActiveRun() != nil) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if engine.CompactionCount() == 0 || engine.ActiveRun() != nil {
			t.Fatal("Question Session compaction did not finish")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := attachment.Release(t.Context(), sessionruntime.RuntimeReleaseClose); err != nil {
		t.Fatal(err)
	}
	f.starter.cfg = unrestricted
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNative
	if _, err := f.controller.ResumeTask(t.Context(), input.Task.ID, nil); err != nil {
		t.Fatal(err)
	}
	answer()
	f.waitForTaskQuiescence(t, input.Task.ID)
	after, err := f.metadata.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Meta.RetainedToolSelection, after.Meta.RetainedToolSelection) {
		t.Fatal("contract refresh or Questions changed the saved list")
	}
}

func TestCurrentNodeStartUsesMaterializedSelectedRoleAndForcesQuestions(t *testing.T) {
	f, input := newMaterializedRoleSelectionStart(t)
	if input.ExecutionRoot == nil {
		t.Fatal("current node start context omitted execution root")
	}
	_, err := f.starter.PrepareCurrentNode(context.Background(), input, workflowruntime.TaskPromptDeliveryAssignment)
	if err != nil {
		t.Fatalf("planCurrentNodeSession: %v", err)
	}
	requests := f.runtimeRequests()
	plan := requests[len(requests)-1]
	if plan.ActiveSettings.Model != "workflow-reviewer" {
		t.Fatalf("planned model = %q, want materialized reviewer model", plan.ActiveSettings.Model)
	}
	if !containsTool(plan.EnabledTools, toolspec.ToolAskQuestion) {
		t.Fatalf("planned tools = %+v, want forced ask_question", plan.EnabledTools)
	}
}

func TestCurrentNodeStartUsesMaterializedSelectedRoleAtCompactionBoundary(t *testing.T) {
	f, input := newMaterializedRoleSelectionStart(t, ScriptedFinalAnswer("source summary"))
	input.ContextMode = workflow.ContextModeCompactAndContinueSession
	input.EnteringEdge.ContextMode = workflow.ContextModeCompactAndContinueSession
	store, err := session.Create(
		filepath.Join(f.cfg.PersistenceRoot, "projects", input.Task.ProjectID, "sessions"),
		"sessions",
		f.workspace,
		sessioncontract.SessionCategoryMain,
		f.starter.storeOptions...,
	)
	if err != nil {
		t.Fatalf("create retained Session: %v", err)
	}
	if err := store.SetContinuationContext(session.ContinuationContext{
		AgentRole: textutil.Value("reviewer"),
	}); err != nil {
		t.Fatalf("set retained Session Agent: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("persist retained Session: %v", err)
	}
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("parse retained Session ID: %v", err)
	}
	input.CurrentNode.SessionID = &sessionID
	input.SourceSessionID = &sessionID
	if input.ExecutionRoot == nil {
		t.Fatal("current node start context omitted execution root")
	}
	_, err = f.starter.PrepareCurrentNode(context.Background(), input, workflowruntime.TaskPromptDeliveryAssignment)
	if err != nil {
		t.Fatalf("planCurrentNodeSession compact: %v", err)
	}
	requests := f.runtimeRequests()
	plan := requests[len(requests)-1]
	if plan.ActiveSettings.Model != "workflow-reviewer" {
		t.Fatalf("compact planned model = %q, want materialized reviewer model", plan.ActiveSettings.Model)
	}
	if !containsTool(plan.EnabledTools, toolspec.ToolAskQuestion) {
		t.Fatalf("compact planned tools = %+v, want forced ask_question", plan.EnabledTools)
	}
}

func TestCurrentNodeStartAppliesMaterializedWorkflowThinkingAfterRoleResolution(t *testing.T) {
	f, input := newMaterializedRoleSelectionStart(t)
	thinking, err := workflow.NewThinkingValue("max")
	if err != nil {
		t.Fatalf("NewThinkingValue: %v", err)
	}
	input.CurrentNode.AgentExecutionSelection.Thinking = &thinking
	if input.ExecutionRoot == nil {
		t.Fatal("current node start context omitted execution root")
	}
	_, err = f.starter.PrepareCurrentNode(context.Background(), input, workflowruntime.TaskPromptDeliveryAssignment)
	if err != nil {
		t.Fatalf("planCurrentNodeSession: %v", err)
	}
	requests := f.runtimeRequests()
	plan := requests[len(requests)-1]
	if plan.ActiveSettings.ThinkingLevel != "max" {
		t.Fatalf("planned thinking level = %q, want max", plan.ActiveSettings.ThinkingLevel)
	}
}

func TestCurrentNodeStartUsesConfiguredFallbackThinking(t *testing.T) {
	f, input := newMaterializedRoleSelectionStart(t)
	thinking, err := workflow.NewThinkingValue("high")
	if err != nil {
		t.Fatalf("NewThinkingValue: %v", err)
	}
	fallback, err := workflow.NewAgentExecutionSelection(
		"coder",
		&thinking,
		workflow.AssigneeOriginConfiguredFallback,
	)
	if err != nil {
		t.Fatalf("NewAgentExecutionSelection: %v", err)
	}
	input.CurrentNode.AgentExecutionSelection = &fallback
	if input.ExecutionRoot == nil {
		t.Fatal("current node start context omitted execution root")
	}
	_, err = f.starter.PrepareCurrentNode(context.Background(), input, workflowruntime.TaskPromptDeliveryAssignment)
	if err != nil {
		t.Fatalf("planCurrentNodeSession: %v", err)
	}
	requests := f.runtimeRequests()
	plan := requests[len(requests)-1]
	if plan.ActiveSettings.ThinkingLevel != "high" {
		t.Fatalf("planned thinking level = %q, want configured fallback high", plan.ActiveSettings.ThinkingLevel)
	}
}

func TestCurrentNodeStartFailsWhenMaterializedRoleIsRemovedFromConfig(t *testing.T) {
	f, input := newMaterializedRoleSelectionStart(t)
	delete(f.starter.cfg.Settings.Subagents, "reviewer")
	if input.ExecutionRoot == nil {
		t.Fatal("current node start context omitted execution root")
	}
	if _, err := f.starter.PrepareCurrentNode(context.Background(), input, workflowruntime.TaskPromptDeliveryAssignment); err == nil {
		t.Fatal("planCurrentNodeSession succeeded after removing materialized role")
	}
}

func newMaterializedRoleSelectionStart(t *testing.T, steps ...ScriptedRuntimeStep) (*currentNodeRunnerFixture, workflowstore.CurrentNodeStartContext) {
	t.Helper()
	f := newCurrentNodeRunnerFixture(t, steps...)
	workflowID := createCurrentNodeRoleSelectionWorkflow(t, f.store)
	task := f.createTask(t, workflowID)
	started, err := f.commitTaskStart(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	first := started.Mutation.Created[0]
	completed, err := f.commitCurrentNode(context.Background(), workflowstore.CurrentNodeCompletionRequest{
		Source:       first.Reference,
		TransitionID: "next",
		OutputValues: map[string]string{"role": "reviewer"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	target := completed.Mutation.Created[0]
	if target.AgentExecutionSelection == nil || target.AgentExecutionSelection.Assignee != "reviewer" {
		t.Fatalf("materialized selection = %+v, want reviewer", target.AgentExecutionSelection)
	}
	input, err := f.store.ResolveCurrentNodeStartContext(context.Background(), target.Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext: %v", err)
	}
	return f, input
}

func containsTool(tools []toolspec.ID, want toolspec.ID) bool {
	for _, tool := range tools {
		if tool == want {
			return true
		}
	}
	return false
}

func createCurrentNodeRoleSelectionWorkflow(t *testing.T, store *workflowstore.Store) runtimeids.WorkflowID {
	t.Helper()
	ctx := context.Background()
	created, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Current Node materialized role"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	firstID := workflow.NodeID(runtimeids.NewGraphEntityID())
	secondID := workflow.NodeID(runtimeids.NewGraphEntityID())
	startGroup := workflow.TransitionGroupID(runtimeids.NewGraphEntityID())
	nextGroup := workflow.TransitionGroupID(runtimeids.NewGraphEntityID())
	doneGroup := workflow.TransitionGroupID(runtimeids.NewGraphEntityID())
	workflowfixture.SaveStoreGraph(t, ctx, store, created.ID, func(definition workflow.Definition, request *workflowstore.WorkflowGraphSaveRequest) {
		start := nodeByKindRunnerTest(t, definition, workflow.NodeKindStart)
		done := nodeByKindRunnerTest(t, definition, workflow.NodeKindTerminal)
		request.Nodes = append(request.Nodes,
			workflowstore.NodeRecord{ID: firstID, WorkflowID: created.ID, Key: "first", Kind: workflow.NodeKindAgent, DisplayName: "First", SubagentRole: "coder"},
			workflowstore.NodeRecord{ID: secondID, WorkflowID: created.ID, Key: "second", Kind: workflow.NodeKindAgent, DisplayName: "Second", SubagentRole: "coder"},
		)
		request.TransitionGroups = append(request.TransitionGroups,
			workflowstore.TransitionGroupRecord{ID: startGroup, WorkflowID: created.ID, SourceNodeID: workflow.NodeIDOf(start), TransitionID: "start", DisplayName: "Start"},
			workflowstore.TransitionGroupRecord{ID: nextGroup, WorkflowID: created.ID, SourceNodeID: firstID, TransitionID: "next", DisplayName: "Next"},
			workflowstore.TransitionGroupRecord{ID: doneGroup, WorkflowID: created.ID, SourceNodeID: secondID, TransitionID: "done", DisplayName: "Done"},
		)
		request.Edges = append(request.Edges,
			workflowstore.EdgeRecord{ID: workflow.EdgeID(runtimeids.NewGraphEntityID()), WorkflowID: created.ID, TransitionGroupID: startGroup, Key: "start", TargetNodeID: firstID, AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured, ContextMode: workflow.ContextModeNewSession, PromptTemplate: "First."},
			workflowstore.EdgeRecord{
				ID: workflow.EdgeID(runtimeids.NewGraphEntityID()), WorkflowID: created.ID, TransitionGroupID: nextGroup, Key: "next", TargetNodeID: secondID,
				AssigneeSelection: workflow.AssigneeSelectionPreviousNode, ThinkingSelection: workflow.ThinkingSelectionConfigured, ContextMode: workflow.ContextModeNewSession,
				PromptTemplate: "Second.", Parameters: []workflow.Parameter{{Key: "role", Purpose: workflow.ParameterPurposeTargetAssignee}},
			},
			workflowstore.EdgeRecord{ID: workflow.EdgeID(runtimeids.NewGraphEntityID()), WorkflowID: created.ID, TransitionGroupID: doneGroup, Key: "done", TargetNodeID: workflow.NodeIDOf(done), AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured, ContextMode: workflow.ContextModeNewSession},
		)
	})
	return created.ID
}

func nodeByKindRunnerTest(t *testing.T, definition workflow.Definition, kind workflow.NodeKind) workflow.Node {
	t.Helper()
	for _, node := range definition.Nodes {
		if node.Kind() == kind {
			return node
		}
	}
	t.Fatalf("node kind %q not found", kind)
	return nil
}
