package workflowexecution

import (
	"core/server/workflow"
	"core/shared/runtimeids"
)

type InterruptSelector struct {
	TaskID    workflow.TaskID
	SessionID *runtimeids.SessionID
}
