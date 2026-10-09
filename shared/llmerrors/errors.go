package llmerrors

import (
	"errors"
	"fmt"
	"strings"

	"core/shared/auth"
	"core/shared/config"
)

var ErrModelStreamStalled = errors.New("model stream stalled")

type UnifiedErrorCode string

const (
	UnifiedErrorCodeUnknown               UnifiedErrorCode = "unknown"
	UnifiedErrorCodeAuthentication        UnifiedErrorCode = "authentication"
	UnifiedErrorCodeEntitlement           UnifiedErrorCode = "entitlement"
	UnifiedErrorCodeProtocolVersion       UnifiedErrorCode = "protocol_version"
	UnifiedErrorCodeContextLengthOverflow UnifiedErrorCode = "context_length_overflow"
	UnifiedErrorCodeProviderContract      UnifiedErrorCode = "provider_contract_error"
	UnifiedErrorCodeProviderOverload      UnifiedErrorCode = "provider_overload"
)

const providerTypeResponseIncomplete = "response.incomplete"

const (
	providerAuthorizationDiagnosticPrefix = " Provider authorization diagnostic: "
	providerRequestIDPrefix               = " Provider request ID: "
)

type ProviderAPIError struct {
	ProviderID              string
	ConnectionID            *config.ConnectionID
	StatusCode              int
	Code                    UnifiedErrorCode
	ProviderCode            string
	ProviderType            string
	ProviderParam           string
	ProviderRequestID       *string
	AuthorizationDiagnostic *string
	Message                 string
	Raw                     string
	Err                     error
}

func NewProviderContractError(providerID string, statusCode int, cause error) *ProviderAPIError {
	message := "provider contract error"
	if cause != nil {
		message = cause.Error()
	}
	return &ProviderAPIError{
		ProviderID: providerID,
		StatusCode: statusCode,
		Code:       UnifiedErrorCodeProviderContract,
		Message:    message,
		Raw:        message,
		Err:        cause,
	}
}

func (e *ProviderAPIError) Error() string {
	if e == nil {
		return "provider api error"
	}
	identity := e.ProviderID
	if e.ConnectionID != nil {
		identity = fmt.Sprintf("connection %s (%s)", *e.ConnectionID, identity)
	}
	return fmt.Sprintf("%s status %d [%s/%s]: %s", identity, e.StatusCode, e.Code, e.ProviderCode, e.Message)
}

