package protoapi

import (
	"errors"

	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/serverapi"
)

func ConnectionFailureToProto(err error) *authpb.ConnectionFailureDetails {
	var failure *serverapi.ConnectionFailure
	var duplicate *config.ConnectionAlreadyExistsError
	var notAPIKey *config.ConnectionNotAPIKeyError
	var reference *config.ConnectionReferenceError
	switch {
	case errors.As(err, &failure):
		return failure.Details
	case errors.As(err, &duplicate):
		return serverapi.NewConnectionFailure(authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_ALREADY_EXISTS, &duplicate.ID).Details
	case errors.As(err, &notAPIKey):
		return serverapi.NewConnectionFailure(authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_NOT_API_KEY, &notAPIKey.ID).Details
	case errors.As(err, &reference):
		if reference.Connection == nil {
			return serverapi.NewConnectionFailure(authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_SELECTION_REQUIRED, nil).Details
		}
		return serverapi.NewConnectionFailure(authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_NOT_FOUND, reference.Connection).Details
	default:
		return nil
	}
}
