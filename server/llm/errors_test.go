package llm

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestIsAuthenticationError(t *testing.T) {
	if !IsAuthenticationError(&ProviderAPIError{ProviderID: "openai", StatusCode: 401, Code: UnifiedErrorCodeAuthentication}) {
		t.Fatal("expected provider authentication code to be auth error")
	}
	if !IsAuthenticationError(&ProviderAPIError{ProviderID: "openai", StatusCode: 401, Code: UnifiedErrorCodeUnknown}) {
		t.Fatal("expected 401 to be auth error")
	}
	if !IsAuthenticationError(&ProviderAPIError{ProviderID: "openai", StatusCode: 403, Code: UnifiedErrorCodeUnknown}) {
		t.Fatal("expected 403 to be auth error")
	}
	if IsAuthenticationError(&ProviderAPIError{ProviderID: "openai", StatusCode: 429, Code: UnifiedErrorCodeUnknown}) {
		t.Fatal("did not expect 429 to be auth error")
	}
	if !IsAuthenticationError(&AuthError{Err: errors.New("token refresh failed")}) {
		t.Fatal("expected AuthError to be auth error")
	}
}

func TestIsNonRetriableModelError(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404} {
		if !IsNonRetriableModelError(&ProviderAPIError{ProviderID: "openai", StatusCode: status, Code: UnifiedErrorCodeUnknown}) {
			t.Fatalf("expected %d to be non-retriable", status)
		}
	}
	for _, status := range []int{408, 409, 429, 500} {
		if IsNonRetriableModelError(&ProviderAPIError{ProviderID: "openai", StatusCode: status, Code: UnifiedErrorCodeUnknown}) {
			t.Fatalf("did not expect %d to be non-retriable", status)
		}
	}
	if !IsNonRetriableModelError(&ProviderAPIError{ProviderID: "openai", StatusCode: 0, Code: UnifiedErrorCodeProviderContract, Message: "unknown provider contract"}) {
		t.Fatal("expected provider contract error to be non-retriable")
	}
	if !IsNonRetriableModelError(&ProviderAPIError{ProviderID: "openai", StatusCode: http.StatusOK, Code: UnifiedErrorCodeUnknown, ProviderType: "response.incomplete", ProviderCode: "max_output_tokens"}) {
		t.Fatal("expected response.incomplete terminal error to be non-retriable")
	}
	if !IsNonRetriableModelError(&AuthError{Err: errors.New("token refresh failed")}) {
		t.Fatal("expected AuthError to be non-retriable")
	}
	if !IsNonRetriableModelError(&ProviderSelectionError{Model: "my-model", Err: ErrUnsupportedProvider}) {
		t.Fatal("expected provider selection error to be non-retriable")
	}
}

func TestIsContextLengthOverflowError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "provider unified overflow code",
			err:  &ProviderAPIError{ProviderID: "openai", StatusCode: 400, Code: UnifiedErrorCodeContextLengthOverflow, ProviderCode: "context_length_exceeded"},
			want: true,
		},
		{
			name: "413 overflow code",
			err:  &ProviderAPIError{ProviderID: "openai", StatusCode: 413, Code: UnifiedErrorCodeContextLengthOverflow, ProviderCode: "context_length_exceeded"},
			want: true,
		},
		{
			name: "wrapped overflow error",
			err:  fmt.Errorf("compact failed: %w", &ProviderAPIError{ProviderID: "openai", StatusCode: 422, Code: UnifiedErrorCodeContextLengthOverflow, ProviderCode: "input_too_long"}),
			want: true,
		},
		{
			name: "provider unknown code",
			err:  &ProviderAPIError{ProviderID: "openai", StatusCode: 400, Code: UnifiedErrorCodeUnknown, ProviderCode: "invalid_tool_arguments"},
			want: false,
		},
		{
			name: "raw body does not override structured classification",
			err: &ProviderAPIError{
				ProviderID: "openai",
				StatusCode: 400,
				Code:       UnifiedErrorCodeUnknown,
				Raw:        `{"error":{"code":"context_length_exceeded"}}`,
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsContextLengthOverflowError(tc.err); got != tc.want {
				t.Fatalf("IsContextLengthOverflowError() = %v, want %v", got, tc.want)
			}
		})
	}
}
