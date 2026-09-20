package auth

import (
	"errors"
	"testing"
)

func TestOAuthCredentialValidation(t *testing.T) {
	credential := OAuthMethod{AccessToken: "access-only"}
	if err := credential.Validate(); err != nil {
		t.Fatalf("access-only credential: %v", err)
	}
	header, err := credential.AuthHeaderValue()
	if err != nil || header != "Bearer access-only" {
		t.Fatalf("credential header = %q, %v", header, err)
	}
	for _, token := range []string{"", " \t\n"} {
		invalid := OAuthMethod{AccessToken: token}
		if err := invalid.Validate(); !errors.Is(err, ErrInvalidAuthMethod) {
			t.Fatalf("invalid token accepted: %v", err)
		}
		if _, err := invalid.AuthHeaderValue(); !errors.Is(err, ErrInvalidAuthMethod) {
			t.Fatalf("invalid credential produced a header: %v", err)
		}
	}
}
