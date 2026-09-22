package authui

import (
	"errors"

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
	FlowErr error
}

func AuthMethodPickerNotice(req AuthMethodPickerNoticeRequest) AuthNotice {
	if req.FlowErr != nil {
		if errors.Is(req.FlowErr, auth.ErrDeviceCodeUnsupported) {
			return AuthNotice{Text: "Device-code sign-in is not enabled for this issuer. Choose another method.", Kind: AuthNoticeError}
		}
		return AuthNotice{Text: "Sign-in failed: " + req.FlowErr.Error(), Kind: AuthNoticeError}
	}
	return AuthNotice{Text: "Choose how to authenticate.", Kind: AuthNoticeNeutral}
}
