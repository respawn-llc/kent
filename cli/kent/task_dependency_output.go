package main

import (
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type taskDependencyMutationJSON struct {
	Outcome        string `json:"outcome"`
	BlockerTaskID  string `json:"blocker_task_id"`
	BlockerShortID string `json:"blocker_short_id"`
	BlockedTaskID  string `json:"blocked_task_id"`
	BlockedShortID string `json:"blocked_short_id"`
}

func taskDependencyMutationOutput(value *taskpb.DependencyMutationSuccess) (taskDependencyMutationJSON, error) {
	outcome, err := protoapi.TaskDependencyMutationOutcome.Decode(value.Outcome)
	return taskDependencyMutationJSON{
		Outcome: outcome, BlockerTaskID: value.BlockerTaskId, BlockerShortID: value.BlockerShortId,
		BlockedTaskID: value.BlockedTaskId, BlockedShortID: value.BlockedShortId,
	}, err
}

type taskDependencyListJSON struct {
	TaskID     string                        `json:"task_id"`
	ShortID    string                        `json:"short_id"`
	Directions []taskDependencyDirectionJSON `json:"directions"`
}

type taskDependencyDirectionJSON struct {
	Direction        string                   `json:"direction"`
	TotalCount       int32                    `json:"total_count"`
	UnsatisfiedCount *int32                   `json:"unsatisfied_count,omitempty"`
	Items            []taskDependencyItemJSON `json:"items"`
}

type taskDependencyItemJSON struct {
	TaskID       string         `json:"task_id"`
	ShortID      string         `json:"short_id"`
	Title        string         `json:"title"`
	WorkflowID   string         `json:"workflow_id"`
	Status       taskStatusJSON `json:"status"`
	Satisfaction *string        `json:"satisfaction,omitempty"`
}

func taskDependencyListOutput(value *taskpb.DependencyListSuccess) (taskDependencyListJSON, error) {
	out := taskDependencyListJSON{
		TaskID: value.TaskId, ShortID: value.ShortId,
		Directions: make([]taskDependencyDirectionJSON, 0, len(value.Directions)),
	}
	for _, direction := range value.Directions {
		name, err := protoapi.TaskDependencyDirection.Decode(direction.Direction)
		if err != nil {
			return taskDependencyListJSON{}, err
		}
		projected := taskDependencyDirectionJSON{
			Direction: name, TotalCount: direction.TotalCount, UnsatisfiedCount: direction.UnsatisfiedCount,
			Items: make([]taskDependencyItemJSON, 0, len(direction.Items)),
		}
		for _, item := range direction.Items {
			status, err := taskStatusOutput(item.Status)
			if err != nil {
				return taskDependencyListJSON{}, err
			}
			row := taskDependencyItemJSON{
				TaskID: item.TaskId, ShortID: item.ShortId, Title: item.Title, WorkflowID: item.WorkflowId, Status: status,
			}
			if item.Satisfaction != nil {
				satisfaction, err := protoapi.TaskDependencySatisfaction.Decode(*item.Satisfaction)
				if err != nil {
					return taskDependencyListJSON{}, err
				}
				row.Satisfaction = &satisfaction
			}
			projected.Items = append(projected.Items, row)
		}
		out.Directions = append(out.Directions, projected)
	}
	return out, nil
}
