package authui

import (
	"errors"
	"testing"

	"core/server/auth"
)

func TestSuccessTitle(t *testing.T) {
	tests := []struct {
		name   string
		method auth.Method
		want   string
	}{
		{name: "default", want: "Auth success"},
		{
			name: "oauth email",
			method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{
				Email: "user@example.com",
			}},
			want: "Auth success for: user@example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AuthSuccessTitle(tt.method); got != tt.want {
				t.Fatalf("AuthSuccessTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMethodPickerNotice(t *testing.T) {
	tests := []struct {
		name string
		req  AuthMethodPickerNoticeRequest
		want AuthNotice
	}{
		{
			name: "device code unsupported",
			req:  AuthMethodPickerNoticeRequest{FlowErr: auth.ErrDeviceCodeUnsupported},
			want: AuthNotice{Text: "Device-code sign-in is not enabled for this issuer. Choose another method.", Kind: AuthNoticeError},
		},
		{
			name: "flow error",
			req:  AuthMethodPickerNoticeRequest{FlowErr: errors.New("boom")},
			want: AuthNotice{Text: "Sign-in failed: boom", Kind: AuthNoticeError},
		},
		{
			name: "startup error",
			req:  AuthMethodPickerNoticeRequest{StartupErr: errors.New("expired")},
			want: AuthNotice{Text: "Saved sign-in needs attention: expired", Kind: AuthNoticeError},
		},
		{
			name: "auth not configured startup error ignored for env notice",
			req:  AuthMethodPickerNoticeRequest{StartupErr: auth.ErrAuthNotConfigured, HasEnvAPIKey: true},
			want: AuthNotice{Text: "Choose how Kent should sign in. OPENAI_API_KEY is available for this launch.", Kind: AuthNoticeNeutral},
		},
		{
			name: "gate reason",
			req:  AuthMethodPickerNoticeRequest{GateReason: "refresh failed"},
			want: AuthNotice{Text: "Saved sign-in needs attention: refresh failed", Kind: AuthNoticeError},
		},
		{
			name: "default",
			req:  AuthMethodPickerNoticeRequest{},
			want: AuthNotice{Text: "Choose how to authenticate.", Kind: AuthNoticeNeutral},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AuthMethodPickerNotice(tt.req); got != tt.want {
				t.Fatalf("AuthMethodPickerNotice = %#v, want %#v", got, tt.want)
			}
		})
	}
}
