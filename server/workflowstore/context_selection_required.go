package workflowstore

import (
	"context"
	"fmt"

	"core/server/metadata/sqlitegen"
	"core/server/workflow"
)

// TaskContextSelectionRequiredError requires an explicit whole-Task Move before
// execution can continue. Historical Session bindings are not safe to infer.
type TaskContextSelectionRequiredError struct {
	TaskID workflow.TaskID
}

func (e *TaskContextSelectionRequiredError) Error() string {
	return fmt.Sprintf("task %s requires explicit context selection through Move", e.TaskID)
}

func validateTaskContextSelection(taskID workflow.TaskID, current []workflow.CurrentNode) error {
	for _, node := range current {
		if node.Scheduling != nil && node.Scheduling.Interruption != nil &&
			node.Scheduling.Interruption.Reason == workflow.CurrentNodeInterruptionReasonContextSelectionRequired {
			return &TaskContextSelectionRequiredError{TaskID: taskID}
		}
	}
	return nil
}

func (s *Store) requireTaskContextSelectionResolved(ctx context.Context, q *sqlitegen.Queries, taskID workflow.TaskID) error {
	current, err := s.listTaskCurrentNodes(ctx, q, taskID)
	if err != nil {
		return err
	}
	return validateTaskContextSelection(taskID, current)
}
