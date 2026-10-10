package runtimeids_test

import (
	"testing"

	"core/shared/runtimeids"
)

func TestSessionSelectorsNormalizeWithoutChangingIdentity(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"1", "1"},
		{"000123", "123"},
		{"9007199254740991", "9007199254740991"},
		{"7fd3bc93-f11c-4814-87d0-b60f10e6dd5c", "7fd3bc93-f11c-4814-87d0-b60f10e6dd5c"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			selector, err := runtimeids.ParseSessionSelector(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if selector.String() != tc.want {
				t.Fatalf("selector = %s, want %s", selector, tc.want)
			}
			if id, ok := selector.(runtimeids.SessionID); ok && !id.IsCanonicalUUIDv4() {
				t.Fatal("selector became a noncanonical Session identity")
			}
		})
	}
}

func TestSessionSelectorsRejectInvalidInput(t *testing.T) {
	for _, input := range []string{
		"", " ", " 1", "1 ", "1\n", "+1", "-1", "0", "000",
		"1.0", "1e2", "１２", "1_000", "9007199254740992",
		"9223372036854775808", "session-1",
		"7FD3BC93-F11C-4814-87D0-B60F10E6DD5C",
	} {
		t.Run(input, func(t *testing.T) {
			if value, err := runtimeids.ParseSessionSelector(input); err == nil {
				t.Fatalf("accepted %q as %v", input, value)
			}
		})
	}
}

func TestSessionIdentityPresentsAssignedNumberOrRecordedIdentity(t *testing.T) {
	id := runtimeids.NewSessionID()
	shortID, err := runtimeids.NewSessionShortID(123)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		shortID *runtimeids.SessionShortID
		want    string
	}{{&shortID, "123"}, {nil, id.String()}} {
		identity := runtimeids.SessionIdentity{ID: id, ShortID: tc.shortID}
		if identity.String() != tc.want || identity.ID != id {
			t.Fatalf("presentation = %s, identity = %s", identity, identity.ID)
		}
	}
}
