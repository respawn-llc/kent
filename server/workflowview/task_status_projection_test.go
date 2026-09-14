package workflowview

import (
	"testing"

	"core/server/workflow"
	"core/server/workflowexecution"
	"core/shared/serverapi"
)

type staticTaskStatusLiveObservationSource struct {
	observation workflowexecution.WorkflowTaskExecutionObservation
}

func TestTaskDetailOffersResumeForSavedAdmissionWithoutLiveExecution(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "saved admission")
	if _, err := fixture.store.AdmitCurrentNode(t.Context(), started.currentNode); err != nil {
		t.Fatalf("AdmitCurrentNode: %v", err)
	}
	projected, err := fixture.detail.GetTask(t.Context(), string(started.task.ID))
	if err != nil {
		t.Fatalf("TaskDetail.GetTask: %v", err)
	}
	if !projected.Actions.CanResume || projected.Actions.CanInterrupt {
		t.Fatalf("saved admission actions = %+v, want Resume without Interrupt", projected.Actions)
	}
	nodes, err := fixture.store.ListCurrentNodes(t.Context(), started.task.ID)
	if err != nil {
		t.Fatalf("ListCurrentNodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Scheduling.State != workflow.CurrentNodeSchedulingAdmitted {
		t.Fatalf("nodes after detail read = %+v, want unchanged admission", nodes)
	}
}

func (s staticTaskStatusLiveObservationSource) ObserveWorkflowTaskExecutions([]workflow.TaskID) (workflowexecution.WorkflowTaskExecutionObservation, error) {
	return s.observation, nil
}

type countingTaskStatusLiveObservationSource struct {
	source TaskStatusLiveObservationSource
	calls  *int
}

func (s countingTaskStatusLiveObservationSource) ObserveWorkflowTaskExecutions(taskIDs []workflow.TaskID) (workflowexecution.WorkflowTaskExecutionObservation, error) {
	*s.calls++
	return s.source.ObserveWorkflowTaskExecutions(taskIDs)
}

func TestTaskDetailProjectsConcurrencyQueuedCurrentNodeAsResumable(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "concurrency queued")
	detail := taskDetailWithObservation(t, fixture, workflowexecution.WorkflowTaskExecutionObservation{
		ConcurrencyQueued: map[workflow.TaskID][]workflow.CurrentNodeReference{
			started.task.ID: {started.currentNode},
		},
		Quiescence: map[workflow.TaskID]bool{started.task.ID: false},
	})

	projected, err := detail.GetTask(t.Context(), string(started.task.ID))
	if err != nil {
		t.Fatalf("TaskDetail.GetTask: %v", err)
	}
	if projected.Status.Kind != serverapi.WorkflowTaskStatusKindQueued ||
		!projected.Actions.CanResume ||
		projected.Actions.CanInterrupt {
		t.Fatalf("concurrency-queued Task detail = %+v", projected)
	}
}
