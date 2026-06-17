package authui

import (
	"errors"
	"fmt"
	"strings"

	"core/shared/auth"
)

type AuthNoticeKind string

const (
	AuthNoticeNeutral AuthNoticeKind = "neutral"
	AuthNoticeError   AuthNoticeKind = "error"
)

type AuthNotice struct {
	Text string
	Kind AuthNoticeKind
}

type AuthMethodPickerNoticeRequest struct {
	FlowErr      error
	StartupErr   error
	GateReason   string
	HasEnvAPIKey bool
}

func AuthSuccessTitle(method auth.Method) string {
	if method.Type == auth.MethodOAuth && method.OAuth != nil {
		if email := strings.TrimSpace(method.OAuth.Email); email != "" {
			return fmt.Sprintf("Auth success for: %s", email)
		}
	}
	return "Auth success"
}

func AuthMethodPickerNotice(req AuthMethodPickerNoticeRequest) AuthNotice {
	if req.FlowErr != nil {
		if errors.Is(req.FlowErr, auth.ErrDeviceCodeUnsupported) {
			return AuthNotice{Text: "Device-code sign-in is not enabled for this issuer. Choose another method.", Kind: AuthNoticeError}
		}
		return AuthNotice{Text: "Sign-in failed: " + req.FlowErr.Error(), Kind: AuthNoticeError}
	}
	if req.StartupErr != nil && !errors.Is(req.StartupErr, auth.ErrAuthNotConfigured) {
		return AuthNotice{Text: "Saved sign-in needs attention: " + req.StartupErr.Error(), Kind: AuthNoticeError}
	}
	gateReason := strings.TrimSpace(req.GateReason)
	if gateReason != "" && gateReason != auth.ErrAuthNotConfigured.Error() {
		return AuthNotice{Text: "Saved sign-in needs attention: " + gateReason, Kind: AuthNoticeError}
	}
	if req.HasEnvAPIKey {
		return AuthNotice{Text: "Choose how Kent should sign in. OPENAI_API_KEY is available for this launch.", Kind: AuthNoticeNeutral}
	}
	return AuthNotice{Text: "Choose how to authenticate.", Kind: AuthNoticeNeutral}
}
