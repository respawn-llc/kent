package submissionerror

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"builder/shared/llmerrors"
	"builder/shared/serverapi"
)

var ErrInterrupted = errors.New("interrupted")

func Format(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrInterrupted) || errors.Is(err, context.Canceled) {
		return ""
	}
	if errors.Is(err, serverapi.ErrSessionAlreadyControlled) {
		return "session is controlled by another client; retry to take over"
	}
	if errors.Is(err, serverapi.ErrInvalidControllerLease) {
		return "lost control of this session; retry to reclaim it"
	}
	if formatted := llmerrors.UserFacingError(err); strings.TrimSpace(formatted) != "" {
		return formatted
	}
	var statusErr *llmerrors.APIStatusError
	if errors.As(err, &statusErr) {
		body := statusErr.Body
		if strings.TrimSpace(body) == "" {
			body = "<empty error body>"
		}
		return fmt.Sprintf("openai status %d\nresponse body:\n%s", statusErr.StatusCode, body)
	}
	return err.Error()
}
