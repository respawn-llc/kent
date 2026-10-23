package serverapi

import (
	"core/shared/clientui"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
)

var (
	ErrPendingWorkCapacity   = runtimeinput.ErrPendingWorkCapacity
	ErrPendingWorkNotPending = runtimeinput.ErrPendingWorkNotPending
)

type PendingWorkCapacityError struct{}

func (*PendingWorkCapacityError) Error() string { return ErrPendingWorkCapacity.Error() }
func (*PendingWorkCapacityError) Unwrap() error { return ErrPendingWorkCapacity }

type PendingWorkNotPendingError struct {
	ItemID runtimeids.QueueItemID
}

func (e *PendingWorkNotPendingError) Error() string {
	return (&runtimeinput.PendingWorkRemovalError{ItemID: e.ItemID}).Error()
}
func (*PendingWorkNotPendingError) Unwrap() error { return ErrPendingWorkNotPending }

func PendingWorkItemIDFromWorktreeOperation(id clientui.WorktreeTransitionID) (runtimeids.QueueItemID, error) {
	return runtimeids.QueueItemIDFromProto(id.String())
}
