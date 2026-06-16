package actions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestUnknownActionIsFatal(t *testing.T) {
	r := NewRegistry()
	err := r.Execute(context.Background(), "missing", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected error for unknown action")
	}
	if !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("expected ErrUnknownAction, got %v", err)
	}
}
