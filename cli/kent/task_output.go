package main

import (
	"fmt"
	"io"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

func writeTaskStartResult(stdout io.Writer, task *taskpb.TaskDetail) error {
	if len(task.LiveSessions) == 1 {
		session := task.LiveSessions[0]
		identity, err := protoapi.SessionIdentityFromProto(session.SessionId, session.ShortId)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "Started task %s in session %s using workflow %q (%s).\n", taskDisplayID(task), identity.String(), task.Workflow.DisplayName, task.Workflow.WorkflowId)
		return err
	}
	if len(task.CurrentScripts) == 1 {
		fmt.Fprintf(stdout, "Started task %s with script %s using workflow %q (%s).\n", taskDisplayID(task), task.CurrentScripts[0].Path, task.Workflow.DisplayName, task.Workflow.WorkflowId)
		return nil
	}
	fmt.Fprintf(stdout, "Started task %s using workflow %q (%s).\n", taskDisplayID(task), task.Workflow.DisplayName, task.Workflow.WorkflowId)
	return nil
}

func writeTaskResumeResult(stdout io.Writer, task *taskpb.TaskDetail, resp *taskpb.ResumeApplied) error {
	fmt.Fprintf(stdout, "Resumed task %s.\n", taskDisplayID(task))
	for _, currentNode := range resp.CurrentNodes {
		if currentNode.SessionId == nil {
			fmt.Fprintf(stdout, "Resumed node %s.\n", currentNode.NodeId)
			continue
		}
		identity, err := protoapi.SessionIdentityFromProto(*currentNode.SessionId, currentNode.ShortId)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "Resumed node %s in session %s.\n", currentNode.NodeId, identity.String()); err != nil {
			return err
		}
	}
	return nil
}

func writeTaskLifecycleResult(stdout io.Writer, action string, task *taskpb.TaskDetail) {
	fmt.Fprintf(stdout, "%s %s.\n", action, taskDisplayID(task))
}

func taskDisplayID(task *taskpb.TaskDetail) string {
	return taskSummaryDisplayID(task.Summary)
}

func taskSummaryDisplayID(summary *taskpb.TaskSummary) string {
	return summary.ShortId
}
