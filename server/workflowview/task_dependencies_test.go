package workflowview

import (
	"core/internal/testharness/workflowfixture"
	"testing"

	"core/server/workflow"
	"core/server/workflowstore"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

func TestNewTaskDependenciesRequiresTaskStatusProjection(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	if _, err := NewTaskDependencies(fixture.metadata, nil, fixture.dependencyCounter); err == nil {
		t.Fatal("NewTaskDependencies accepted a nil TaskStatusProjection")
	}
}

func TestTaskDependenciesFocusedCountFollowsSatisfactionWithoutTouchingBlockedTask(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	dependencies, err := NewTaskDependencies(fixture.metadata, fixture.projection, fixture.dependencyCounter)
	if err != nil {
		t.Fatalf("NewTaskDependencies: %v", err)
	}
	emptyTask := createViewTask(t, fixture, "Empty")
	blocker := createViewTask(t, fixture, "Blocker")
	if _, err := fixture.store.AddTaskDependency(fixture.ctx, workflowstore.TaskDependencyAddRequest{BlockerTaskID: blocker.ID, BlockedTaskID: emptyTask.ID}); err != nil {
		t.Fatalf("add dependency: %v", err)
	}
	beforeUpdatedAt := viewTaskUpdatedAt(t, fixture, emptyTask.ID)
	focused, err := dependencies.CountUnsatisfiedBlockers(fixture.ctx, string(emptyTask.ID))
	if err != nil {
		t.Fatalf("CountUnsatisfiedBlockers: %v", err)
	}
	if focused != 1 {
		t.Fatalf("focused unsatisfied count = %d, want 1", focused)
	}
	doneDefinition, _, err := fixture.store.GetDefinition(fixture.ctx, fixture.workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	if _, err := workflowfixture.MoveTask(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.ManualMoveRequest{
		TaskID:       blocker.ID,
		TargetNodeID: terminalNodeID(t, doneDefinition),
	}); err != nil {
		t.Fatalf("complete blocker: %v", err)
	}
	focused, err = dependencies.CountUnsatisfiedBlockers(fixture.ctx, string(emptyTask.ID))
	if err != nil {
		t.Fatalf("CountUnsatisfiedBlockers after done: %v", err)
	}
	if focused != 0 {
		t.Fatalf("focused count after done = %d, want 0", focused)
	}
	if got := viewTaskUpdatedAt(t, fixture, emptyTask.ID); got != beforeUpdatedAt {
		t.Fatalf("blocked task timestamp = %d, want unchanged %d", got, beforeUpdatedAt)
	}
	if _, err := workflowfixture.MoveTask(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.ManualMoveRequest{
		TaskID:       blocker.ID,
		TargetNodeID: backlogNodeID(t, doneDefinition),
	}); err != nil {
		t.Fatalf("reopen blocker: %v", err)
	}
	focused, err = dependencies.CountUnsatisfiedBlockers(fixture.ctx, string(emptyTask.ID))
	if err != nil {
		t.Fatalf("CountUnsatisfiedBlockers after reopen: %v", err)
	}
	if focused != 1 {
		t.Fatalf("focused count after reopen = %d, want 1", focused)
	}
}

func TestListTaskDependenciesOmitsEmptyDirections(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	dependencies, err := NewTaskDependencies(fixture.metadata, fixture.projection, fixture.dependencyCounter)
	if err != nil {
		t.Fatalf("NewTaskDependencies: %v", err)
	}
	task := createViewTask(t, fixture, "Empty")

	listed, err := dependencies.ListTaskDependencies(fixture.ctx, string(task.ID), nil)
	if err != nil {
		t.Fatalf("ListTaskDependencies: %v", err)
	}
	if listed.Directions == nil || len(listed.Directions) != 0 {
		t.Fatalf("directions = %+v, want empty list", listed.Directions)
	}
	direction := taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS
	filtered, err := dependencies.ListTaskDependencies(
		fixture.ctx,
		string(task.ID),
		&direction,
	)
	if err != nil {
		t.Fatalf("ListTaskDependencies filtered: %v", err)
	}
	if filtered.Directions == nil || len(filtered.Directions) != 0 {
		t.Fatalf("filtered directions = %+v, want empty list", filtered.Directions)
	}
}

func TestListTaskDependenciesSortsBothDirectionsUnfinishedFirstThenShortID(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	dependencies, err := NewTaskDependencies(fixture.metadata, fixture.projection, fixture.dependencyCounter)
	if err != nil {
		t.Fatalf("NewTaskDependencies: %v", err)
	}
	subject := createViewTask(t, fixture, "Subject")
	blockerDone := createViewTask(t, fixture, "Blocker done")
	blockerOpenFirst := createViewTask(t, fixture, "Blocker open first")
	blockerOpenSecond := createViewTask(t, fixture, "Blocker open second")
	blockedDone := createViewTask(t, fixture, "Blocked done")
	blockedOpenFirst := createViewTask(t, fixture, "Blocked open first")
	blockedOpenSecond := createViewTask(t, fixture, "Blocked open second")
	for _, blocker := range []workflowstore.TaskRecord{blockerDone, blockerOpenFirst, blockerOpenSecond} {
		if _, err := fixture.store.AddTaskDependency(fixture.ctx, workflowstore.TaskDependencyAddRequest{BlockerTaskID: blocker.ID, BlockedTaskID: subject.ID}); err != nil {
			t.Fatalf("add blocked-by dependency: %v", err)
		}
	}
	for _, blocked := range []workflowstore.TaskRecord{blockedDone, blockedOpenFirst, blockedOpenSecond} {
		if _, err := fixture.store.AddTaskDependency(fixture.ctx, workflowstore.TaskDependencyAddRequest{BlockerTaskID: subject.ID, BlockedTaskID: blocked.ID}); err != nil {
			t.Fatalf("add blocks dependency: %v", err)
		}
	}
	definition, _, err := fixture.store.GetDefinition(fixture.ctx, fixture.workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	for _, task := range []workflowstore.TaskRecord{blockerDone, blockedDone} {
		if _, err := workflowfixture.MoveTask(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.ManualMoveRequest{
			TaskID:       task.ID,
			TargetNodeID: terminalNodeID(t, definition),
		}); err != nil {
			t.Fatalf("move %q to terminal: %v", task.ShortID, err)
		}
	}

	listed, err := dependencies.ListTaskDependencies(fixture.ctx, string(subject.ID), nil)
	if err != nil {
		t.Fatalf("ListTaskDependencies: %v", err)
	}
	assertDependencyItemOrder(t,
		listedDependencyDirection(t, listed, taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKED_BY).Items,
		blockerOpenFirst, blockerOpenSecond, blockerDone,
	)
	assertDependencyItemOrder(t,
		listedDependencyDirection(t, listed, taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS).Items,
		blockedOpenFirst, blockedOpenSecond, blockedDone,
	)
}

func createViewTask(t *testing.T, fixture currentNodeViewFixture, title string) workflowstore.TaskRecord {
	t.Helper()
	task, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  fixture.binding.ProjectID,
		WorkflowID: &fixture.workflowID,
		Title:      title,
		Body:       "Body",
	})
	if err != nil {
		t.Fatalf("CreateTask %q: %v", title, err)
	}
	return task
}

func terminalNodeID(t *testing.T, definition workflow.Definition) workflow.NodeID {
	t.Helper()
	for _, node := range definition.Nodes {
		if node.Kind() == workflow.NodeKindTerminal {
			return workflow.NodeIDOf(node)
		}
	}
	t.Fatal("terminal node missing")
	return workflow.NodeID("unreachable")
}

func backlogNodeID(t *testing.T, definition workflow.Definition) workflow.NodeID {
	t.Helper()
	for _, node := range definition.Nodes {
		if node.Kind() == workflow.NodeKindStart {
			return workflow.NodeIDOf(node)
		}
	}
	t.Fatal("start node missing")
	return workflow.NodeID("unreachable")
}

func listedDependencyDirection(t *testing.T, dependencies *taskpb.DependencyListSuccess, direction taskpb.DependencyDirection) *taskpb.DependencyListDirection {
	t.Helper()
	for _, candidate := range dependencies.Directions {
		if candidate.Direction == direction {
			return candidate
		}
	}
	t.Fatalf("dependency direction %q missing from %+v", direction, dependencies.Directions)
	return &taskpb.DependencyListDirection{}
}

func assertDependencyItemOrder(t *testing.T, items []*taskpb.DependencyItem, expected ...workflowstore.TaskRecord) {
	t.Helper()
	if len(items) != len(expected) {
		t.Fatalf("items = %+v, want %d items", items, len(expected))
	}
	for index, task := range expected {
		if items[index].TaskId != string(task.ID) {
			t.Fatalf("items = %+v, want task %q at index %d", items, task.ShortID, index)
		}
	}
}

func viewTaskUpdatedAt(t *testing.T, fixture currentNodeViewFixture, taskID workflow.TaskID) int64 {
	t.Helper()
	var updatedAt int64
	if err := fixture.metadata.DB().QueryRow(`SELECT updated_at_unix_ms FROM tasks WHERE id = ?`, taskID).Scan(&updatedAt); err != nil {
		t.Fatalf("read task timestamp: %v", err)
	}
	return updatedAt
}
