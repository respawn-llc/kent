package workflowrunner

import (
	"context"
	"errors"
	"testing"

	"core/server/llm"
	"core/server/workflow"
	"core/shared/config"
)

func TestRetainedRuntimeCompactAndContinueUsesTargetConfiguration(t *testing.T) {
	runRetainedRuntimeConfiguration(t, workflow.ContextModeCompactAndContinueSession, false)
}

func TestRetainedRuntimeContinuePreservesOutgoingConfiguration(t *testing.T) {
	runRetainedRuntimeConfiguration(t, workflow.ContextModeContinueSession, false)
}

func TestRetainedRuntimeCACResumePreservesEstablishedConfiguration(t *testing.T) {
	runRetainedRuntimeConfiguration(t, workflow.ContextModeCompactAndContinueSession, true)
}

func runRetainedRuntimeConfiguration(t *testing.T, mode workflow.ContextMode, resume bool) {
	t.Helper()
	steps := []ScriptedRuntimeStep{
		ScriptedFinalAnswer(`{"transition":"next","commentary":"ready"}`),
		ScriptedFinalAnswer(`{"commentary":"reviewed"}`),
	}
	if resume {
		steps[1] = ScriptedCancellation()
		steps = append(steps, ScriptedFinalAnswer(`{"commentary":"resumed"}`))
	}
	client := NewCompactingScriptedClient(
		llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true, SupportsResponsesCompact: true},
		[]llm.CompactionResponse{workflowPostCompletionCompactionResponse("retained-source")},
		steps...,
	)
	f := newCurrentNodeRunnerFixtureWithClient(t, client)
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNative
	writeCurrentNodeConfig(t, f.starter.cfg)
	configureCompactionThinking(t, f)
	workflowID := createCurrentNodeTwoStepWorkflowWithTransition(
		t, f.store, "Retained runtime role boundary",
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "coder", prompt: "Complete."},
		currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "reviewer", prompt: "Review."},
		currentNodeLinearTransition{
			id: "next", mode: mode, requiresApproval: true,
		},
	)
	task := f.createTask(t, workflowID)
	source := f.startTask(t, task)
	approval := f.waitForPendingApproval(t, task.ID)
	f.waitForTaskQuiescence(t, task.ID)
	association, err := f.store.LatestTaskSessionForNode(context.Background(), source)
	if err != nil {
		t.Fatalf("resolve retained source Session: %v", err)
	}
	f.openRetainedRuntime(t, association.SessionID)
	if _, err := f.controller.ApplyPendingApproval(context.Background(), approval.ID); err != nil {
		t.Fatalf("apply target Approval: %v", err)
	}
	requests := f.waitForModelRequests(t, 2)
	if requests[0].Model != "workflow-coder" || requests[0].ReasoningEffort != "low" {
		t.Fatalf("source configuration = %q/%q, want workflow-coder/low", requests[0].Model, requests[0].ReasoningEffort)
	}
	wantModel, wantThinking := "workflow-reviewer", "high"
	wantCompactions := 1
	if mode == workflow.ContextModeContinueSession {
		wantModel, wantThinking = "workflow-coder", "low"
		wantCompactions = 0
	}
	if requests[1].Model != wantModel || requests[1].ReasoningEffort != wantThinking {
		t.Fatalf("target configuration = %q/%q, want %s/%s", requests[1].Model, requests[1].ReasoningEffort, wantModel, wantThinking)
	}
	if requests[1].SessionID == nil || *requests[1].SessionID != association.SessionID.String() {
		t.Fatalf("target Session = %v, want retained %s", requests[1].SessionID, association.SessionID)
	}
	compactions := client.CompactionCalls()
	if len(compactions) != wantCompactions {
		t.Fatalf("compactions = %d, want %d", len(compactions), wantCompactions)
	}
	for _, compaction := range compactions {
		if compaction.Model != "workflow-coder" || compaction.ReasoningEffort != "low" {
			t.Fatalf("compaction configuration = %q/%q, want outgoing coder/low", compaction.Model, compaction.ReasoningEffort)
		}
	}
	f.waitForTaskQuiescence(t, task.ID)
	if resume {
		role := f.starter.cfg.Settings.Subagents["reviewer"]
		role.Settings.Model = "changed-reviewer"
		f.starter.cfg.Settings.Subagents["reviewer"] = role
		writeCurrentNodeConfig(t, f.starter.cfg)
		if _, err := f.controller.ResumeTask(context.Background(), task.ID, nil); err != nil {
			t.Fatalf("resume interrupted target: %v", err)
		}
		requests = f.waitForModelRequests(t, 3)
		if requests[2].Model != wantModel || requests[2].ReasoningEffort != wantThinking {
			t.Fatalf("resumed configuration = %q/%q, want established %s/%s", requests[2].Model, requests[2].ReasoningEffort, wantModel, wantThinking)
		}
		if len(client.CompactionCalls()) != wantCompactions {
			t.Fatal("resume compacted the established target again")
		}
		f.waitForTaskQuiescence(t, task.ID)
	}
}

func TestRetainedRuntimeCACFailurePreservesOutgoingConfiguration(t *testing.T) {
	client := newLazyCompactionClient(nil)
	f, approval := prepareLazyCompactionApproval(t, client)
	before := lazyCompactionSourceMeta(t, f, approval.Source)
	association, err := f.store.LatestTaskSessionForNode(context.Background(), approval.Source)
	if err != nil {
		t.Fatalf("resolve retained source Session: %v", err)
	}
	f.openRetainedRuntime(t, association.SessionID)
	if _, err := f.controller.ApplyPendingApproval(context.Background(), approval.ID); !errors.Is(err, ErrScriptedRuntime) {
		t.Fatalf("Approval error = %v, want failed compaction", err)
	}
	f.waitForTaskQuiescence(t, approval.Source.TaskID)
	requireLazyCompactionRetainsOutgoingConfiguration(t, f, approval.Source, before)
	compactions := client.CompactionCalls()
	if len(compactions) == 0 {
		t.Fatal("failed compaction was not attempted")
	}
	for _, compaction := range compactions {
		if compaction.Model != "workflow-coder" || compaction.ReasoningEffort != "low" {
			t.Fatalf("failed compaction configuration = %q/%q, want outgoing coder/low", compaction.Model, compaction.ReasoningEffort)
		}
	}
}
