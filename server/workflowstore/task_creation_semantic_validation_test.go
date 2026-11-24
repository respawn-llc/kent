package workflowstore

import (
	"context"
	"testing"

	"core/server/workflow"
	"core/shared/runtimeids"
)

func TestCreateTaskForLinkedWorkflowWithSemanticIssuesStartsAtBacklog(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createSemanticallyInvalidWorkflow(t, ctx, store, binding.ProjectID, false)

	task, err := store.CreateTask(ctx, CreateTaskRequest{
		ProjectID:  binding.ProjectID,
		WorkflowID: &workflowID,
		Title:      "Backlog task",
		Body:       "Create against a non-ready linked Workflow.",
	})
	if err != nil {
		t.Fatalf("CreateTask with semantically invalid linked Workflow: %v", err)
	}
	if task.WorkflowID != workflowID {
		t.Fatalf("created Task Workflow = %s, want %s", task.WorkflowID, workflowID)
	}

	assertTaskStartsAtBacklog(t, ctx, store, task)
}

func TestCreateTaskUsesSemanticallyInvalidDefaultWorkflowAtBacklog(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createSemanticallyInvalidWorkflow(t, ctx, store, binding.ProjectID, true)

	task, err := store.CreateTask(ctx, CreateTaskRequest{
		ProjectID: binding.ProjectID,
		Title:     "Default Workflow task",
		Body:      "Create against a non-ready default Workflow.",
	})
	if err != nil {
		t.Fatalf("CreateTask with semantically invalid default Workflow: %v", err)
	}
	if task.WorkflowID != workflowID {
		t.Fatalf("created Task Workflow = %s, want default %s", task.WorkflowID, workflowID)
	}

	assertTaskStartsAtBacklog(t, ctx, store, task)
}

func createSemanticallyInvalidWorkflow(
	t *testing.T,
	ctx context.Context,
	store *Store,
	projectID string,
	isDefault bool,
) runtimeids.WorkflowID {
	t.Helper()
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, projectID, workflowID, isDefault)
	saved := saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		for index := range req.Edges {
			if req.Edges[index].Key != "done" {
				continue
			}
			req.Edges[index].AssigneeSelection = workflow.AssigneeSelectionPreviousNode
			req.Edges[index].Parameters = []workflow.Parameter{{
				Key:     "role",
				Purpose: workflow.ParameterPurposeTargetAssignee,
			}}
		}
	})
	if !hasWorkflowValidationCode(saved.ValidationErrors, workflow.CodeAssigneeSelectionInapplicable) {
		t.Fatalf("saved draft diagnostics = %+v, want semantically inapplicable Assignee selector", saved.ValidationErrors)
	}
	return workflowID
}

func assertTaskStartsAtBacklog(t *testing.T, ctx context.Context, store *Store, task TaskRecord) {
	t.Helper()
	definition, _, err := store.GetDefinition(ctx, task.WorkflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := workflow.NodeIDOf(nodeByKind(t, definition, workflow.NodeKindStart))
	backlog, err := workflow.NewCurrentNodeReference(task.ID, start, nil)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference: %v", err)
	}
	currentNodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListCurrentNodes: %v", err)
	}
	if len(currentNodes) != 1 ||
		!currentNodes[0].Reference.Equal(backlog) ||
		currentNodes[0].Scheduling != nil ||
		currentNodes[0].SessionID != nil {
		t.Fatalf("created Task Current Nodes = %+v, want one unscheduled Start Current Node", currentNodes)
	}
}
