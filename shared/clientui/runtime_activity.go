package clientui

import (
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
)

type RuntimeSubmitRequest struct {
	Input       runtimeinput.Input
	Intent      runtimeinput.PendingWorkLane
	QueueItemID runtimeids.QueueItemID
}

type RuntimeCompactRequest struct {
	Admission runtimeinput.ManualCompactionAdmission
}

type RuntimeCompactResult struct {
	Diagnostic error
}
