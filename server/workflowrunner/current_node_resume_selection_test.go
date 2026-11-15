package workflowrunner

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"core/internal/testharness/workflowfixture"
	"core/server/sessionstore"
	"core/server/workflow"
	"core/server/workflowconfig"
	"core/server/workflowexecution"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/config"
	"core/shared/textutil"
)

func TestResumeDerivesNewProtectedPromptInputsWithoutBackfillingHistory(t *testing.T) {
	f, input := newMaterializedRoleSelectionStart(t)
	role := f.cfg.Settings.Subagents["reviewer"]
	role.Settings.Model = "gpt-6-sol"
	f.cfg.Settings.Subagents["reviewer"] = role
	writeCurrentNodeConfig(t, f.cfg)
	input.EnteringEdge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
	input.EnteringEdge.Parameters = append(input.EnteringEdge.Parameters, workflow.Parameter{
		Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking,
	})
	input.TransitionPrompt = `{"effort":"{{.Params.effort}}"}`
	thinking, err := workflow.NewThinkingValue("high")
	if err != nil {
		t.Fatal(err)
	}
	input.CurrentNode.AgentExecutionSelection.Thinking = &thinking
	prepared, err := f.starter.PrepareCurrentNode(t.Context(), input, workflowruntime.TaskPromptDeliveryResume)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Assignment.(workflowexecution.CurrentNodeAssignmentPreparation).Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	record, err := f.metadata.ResolvePersistedSession(t.Context(), prepared.Session.SessionID.String())
	if err != nil {
		t.Fatal(err)
	}
	var rendered struct {
		Effort string `json:"effort"`
	}
	if err := json.Unmarshal([]byte(record.Meta.FirstPromptPreview), &rendered); err != nil {
		t.Fatal(err)
	}
	if rendered.Effort != string(thinking) {
		t.Fatalf("rendered effort = %q, want saved thinking", rendered.Effort)
	}
	if _, exists := input.CurrentNode.CurrentInputValues["effort"]; exists {
		t.Fatal("Resume backfilled historical input values")
	}
}

