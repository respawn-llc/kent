package main

import (
	"core/shared/protoapi"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type taskStatusJSON struct {
	Kind           string   `json:"kind"`
	NativeState    string   `json:"native_state"`
	NodeIDs        []string `json:"node_ids,omitempty"`
	AttentionTypes []string `json:"attention_types,omitempty"`
}

func taskStatusOutput(status *taskpb.TaskStatus) taskStatusJSON {
	kind := protoapi.TaskStatusKind.FromProto(status.Kind)
	native := protoapi.TaskNativeStateName.FromProto(status.NativeState)
	out := taskStatusJSON{Kind: kind, NativeState: native, NodeIDs: status.NodeIds}
	for _, attention := range status.AttentionTypes {
		name := protoapi.TaskAttentionKind.FromProto(attention)
		out.AttentionTypes = append(out.AttentionTypes, name)
	}
	return out
}

type taskDependencyProgressJSON struct {
	SatisfiedCount int32 `json:"satisfied_count"`
	TotalCount     int32 `json:"total_count"`
}

type taskListItemJSON struct {
	TaskID             string                      `json:"task_id"`
	ShortID            string                      `json:"short_id"`
	WorkflowID         string                      `json:"workflow_id"`
	WorkflowName       *string                     `json:"workflow_name,omitempty"`
	Title              string                      `json:"title"`
	CreatedAtUnixMs    int64                       `json:"created_at_unix_ms"`
	UpdatedAtUnixMs    int64                       `json:"updated_at_unix_ms"`
	ColumnKeys         *[]string                   `json:"column_keys,omitempty"`
	Status             taskStatusJSON              `json:"status"`
	Labels             []*workflowpb.ProjectLabel  `json:"labels"`
	DependencyProgress *taskDependencyProgressJSON `json:"dependency_progress,omitempty"`
}

type taskListJSON struct {
	Scope                       *taskpb.ListScope  `json:"scope"`
	MatchingWorkflowCardinality string             `json:"matching_workflow_cardinality"`
	NextOffset                  *int32             `json:"next_offset,omitempty"`
	GeneratedAtUnixMs           int64              `json:"generated_at_unix_ms"`
	Tasks                       []taskListItemJSON `json:"tasks"`
}

func taskListOutput(response *taskpb.ListSuccess) taskListJSON {
	cardinality := protoapi.TaskCardinality.FromProto(response.MatchingWorkflowCardinality)
	out := taskListJSON{
		Scope: response.Scope, MatchingWorkflowCardinality: cardinality, NextOffset: response.NextOffset,
		GeneratedAtUnixMs: response.GeneratedAt.AsTime().UnixMilli(),
		Tasks:             make([]taskListItemJSON, 0, len(response.Tasks)),
	}
	for _, task := range response.Tasks {
		status := taskStatusOutput(task.Status)
		item := taskListItemJSON{
			TaskID: task.TaskId, ShortID: task.ShortId, WorkflowID: task.WorkflowId, WorkflowName: task.WorkflowName,
			Title: task.Title, CreatedAtUnixMs: task.CreatedAt.AsTime().UnixMilli(), UpdatedAtUnixMs: task.UpdatedAt.AsTime().UnixMilli(),
			Status: status, Labels: append([]*workflowpb.ProjectLabel{}, task.Labels...),
		}
		if task.ColumnKeys != nil {
			values := append([]string{}, task.ColumnKeys.Values...)
			item.ColumnKeys = &values
		}
		if progress := task.DependencyProgress; progress != nil {
			item.DependencyProgress = &taskDependencyProgressJSON{SatisfiedCount: progress.SatisfiedCount, TotalCount: progress.TotalCount}
		}
		out.Tasks = append(out.Tasks, item)
	}
	return out
}
