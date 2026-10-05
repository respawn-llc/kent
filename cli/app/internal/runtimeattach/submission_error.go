package runtimeattach

import (
	"context"
	"errors"
	"strings"

	"core/shared/llmerrors"
)

var ErrSubmissionInterrupted = errors.New("interrupted")

func FormatSubmissionError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrSubmissionInterrupted) || errors.Is(err, context.Canceled) {
		return ""
	}
	if formatted := llmerrors.UserFacingError(err); strings.TrimSpace(formatted) != "" {
		return formatted
	}
	return err.Error()
}