func TestResumeRetainsSavedModelWhenAgentRoleWasDeleted(t *testing.T) {
	f := newCurrentNodeRunnerFixture(t, ScriptedCancellation(), ScriptedFinalAnswer(`{"commentary":"done"}`))
	task := f.createTask(t, createCurrentNodeAgentWorkflow(t, f.store))
	f.startTask(t, task)
	before := f.waitForModelRequests(t, 1)[0]
	f.waitForTaskQuiescence(t, task.ID)
	current, err := f.store.ListCurrentNodes(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved := current[0].AgentExecutionSelection.Clone()
	delete(f.cfg.Settings.Subagents, saved.Assignee)
	writeCurrentNodeConfig(t, f.cfg)
	f.restartRuntime(t)
	if _, err := f.controller.ResumeTask(t.Context(), task.ID, nil); err != nil {
		t.Fatal(err)
	}
	after := f.waitForModelRequests(t, 2)[1]
	f.waitForTaskQuiescence(t, task.ID)
	if after.Model != before.Model || after.SessionID == nil || before.SessionID == nil || *after.SessionID != *before.SessionID {
		t.Fatalf("Resume changed saved Session/model: before=%v/%s after=%v/%s", before.SessionID, before.Model, after.SessionID, after.Model)
	}
}

func TestResumeUsesSavedSettingsWhenThinkingSelectionIsAbsent(t *testing.T) {
	for _, referenced := range []bool{false, true} {
		t.Run(map[bool]string{false: "no placeholder", true: "new placeholder"}[referenced], func(t *testing.T) {
			f, input := newMaterializedRoleSelectionStart(t)
			requests := f.runtimeRequests()
			savedThinking := requests[len(requests)-1].ActiveSettings.ThinkingLevel
			input.EnteringEdge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
			input.EnteringEdge.Parameters = append(input.EnteringEdge.Parameters, workflow.Parameter{
				Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking,
			})
			input.CurrentNode.AgentExecutionSelection.Thinking = nil
			input.TransitionPrompt = `{}`
			if referenced {
				input.TransitionPrompt = `{"effort":"{{.Params.effort}}"}`
			}
			prepared, err := f.starter.PrepareCurrentNode(t.Context(), input, workflowruntime.TaskPromptDeliveryResume)
			if err != nil {
				t.Fatal(err)
			}
			if err := prepared.Assignment.(workflowexecution.CurrentNodeAssignmentPreparation).Prepare(t.Context()); err != nil {
				t.Fatal(err)
			}
			record, err := f.metadata.ResolvePersistedSession(t.Context(), prepared.Session.SessionID.String())
			if err != nil {
				t.Fatal(err)
			}
			var rendered map[string]string
			if err := json.Unmarshal([]byte(record.Meta.FirstPromptPreview), &rendered); err != nil {
				t.Fatal(err)
			}
			effort, present := rendered["effort"]
			if present != referenced || (referenced && effort != savedThinking) {
				t.Fatalf("default thinking projection = %v, referenced=%v", rendered, referenced)
			}
			requests = f.runtimeRequests()
			if got := requests[len(requests)-1].ActiveSettings.ThinkingLevel; got != savedThinking {
				t.Fatalf("Resume changed effective thinking to %q", got)
			}
		})
	}
}

func TestResumeRetainsThinkingAfterIncomingSelectorEdit(t *testing.T) {
	f, input := newMaterializedRoleSelectionStart(t)
	savedThinking := "high"
	descriptor, err := session.NewOpenSessionDescriptor(*input.CurrentNode.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.authority.WithDormantSessionStore(t.Context(), descriptor, func(_ context.Context, store *session.Store) error {
		return store.SetThinkingOverride(textutil.Value(savedThinking))
	}); err != nil {
		t.Fatal(err)
	}
	input.ContextMode = workflow.ContextModeContinueSession
	input.EnteringEdge.ContextMode = workflow.ContextModeContinueSession
	input.EnteringEdge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceImmediateSource}
	input.EnteringEdge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
	input.CurrentNode.AgentExecutionSelection.Origin = workflow.AssigneeOriginRetainedSession
	input.CurrentNode.AgentExecutionSelection.Thinking = nil
	if _, err := f.starter.PrepareCurrentNode(t.Context(), input, workflowruntime.TaskPromptDeliveryResume); err != nil {
		t.Fatal(err)
	}
	requests := f.runtimeRequests()
	if got := requests[len(requests)-1].ActiveSettings.ThinkingLevel; got != savedThinking {
		t.Fatalf("edited selector changed saved thinking from %q to %q", savedThinking, got)
	}
}

func TestResumeAfterWorkflowEditRecoversDeletedAgentAndProjectsNewThinkingInput(t *testing.T) {
	f := newCurrentNodeRunnerFixture(t,
		ScriptedFinalAnswer(`{"transition":"next","commentary":"ready"}`),
		ScriptedCancellation(), ScriptedCancellation(),
	)
	f.cfg.Settings.CompactionMode = config.CompactionModeNone
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNone
	writeCurrentNodeConfig(t, f.cfg)
	var err error
	f.store, err = workflowstore.New(f.metadata, workflowstore.WithRoleResolver(workflowconfig.Catalog{App: f.cfg}))
	if err != nil {
		t.Fatal(err)
	}
	f.restartRuntime(t)
	workflowID := createCurrentNodeTwoStepWorkflowWithTransition(t, f.store, "Resume edited Workflow",
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "coder", prompt: "Implement."},
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "reviewer", prompt: "Review."},
		currentNodeLinearTransition{id: "next", mode: workflow.ContextModeNewSession},
	)
	task := f.createTask(t, workflowID)
	f.startTask(t, task)
	beforeRequest := f.waitForModelRequests(t, 2)[1]
	f.waitForTaskQuiescence(t, task.ID)
	before, err := f.store.ListCurrentNodes(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	current := before[0]
	workflowfixture.SaveStoreGraph(t, t.Context(), f.store, workflowID, func(_ workflow.Definition, request *workflowstore.WorkflowGraphSaveRequest) {
		for i := range request.Nodes {
			if request.Nodes[i].ID == current.Reference.NodeID {
				request.Nodes[i].SubagentRole = "coder"
			}
		}
		for i := range request.Edges {
			if request.Edges[i].TargetNodeID == current.Reference.NodeID {
				request.Edges[i].ThinkingSelection = workflow.ThinkingSelectionPreviousNode
				request.Edges[i].Parameters = []workflow.Parameter{{Key: "effort", Purpose: workflow.ParameterPurposeTargetThinking}}
				request.Edges[i].PromptTemplate = `{"effort":"{{.Params.effort}}"}`
			}
		}
	})
	delete(f.cfg.Settings.Subagents, "reviewer")
	delete(f.cfg.Settings.Subagents, "coder")
	writeCurrentNodeConfig(t, f.cfg)
	f.restartRuntime(t)
	if _, err := f.controller.ResumeTask(t.Context(), task.ID, nil); err != nil {
		t.Fatal(err)
	}
	afterRequest := f.waitForModelRequests(t, 3)[2]
	f.waitForTaskQuiescence(t, task.ID)
	if afterRequest.Model != beforeRequest.Model || afterRequest.ReasoningEffort != beforeRequest.ReasoningEffort {
		t.Fatalf("Resume changed model/thinking from %s/%s to %s/%s", beforeRequest.Model, beforeRequest.ReasoningEffort, afterRequest.Model, afterRequest.ReasoningEffort)
	}
	after, err := f.store.ListCurrentNodes(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumed := after[0]
	if *resumed.SessionID != *current.SessionID ||
		!reflect.DeepEqual(resumed.AgentExecutionSelection, current.AgentExecutionSelection) ||
		!reflect.DeepEqual(resumed.CurrentInputValues, current.CurrentInputValues) ||
		!reflect.DeepEqual(resumed.PriorValues, current.PriorValues) {
		t.Fatal("Resume changed saved selection, Session, or historical Parameters")
	}
	record, err := f.metadata.ResolvePersistedSession(t.Context(), resumed.SessionID.String())
	if err != nil {
		t.Fatal(err)
	}
	var projection struct {
		Effort string `json:"effort"`
	}
	if err := json.Unmarshal([]byte(record.Meta.FirstPromptPreview), &projection); err != nil {
		t.Fatal(err)
	}
	if projection.Effort != beforeRequest.ReasoningEffort {
		t.Fatalf("new thinking input = %q, want saved %q", projection.Effort, beforeRequest.ReasoningEffort)
	}
}
