package serverapi

import (
	"core/shared/clientui"
)

type AskListPendingBySessionRequest struct {
	SessionID string
}

type AskListPendingBySessionResponse struct {
	Asks []clientui.PendingAsk
}

func (r AskListPendingBySessionRequest) Validate() error {
	return validateRequiredSessionID(r.SessionID)
}
