package authview

import (
	"errors"
	"fmt"
	"strings"

	"builder/server/auth"
)

type NoticeKind string

const (
	NoticeNeutral NoticeKind = "neutral"
	NoticeError   NoticeKind = "error"
)

type Notice struct {
	Text string
	Kind NoticeKind
}

type MethodPickerNoticeRequest struct {
	FlowErr      error
	StartupErr   error
	GateReason   string
	HasEnvAPIKey bool
}

func SuccessTitle(method auth.Method) string {
	if method.Type == auth.MethodOAuth && method.OAuth != nil {
		if email := strings.TrimSpace(method.OAuth.Email); email != "" {
			return fmt.Sprintf("Auth success for: %s", email)
		}
	}
	return "Auth success"
}

func MethodPickerNotice(req MethodPickerNoticeRequest) Notice {
	if req.FlowErr != nil {
		if errors.Is(req.FlowErr, auth.ErrDeviceCodeUnsupported) {
			return Notice{Text: "Device-code sign-in is not enabled for this issuer. Choose another method.", Kind: NoticeError}
		}
		return Notice{Text: "Sign-in failed: " + req.FlowErr.Error(), Kind: NoticeError}
	}
	if req.StartupErr != nil && !errors.Is(req.StartupErr, auth.ErrAuthNotConfigured) {
		return Notice{Text: "Saved sign-in needs attention: " + req.StartupErr.Error(), Kind: NoticeError}
	}
	gateReason := strings.TrimSpace(req.GateReason)
	if gateReason != "" && gateReason != auth.ErrAuthNotConfigured.Error() {
		return Notice{Text: "Saved sign-in needs attention: " + gateReason, Kind: NoticeError}
	}
	if req.HasEnvAPIKey {
		return Notice{Text: "Choose how Builder should sign in. OPENAI_API_KEY is available for this launch.", Kind: NoticeNeutral}
	}
	return Notice{Text: "Choose how to authenticate.", Kind: NoticeNeutral}
}
