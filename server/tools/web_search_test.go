package tools

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateWebSearchInputRejectsWhitespaceQuery(t *testing.T) {
	err := ValidateWebSearchInput(json.RawMessage(`{"query":"   "}`))
	if err == nil {
		t.Fatal("expected whitespace-only query to be rejected")
	}
	if !errors.Is(err, ErrInvalidWebSearchQuery) {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
