package clientui

import "testing"

func TestWorktreeTransitionIDSourceAcceptance(t *testing.T) {
	operationID := NewWorktreeTransitionID()
	if err := operationID.Validate(); err != nil {
		t.Fatalf("generated transition id rejected: %v", err)
	}
	for _, raw := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000", "11111111-1111-1111-1111-111111111111"} {
		if _, err := ParseWorktreeTransitionID(raw); err == nil {
			t.Fatalf("ParseWorktreeTransitionID(%q) succeeded", raw)
		}
	}
}
