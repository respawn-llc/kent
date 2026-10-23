package serverapi

import (
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

type ConnectionRemovalFailureStage uint8

const (
	ConnectionRemovalCredentialClearFailure ConnectionRemovalFailureStage = iota + 1
	ConnectionRemovalDefinitionRemoveFailure
)

type ConnectionFailure struct {
	Details *authpb.ConnectionFailureDetails
}

func (e *ConnectionFailure) Error() string {
	message := "connection operation: " + e.Details.Reason.String()
	if e.Details.Diagnostic != nil {
		message += ": " + *e.Details.Diagnostic
	}
	return message
}

func NewConnectionFailure(reason authpb.ConnectionFailureReason, id *config.ConnectionID) *ConnectionFailure {
	details := &authpb.ConnectionFailureDetails{Reason: reason}
	if id != nil {
		value := string(*id)
		details.ConnectionId = &value
	}
	return &ConnectionFailure{Details: details}
}

func NewConnectionRemovalFailure(id config.ConnectionID, stage ConnectionRemovalFailureStage, cause error) *ConnectionFailure {
	reason := authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_UNSPECIFIED
	switch stage {
	case ConnectionRemovalCredentialClearFailure:
		reason = authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_CREDENTIAL_CLEAR_FAILED
	case ConnectionRemovalDefinitionRemoveFailure:
		reason = authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_DEFINITION_REMOVE_FAILED
	default:
		panic("invalid connection removal failure stage")
	}
	value := string(id)
	diagnostic := cause.Error()
	return &ConnectionFailure{Details: &authpb.ConnectionFailureDetails{
		Reason: reason, ConnectionId: &value, Diagnostic: &diagnostic,
	}}
}
