package serverapi

import (
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
)

type ChatInputAdmissionResult struct {
	QueueItemID          runtimeids.QueueItemID
	Disposition          runtimeinput.InputAdmissionDisposition
	Accepted             bool
	PromptHistoryFailure error
}
