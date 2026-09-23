package serverapi

import (
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

type ConnectionFailure struct {
	Details *authpb.ConnectionFailureDetails
}

func (e *ConnectionFailure) Error() string {
	return "connection operation: " + e.Details.Reason.String()
}

func NewConnectionFailure(reason authpb.ConnectionFailureReason, id *config.ConnectionID) *ConnectionFailure {
	details := &authpb.ConnectionFailureDetails{Reason: reason}
	if id != nil {
		value := string(*id)
		details.ConnectionId = &value
	}
	return &ConnectionFailure{Details: details}
}
