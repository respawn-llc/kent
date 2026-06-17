package runtimeattach

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"core/server/llm"
	"core/shared/auth"
	"core/shared/llmerrors"
	"core/shared/serverapi"
)

func TestFormatSubmissionError(t *testing.T) {
	body := strings.Repeat("AUTH_ERR_", 4)
	tests := []struct {
		name     string
		err      error
		contains []string
		excludes []string
		want     string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "interrupted", err: ErrSubmissionInterrupted, want: ""},
		{name: "canceled", err: context.Canceled, want: ""},
		{name: "already controlled", err: serverapi.ErrSessionAlreadyControlled, want: "session is controlled by another client; retry to take over"},
		{name: "invalid controller lease", err: serverapi.ErrInvalidControllerLease, want: "lost control of this session; retry to reclaim it"},
		{
			name:     "embedded api status body",
			err:      fmt.Errorf("wrapped: %w", &llm.APIStatusError{StatusCode: 429, Body: body}),
			contains: []string{"openai status 429", body},
		},
		{
			name:     "remote api status body",
			err:      &llmerrors.APIStatusError{StatusCode: 429, Body: body},
			contains: []string{"openai status 429", body},
		},
		{
			name:     "empty remote api status body",
			err:      &llmerrors.APIStatusError{StatusCode: 500},
			contains: []string{"openai status 500", "<empty error body>"},
		},
		{
			name:     "auth not configured",
			err:      &llmerrors.AuthError{Err: auth.ErrAuthNotConfigured},
			want:     "Not authenticated, run /login to sign in with your provider",
			excludes: []string{"OPENAI_API_KEY", "openai_base_url"},
		},
		{
			name:     "embedded friendly provider auth",
			err:      fmt.Errorf("request failed: %w", &llm.ProviderAPIError{ProviderID: "openai-compatible", StatusCode: 401, Code: llm.UnifiedErrorCodeAuthentication}),
			contains: []string{"Authentication failed", "/login"},
			excludes: []string{"openai status 401"},
		},
		{
			name:     "remote friendly provider auth dto",
			err:      fmt.Errorf("request failed: %w", &llmerrors.ProviderAPIError{ProviderID: "openai-compatible", StatusCode: 401, Code: llmerrors.UnifiedErrorCodeAuthentication}),
			contains: []string{"Authentication failed", "/login"},
			excludes: []string{"openai status 401"},
		},
		{
			name:     "provider selection",
			err:      &llm.ProviderSelectionError{Model: "my-local-model", Err: llm.ErrUnsupportedProvider},
			contains: []string{"provider/auth path", "provider_override", "openai_base_url"},
		},
		{name: "generic", err: fmt.Errorf("boom"), want: "boom"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatSubmissionError(tc.err)
			if tc.want != "" || len(tc.contains) == 0 {
				if got != tc.want {
					t.Fatalf("FormatSubmissionError()=%q, want %q", got, tc.want)
				}
			}
			for _, needle := range tc.contains {
				if !strings.Contains(got, needle) {
					t.Fatalf("FormatSubmissionError()=%q, want substring %q", got, needle)
				}
			}
			for _, needle := range tc.excludes {
				if strings.Contains(got, needle) {
					t.Fatalf("FormatSubmissionError()=%q, did not want substring %q", got, needle)
				}
			}
		})
	}
}
