package main

import (
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type taskSessionsJSON struct {
	TaskID     string                `json:"task_id"`
	Items      []taskSessionItemJSON `json:"items"`
	NextOffset *int32                `json:"next_offset,omitempty"`
}

type taskSessionItemJSON struct {
	SessionID   string  `json:"session_id"`
	SessionName *string `json:"session_name,omitempty"`
	NodeName    *string `json:"node_name,omitempty"`
	AgentRole   string  `json:"agent_role"`
	Status      string  `json:"status"`
}

func taskSessionsOutput(value *taskpb.SessionListSuccess) (taskSessionsJSON, error) {
	out := taskSessionsJSON{TaskID: value.TaskId, NextOffset: value.NextOffset, Items: make([]taskSessionItemJSON, 0, len(value.Items))}
	for _, item := range value.Items {
		status, err := protoapi.TaskSessionStatus.Decode(item.Status)
		if err != nil {
			return taskSessionsJSON{}, err
		}
		out.Items = append(out.Items, taskSessionItemJSON{
			SessionID: item.SessionId, SessionName: item.SessionName, NodeName: item.NodeName,
			AgentRole: item.AgentRole, Status: status,
		})
	}
	return out, nil
}
