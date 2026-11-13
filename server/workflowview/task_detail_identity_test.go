package workflowview

import (
	"database/sql"
	"errors"
	"testing"

	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowstore"
)

func TestTaskDetailLookupByIDPreservesMissingResult(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	task := fixture.createBacklogTask(t, "Task detail identity")

	detail, err := fixture.detail.GetTask(fixture.ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if detail.Summary.Id != string(task.ID) {
		t.Fatalf("Task ID = %q, want %q", detail.Summary.Id, task.ID)
	}

	if _, err := fixture.detail.GetTask(fixture.ctx, "missing-task"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTask missing Task error = %v, want sql.ErrNoRows", err)
	}
}

func TestTaskDetailShortIDLookupsPreserveProjectAndCardinality(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	first := fixture.createBacklogTask(t, "First project Task")
	unique, err := fixture.detail.GetTaskByShortID(fixture.ctx, first.ShortID)
	if err != nil {
		t.Fatalf("GetTaskByShortID: %v", err)
	}
	if unique.Summary.Id != string(first.ID) {
		t.Fatalf("unique short ID resolved to %q, want %q", unique.Summary.Id, first.ID)
	}

	if _, err := fixture.detail.GetTaskByShortID(fixture.ctx, "missing-short-id"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTaskByShortID missing Task error = %v, want sql.ErrNoRows", err)
	}
	if _, err := fixture.detail.GetTaskByProjectShortID(fixture.ctx, fixture.binding.ProjectID, "missing-short-id"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTaskByProjectShortID missing Task error = %v, want sql.ErrNoRows", err)
	}

	otherBinding, err := fixture.metadata.RegisterWorkspaceBinding(fixture.ctx, t.TempDir())
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	if _, err := fixture.store.LinkWorkflowWithDefaultPolicy(
		fixture.ctx,
		otherBinding.ProjectID,
		fixture.workflowID,
		workflowstore.WorkflowLinkDefaultAlways,
	); err != nil {
		t.Fatalf("LinkWorkflow second project: %v", err)
	}
	workflowID := fixture.workflowID
	second, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:         otherBinding.ProjectID,
		WorkflowID:        &workflowID,
		Title:             "Second project Task",
		SourceWorkspaceID: otherBinding.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("CreateTask second project: %v", err)
	}

	const duplicateShortID = "DUP-1"
	for _, taskID := range []workflow.TaskID{first.ID, second.ID} {
		if _, err := fixture.metadata.DB().ExecContext(
			fixture.ctx,
			"UPDATE tasks SET short_id = ? WHERE id = ?",
			duplicateShortID,
			string(taskID),
		); err != nil {
			t.Fatalf("Set duplicate Task short ID: %v", err)
		}
	}

	for _, test := range []struct {
		projectID string
		wantTask  workflow.TaskID
	}{
		{projectID: fixture.binding.ProjectID, wantTask: first.ID},
		{projectID: otherBinding.ProjectID, wantTask: second.ID},
	} {
		detail, err := fixture.detail.GetTaskByProjectShortID(
			fixture.ctx,
			" "+test.projectID+" ",
			" "+duplicateShortID+" ",
		)
		if err != nil {
			t.Fatalf("GetTaskByProjectShortID: %v", err)
		}
		if detail.Summary.Id != string(test.wantTask) {
			t.Fatalf("project-scoped short ID resolved to %q, want %q", detail.Summary.Id, test.wantTask)
		}
	}

	if _, err := fixture.detail.GetTaskByShortID(fixture.ctx, duplicateShortID); err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ambiguous GetTaskByShortID error = %v, want a non-not-found error", err)
	}
}

func TestTaskDetailByProjectShortIDFailsIfTaskDisappearsBeforeProjection(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "Task removed during detail projection")
	projection, err := NewTaskStatusProjection(
		fixture.store,
		NewTaskProjector(),
		taskStatusLiveObservationSourceFunc(func(taskIDs []workflow.TaskID) (workflowexecution.WorkflowTaskExecutionObservation, error) {
			observation, err := fixture.quiescence.ObserveWorkflowTaskExecutions(taskIDs)
			if err != nil {
				return workflowexecution.WorkflowTaskExecutionObservation{}, err
			}
			if _, err := fixture.store.DeleteTask(fixture.ctx, started.task.ID); err != nil {
				return workflowexecution.WorkflowTaskExecutionObservation{}, err
			}
			return observation, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewTaskStatusProjection: %v", err)
	}
	dependencies, err := NewTaskDependencies(fixture.metadata, projection, fixture.dependencyCounter)
	if err != nil {
		t.Fatalf("NewTaskDependencies: %v", err)
	}
	detail, err := NewTaskDetail(fixture.metadata, projection, dependencies)
	if err != nil {
		t.Fatalf("NewTaskDetail: %v", err)
	}

	if _, err := detail.GetTaskByProjectShortID(fixture.ctx, fixture.binding.ProjectID, started.task.ShortID); err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTaskByProjectShortID after Task deletion error = %v, want a non-not-found projection error", err)
	}
}