func (e *ProviderAPIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type AuthError struct {
	ConnectionID *config.ConnectionID
	Err          error
}

type ProviderSelectionError struct {
	Model string
	Err   error
}

func (e *ProviderSelectionError) Error() string {
	if e == nil {
		return "provider selection error"
	}
	model := strings.TrimSpace(e.Model)
	if model == "" {
		return "could not resolve provider access; select a named connection with the connection setting"
	}
	return fmt.Sprintf("could not resolve provider access for model %q; select a named connection with the connection setting", model)
}

func (e *ProviderSelectionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *AuthError) Error() string {
	if e == nil || e.Err == nil {
		return "authentication error"
	}
	return e.Err.Error()
}

func (e *AuthError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func IsAuthenticationError(err error) bool {
	if err == nil {
		return false
	}
	var authErr *AuthError
	if errors.As(err, &authErr) {
		return true
	}
	var providerErr *ProviderAPIError
	if errors.As(err, &providerErr) {
		if providerErr.Code == UnifiedErrorCodeEntitlement || providerErr.Code == UnifiedErrorCodeProtocolVersion {
			return false
		}
		if providerErr.Code == UnifiedErrorCodeAuthentication {
			return true
		}
		switch providerErr.StatusCode {
		case 401, 403:
			return true
		}
	}
	return false
}

func IsNonRetriableModelError(err error) bool {
	if err == nil {
		return false
	}
	var providerSelectionErr *ProviderSelectionError
	if errors.As(err, &providerSelectionErr) {
		return true
	}
	if IsAuthenticationError(err) {
		return true
	}
	var providerErr *ProviderAPIError
	if errors.As(err, &providerErr) {
		if providerErr.ProviderType == providerTypeResponseIncomplete {
			return true
		}
		if providerErr.Code == UnifiedErrorCodeProviderContract || providerErr.Code == UnifiedErrorCodeEntitlement || providerErr.Code == UnifiedErrorCodeProtocolVersion {
			return true
		}
		switch providerErr.StatusCode {
		case 400, 401, 403, 404:
			return true
		}
	}
	return false
}

func IsContextLengthOverflowError(err error) bool {
	if err == nil {
		return false
	}
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) {
		return false
	}
	return providerErr.Code == UnifiedErrorCodeContextLengthOverflow
}

// HasHTTPStatus reports whether err (or anything it wraps) carries the given
// provider HTTP status code.
func HasHTTPStatus(err error, statusCode int) bool {
	if err == nil {
		return false
	}
	var providerErr *ProviderAPIError
	if errors.As(err, &providerErr) && providerErr.StatusCode == statusCode {
		return true
	}
	return false
}

func UserFacingError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrModelStreamStalled) {
		return "The model request stalled: no streaming activity within the timeout window (timeouts.model_request_seconds). The model may be overloaded; retry, or raise the timeout."
	}
	var providerSelectionErr *ProviderSelectionError
	if errors.As(err, &providerSelectionErr) {
		return providerSelectionErr.Error()
	}
	var authErr *AuthError
	if errors.As(err, &authErr) {
		if errors.Is(authErr.Err, auth.ErrAuthNotConfigured) {
			return "Not authenticated, run /login to sign in with your provider"
		}
		if authErr.ConnectionID != nil {
			if errors.Is(authErr.Err, auth.ErrOAuthRefreshFailed) {
				detail := strings.TrimRight(authErr.Error(), ".")
				return fmt.Sprintf("Failed to authenticate the provider connection: %s.\nRun /login to authenticate connection %s, used for this session.", detail, *authErr.ConnectionID)
			}
			return authErr.Error()
		}
	}
	if errors.Is(err, auth.ErrAuthNotConfigured) {
		return "Not authenticated, run /login to sign in with your provider"
	}
	var providerErr *ProviderAPIError
	if errors.As(err, &providerErr) {
		if providerErr.Code == UnifiedErrorCodeProtocolVersion {
			return fmt.Sprintf("The provider rejected this client version. Update Kent before trying again. %s", providerErr.Error())
		}
		if IsAuthenticationError(providerErr) {
			if providerErr.ConnectionID != nil {
				return fmt.Sprintf("Failed to authenticate the provider connection: %s. Run /login to authenticate connection %s, used for this session.", strings.TrimRight(providerErr.Error(), "."), *providerErr.ConnectionID)
			}
			message := authenticationFailedWarning(providerErr.ProviderID, providerErr.StatusCode)
			if diagnostic := optionalDiagnosticValue(providerErr.AuthorizationDiagnostic); diagnostic != "" {
				message += providerAuthorizationDiagnosticPrefix + diagnostic + "."
			}
			if requestID := optionalDiagnosticValue(providerErr.ProviderRequestID); requestID != "" {
				message += providerRequestIDPrefix + requestID + "."
			}
			return message
		}
	}
	return ""
}

func optionalDiagnosticValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func authenticationFailedWarning(provider string, statusCode int) string {
	label := strings.TrimSpace(provider)
	if label == "" {
		label = "API"
	}
	if statusCode > 0 {
		return fmt.Sprintf("Authentication failed with %s (HTTP %d). Run /login for the selected connection or check its configured environment variable on the server and restart after changing the server environment.", label, statusCode)
	}
	return fmt.Sprintf("Authentication failed with %s. Run /login for the selected connection or check its configured environment variable on the server and restart after changing the server environment.", label)
}
