package workflowview

import (
	"testing"

	"core/server/workflow"
	"core/server/workflowexecution"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type staticTaskStatusLiveObservationSource struct {
	observation workflowexecution.WorkflowTaskExecutionObservation
}

func TestTaskDetailOffersResumeForSavedAdmissionWithoutLiveExecution(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "saved admission")
	projected, err := fixture.detail.GetTask(t.Context(), string(started.task.ID))
	if err != nil {
		t.Fatalf("TaskDetail.GetTask: %v", err)
	}
	if projected.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED {
		t.Fatalf("saved admission status = %v, want interrupted", projected.Status.Kind)
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

func TestSavedCurrentNodeStatusAcrossTaskSurfaces(t *testing.T) {
	for _, test := range []struct {
		name      string
		reason    *workflow.CurrentNodeInterruptionReason
		ready     bool
		want      taskpb.TaskStatusKind
		attention int32
	}{
		{name: "saved admission after restart", want: taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED},
		{name: "manual interruption", reason: new(workflow.CurrentNodeInterruptionReasonUserInterrupt), want: taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED},
		{name: "runtime canceled", reason: new(workflow.CurrentNodeInterruptionReasonRuntimeCanceled), want: taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED},
		{name: "saved server restart", reason: new(workflow.CurrentNodeInterruptionReason("server_restart")), want: taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED},
		{name: "execution error", reason: new(workflow.CurrentNodeInterruptionReason("execution_error")), want: taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED, attention: 1},
		{name: "ready but never admitted", ready: true, want: taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCurrentNodeViewFixture(t, false)
			started := fixture.startTask(t, test.name)
			if test.reason != nil {
				if err := fixture.store.InterruptCurrentNode(fixture.ctx, started.currentNode, *test.reason, workflow.CurrentNodeInterruptionDetail{Code: string(*test.reason)}); err != nil {
					t.Fatal(err)
				}
			}
			if test.ready {
				fixture.makeCurrentNodeReady(t, started.task.ID)
			}
			detail, err := fixture.detail.GetTask(fixture.ctx, string(started.task.ID))
			if err != nil {
				t.Fatal(err)
			}
			if detail.Status.Kind != test.want || !detail.Actions.CanResume || detail.Actions.CanInterrupt || detail.AttentionCount != test.attention {
				t.Fatalf("detail = %+v, want %v, Resume and %d attention items", detail, test.want, test.attention)
			}
			if len(detail.Status.AttentionTypes) != int(test.attention) {
				t.Fatalf("status attention types = %v, want %d kinds", detail.Status.AttentionTypes, test.attention)
			}
			attention, err := fixture.attention(t).ListTask(fixture.ctx, &taskpb.TaskAttentionListRequest{TaskId: string(started.task.ID)})
			if err != nil {
				t.Fatal(err)
			}
			if len(attention.Items) != int(test.attention) {
				t.Fatalf("attention = %+v, want %d items", attention.Items, test.attention)
			}
			for _, item := range attention.Items {
				if item.GetInterruptedCurrentNode().GetDetails() == nil {
					t.Fatal("error interruption is missing diagnostic details")
				}
			}
			inbox, err := fixture.attention(t).List(fixture.ctx, &taskpb.AttentionListRequest{PageSize: 20})
			if err != nil {
				t.Fatal(err)
			}
			if len(inbox.Items) != int(test.attention) {
				t.Fatalf("Inbox = %+v, want %d items", inbox.Items, test.attention)
			}
			list, err := fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
				ProjectId: &fixture.binding.ProjectID, LabelFilter: noLabelFilter(),
				StatusKinds: []taskpb.TaskStatusKind{test.want},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(list.Tasks) != 1 || list.Tasks[0].Status.Kind != test.want {
				t.Fatalf("list = %+v, want one %v Task", list.Tasks, test.want)
			}
			cards, err := fixture.board.ListNodeCards(fixture.ctx, &taskpb.BoardNodeCardsListRequest{
				ProjectId: fixture.binding.ProjectID, WorkflowId: fixture.workflowID.String(),
				NodeId: string(fixture.agentNodeID), PageSize: 20, LabelFilter: noLabelFilter(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(cards.Cards) != 1 || cards.Cards[0].Status.Kind != test.want || !cards.Cards[0].Actions.CanResume {
				t.Fatalf("board = %+v, want one resumable %v Task", cards.Cards, test.want)
			}
		})
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
	if projected.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_QUEUED ||
		!projected.Actions.CanResume ||
		projected.Actions.CanInterrupt {
		t.Fatalf("concurrency-queued Task detail = %+v", projected)
	}
}
