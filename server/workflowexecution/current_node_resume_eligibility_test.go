package workflowexecution

import (
	"context"
	"errors"
	"testing"

	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestCurrentNodeControllerResumeEligibilityRejectsTaskWithoutInterruptedExecutableCurrentNodes(t *testing.T) {
	taskID := workflow.TaskID("task-resume-eligibility-empty")
	store := &currentNodeControllerStore{}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	_, err := controller.PreflightTaskResume(context.Background(), taskID)

	var conflict *TaskResumeConflictError
	if !errors.As(err, &conflict) || conflict.TaskID != taskID {
		t.Fatalf("PreflightTaskResume error = %T %v, want conflict for %q", err, err, taskID)
	}
	if len(store.resumed) != 0 {
		t.Fatalf("resume mutations = %v, want none", store.resumed)
	}
}

func TestCurrentNodeControllerResumeEligibilityReturnsAllInvalidClassificationErrors(t *testing.T) {
	taskID := workflow.TaskID("task-resume-eligibility-invalid")
	reference := currentNodeReferenceForControllerTest(t, string(taskID), "node-review")
	store := &currentNodeControllerStore{
		resumeClassifications: []workflowstore.CurrentNodeResumeClassification{{
			CurrentNode: workflow.CurrentNode{Reference: reference},
			Diagnostics: []workflowstore.CurrentNodeResumeValidationDiagnostic{{
				Code:           workflowstore.CurrentNodeResumeParameterNotMaterializedCode,
				CurrentNode:    reference,
				EnteringEdgeID: workflow.EdgeID("edge-review"),
				ParameterKey:   "reviewer",
			}},
		}},
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	_, err := controller.PreflightTaskResume(context.Background(), taskID)

	var validationErr *workflowstore.CurrentNodeResumeValidationError
	if !errors.As(err, &validationErr) || len(validationErr.Diagnostics) != 1 {
		t.Fatalf("PreflightTaskResume error = %T %v, want typed validation error", err, err)
	}
	if len(store.resumed) != 0 {
		t.Fatalf("resume mutations = %v, want none", store.resumed)
	}
}

func TestCurrentNodeControllerResumeEligibilityAcceptsMixedValidAndInvalidClassifications(t *testing.T) {
	taskID := workflow.TaskID("task-resume-eligibility-mixed")
	invalidReference := currentNodeReferenceForControllerTest(t, string(taskID), "node-invalid")
	validReference := currentNodeReferenceForControllerTest(t, string(taskID), "node-valid")
	store := &currentNodeControllerStore{
		resumeClassifications: []workflowstore.CurrentNodeResumeClassification{
			{
				CurrentNode: workflow.CurrentNode{Reference: invalidReference},
				Diagnostics: []workflowstore.CurrentNodeResumeValidationDiagnostic{{
					Code:           workflowstore.CurrentNodeResumeParameterNotMaterializedCode,
					CurrentNode:    invalidReference,
					EnteringEdgeID: workflow.EdgeID("edge-invalid"),
					ParameterKey:   "reviewer",
				}},
			},
			{CurrentNode: workflow.CurrentNode{Reference: validReference}},
		},
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	preflight, err := controller.PreflightTaskResume(context.Background(), taskID)
	if err != nil {
		t.Fatalf("PreflightTaskResume: %v", err)
	}
	if preflight.Outcome != TaskResumePreflightResumable ||
		len(preflight.CurrentNodes) != 1 ||
		!preflight.CurrentNodes[0].Reference.Equal(validReference) {
		t.Fatalf("PreflightTaskResume result = %+v, want resumable %v", preflight, validReference)
	}
	if len(store.resumed) != 0 {
		t.Fatalf("resume mutations = %v, want none", store.resumed)
	}
}

func TestCurrentNodeControllerResumeEligibilityRejectsUnavailableTaskBeforeStorePreflight(t *testing.T) {
	taskID := workflow.TaskID("task-resume-eligibility-unavailable")
	reference := currentNodeReferenceForControllerTest(t, string(taskID), "node-review")
	key, err := reference.Key()
	if err != nil {
		t.Fatalf("Current Node key: %v", err)
	}
	store := &currentNodeControllerStore{}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})
	controller.mu.Lock()
	fence, err := controller.interrupts.beginTask(taskID)
	if err != nil {
		controller.mu.Unlock()
		t.Fatalf("begin Task interrupt fence: %v", err)
	}
	controller.interrupts.addCurrentNode(fence, key)
	controller.mu.Unlock()

	_, err = controller.PreflightTaskResume(context.Background(), taskID)

	if !errors.Is(err, ErrTaskExecutionNotQuiescent) {
		t.Fatalf("PreflightTaskResume error = %v, want %v", err, ErrTaskExecutionNotQuiescent)
	}
	if store.preflightResumeCalls != 0 {
		t.Fatalf("store preflight calls = %d, want none", store.preflightResumeCalls)
	}
	if len(store.resumed) != 0 {
		t.Fatalf("resume mutations = %v, want none", store.resumed)
	}
}

func TestCurrentNodeControllerReactivateWorkflowSessionRejectsPendingApprovalBeforeResume(t *testing.T) {
	taskID := workflow.TaskID("task-reactivate-pending-approval")
	sessionID := runtimeids.NewSessionID()
	reference := currentNodeReferenceForControllerTest(t, string(taskID), "node-review")
	store := &currentNodeControllerStore{
		currentNodes: []workflow.CurrentNode{{
			Reference: reference,
			SessionID: &sessionID,
		}},
		sessionTaskID: &taskID,
		sessionAssociation: &workflowstore.TaskSessionAssociation{
			SessionID:   sessionID,
			CurrentNode: reference,
		},
		pendingApprovals: []workflow.PendingApproval{{
			ID:     workflow.NewApprovalID(),
			Source: reference,
		}},
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	_, err := controller.ReactivateWorkflowSession(context.Background(), sessionID)

	var rejection *serverapi.WorkflowContinuationRejectionError
	if !errors.As(err, &rejection) ||
		rejection.TaskID != string(taskID) ||
		rejection.Reason != serverapi.WorkflowContinuationWaitingForApproval {
		t.Fatalf("ReactivateWorkflowSession error = %T %v, want pending-Approval rejection for %q", err, err, taskID)
	}
	if len(store.resumed) != 0 {
		t.Fatalf("resumed Current Nodes = %v, want none", store.resumed)
	}
}

func TestCurrentNodeControllerValidateWorkflowSessionContinuationUsesSelectedBranch(t *testing.T) {
	taskID := workflow.TaskID("task-selected-continuation-branch")
	selectedBranch := workflow.TransitionBranchKey("selected")
	selected, err := workflow.NewCurrentNodeReference(taskID, "node-review", &selectedBranch)
	if err != nil {
		t.Fatalf("selected Current Node reference: %v", err)
	}
	siblingBranch := workflow.TransitionBranchKey("sibling")
	sibling, err := workflow.NewCurrentNodeReference(taskID, "node-review", &siblingBranch)
	if err != nil {
		t.Fatalf("sibling Current Node reference: %v", err)
	}
	sessionID := runtimeids.NewSessionID()
	store := &currentNodeControllerStore{
		currentNodes: []workflow.CurrentNode{
			{Reference: selected, SessionID: &sessionID},
			{Reference: sibling},
		},
		sessionTaskID: &taskID,
		sessionAssociation: &workflowstore.TaskSessionAssociation{
			SessionID:   sessionID,
			CurrentNode: selected,
		},
		resumeClassifications: []workflowstore.CurrentNodeResumeClassification{
			{CurrentNode: workflow.CurrentNode{Reference: selected, SessionID: &sessionID}},
			{CurrentNode: workflow.CurrentNode{Reference: sibling}},
		},
		pendingApprovals: []workflow.PendingApproval{{Source: sibling}},
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	if err := controller.ValidateWorkflowSessionContinuation(context.Background(), sessionID); err != nil {
		t.Fatalf("ValidateWorkflowSessionContinuation with pending sibling: %v", err)
	}

	store.pendingApprovals = []workflow.PendingApproval{{Source: selected}}
	err = controller.ValidateWorkflowSessionContinuation(context.Background(), sessionID)
	var rejection *serverapi.WorkflowContinuationRejectionError
	if !errors.As(err, &rejection) ||
		rejection.TaskID != string(taskID) ||
		rejection.Reason != serverapi.WorkflowContinuationWaitingForApproval {
		t.Fatalf("selected pending continuation error = %T %v, want selected-branch rejection", err, err)
	}
	if len(store.resumed) != 0 {
		t.Fatalf("resumed Current Nodes = %v, want none", store.resumed)
	}
}
