package authview

import (
	"errors"
	"testing"

	"builder/server/auth"
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
			if got := SuccessTitle(tt.method); got != tt.want {
				t.Fatalf("SuccessTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMethodPickerNotice(t *testing.T) {
	tests := []struct {
		name string
		req  MethodPickerNoticeRequest
		want Notice
	}{
		{
			name: "device code unsupported",
			req:  MethodPickerNoticeRequest{FlowErr: auth.ErrDeviceCodeUnsupported},
			want: Notice{Text: "Device-code sign-in is not enabled for this issuer. Choose another method.", Kind: NoticeError},
		},
		{
			name: "flow error",
			req:  MethodPickerNoticeRequest{FlowErr: errors.New("boom")},
			want: Notice{Text: "Sign-in failed: boom", Kind: NoticeError},
		},
		{
			name: "startup error",
			req:  MethodPickerNoticeRequest{StartupErr: errors.New("expired")},
			want: Notice{Text: "Saved sign-in needs attention: expired", Kind: NoticeError},
		},
		{
			name: "auth not configured startup error ignored for env notice",
			req:  MethodPickerNoticeRequest{StartupErr: auth.ErrAuthNotConfigured, HasEnvAPIKey: true},
			want: Notice{Text: "Choose how Kent should sign in. OPENAI_API_KEY is available for this launch.", Kind: NoticeNeutral},
		},
		{
			name: "gate reason",
			req:  MethodPickerNoticeRequest{GateReason: "refresh failed"},
			want: Notice{Text: "Saved sign-in needs attention: refresh failed", Kind: NoticeError},
		},
		{
			name: "default",
			req:  MethodPickerNoticeRequest{},
			want: Notice{Text: "Choose how to authenticate.", Kind: NoticeNeutral},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MethodPickerNotice(tt.req); got != tt.want {
				t.Fatalf("MethodPickerNotice = %#v, want %#v", got, tt.want)
			}
		})
	}
}
