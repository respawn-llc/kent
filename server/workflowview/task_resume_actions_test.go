package workflowview

import (
	"testing"
	"time"

	"core/server/metadata/sqlitegen"
	"core/server/sessionruntime"
	"core/server/workflow"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"

	"github.com/google/uuid"
)

func TestTaskActionsOfferResumeForInterruptedSiblingOfRunningExecution(t *testing.T) {
	taskID := workflow.TaskID(uuid.NewString())
	projectID := uuid.NewString()
	workflowID := runtimeids.NewWorkflowID()
	running := workflow.CurrentNodeReference{TaskID: taskID, NodeID: workflow.NodeID(uuid.NewString())}
	interrupted := workflow.CurrentNodeReference{TaskID: taskID, NodeID: workflow.NodeID(uuid.NewString())}
	interruption := &workflow.CurrentNodeScheduling{
		State: workflow.CurrentNodeSchedulingInterrupted,
		Interruption: &workflow.CurrentNodeInterruption{
			Reason:     workflow.CurrentNodeInterruptionReasonUserInterrupt,
			OccurredAt: time.Now(),
			Detail:     workflow.CurrentNodeInterruptionDetail{Code: "user_interrupt"},
		},
	}
	for _, test := range []struct {
		name        string
		scheduling  *workflow.CurrentNodeScheduling
		alreadyLive bool
		done        bool
		wantResume  bool
	}{
		{name: "interrupted sibling", scheduling: interruption, wantResume: true},
		{name: "nonexecutable sibling"},
		{name: "noninterrupted sibling", scheduling: &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingAdmitted}},
		{name: "already running sibling", scheduling: interruption, alreadyLive: true},
		{name: "terminal task", scheduling: interruption, done: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			live := []sessionruntime.TaskExecution{{
				Ref: sessionruntime.WorkflowExecutionRef{
					ProjectID: projectID, WorkflowID: workflowID, CurrentNode: running,
				},
				Script: &sessionruntime.TaskScriptExecutionTarget{Path: "scripts/task.sh"},
			}}
			if test.alreadyLive {
				sibling := live[0]
				sibling.Ref.CurrentNode = interrupted
				live = append(live, sibling)
			}
			facts := NewTaskProjector().ProjectTaskFacts(TaskFactsInput{
				Task: sqlitegen.TaskRecord{ID: string(taskID), ProjectID: projectID, WorkflowID: workflowID},
				Status: workflowTaskStatusFact{
					Status: &taskpb.TaskStatus{Kind: taskpb.TaskStatusKind_TASK_STATUS_KIND_RUNNING},
					Done:   test.done,
				},
				CurrentNodes: []workflow.CurrentNode{
					{Reference: running, Scheduling: &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingAdmitted}},
					{Reference: interrupted, Scheduling: test.scheduling},
				},
				LiveExecutions: live,
			})
			if facts.Actions.CanResume != test.wantResume {
				t.Fatalf("CanResume = %t, want %t", facts.Actions.CanResume, test.wantResume)
			}
		})
	}
}
