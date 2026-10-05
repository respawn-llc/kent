package authui

import (
	"errors"
	"fmt"

	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/serverapi"
)

func ConnectionFailureText(err error) (string, bool) {
	var failure *serverapi.ConnectionFailure
	if !errors.As(err, &failure) {
		return "", false
	}
	details := failure.Details
	switch details.Reason {
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_ALREADY_EXISTS:
		return fmt.Sprintf("Connection %q already exists. Choose another ID.", details.GetConnectionId()), true
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_NOT_FOUND:
		return fmt.Sprintf("Connection %q is no longer defined. Choose another connection.", details.GetConnectionId()), true
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_NOT_API_KEY:
		return fmt.Sprintf("Connection %q does not use an API-key reference.", details.GetConnectionId()), true
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_NOT_SUBSCRIPTION:
		return fmt.Sprintf("Connection %q does not use subscription sign-in.", details.GetConnectionId()), true
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_SIGN_IN_REQUIRED:
		return fmt.Sprintf("Sign in before adding connection %q.", details.GetConnectionId()), true
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_SETUP_REQUIRED:
		return "Complete first-time setup before adding another connection.", true
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_SETUP_COMPLETED:
		return "First-time setup is complete. Open /login to manage connections.", true
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_FINISH_ACCEPTED:
		return "Finish has already been submitted. Wait for setup to complete.", true
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_SETUP_CHANGED:
		return "This setup was changed or discarded. Select the connection again.", true
	case authpb.ConnectionFailureReason_CONNECTION_FAILURE_REASON_SELECTION_REQUIRED:
		return "Select the first provider connection before continuing.", true
	default:
		return "", false
	}
}
