package serverapi

import (
	"core/shared/runtimeids"
)

type ChatInputAdmissionResult struct {
	QueueItemID          runtimeids.QueueItemID
	Accepted             bool
	PromptHistoryFailure error
}
