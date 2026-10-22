package main

import "testing"

func TestSessionFlagsNormalizeSelectors(t *testing.T) {
	for _, tc := range []struct {
		flags commonFlags
		want  string
		valid bool
	}{
		{commonFlags{SessionID: "00123", ContinueID: "123"}, "123", true},
		{commonFlags{ContinueID: "7fd3bc93-f11c-4814-87d0-b60f10e6dd5c"}, "7fd3bc93-f11c-4814-87d0-b60f10e6dd5c", true},
		{commonFlags{SessionID: " 123"}, "", false},
		{commonFlags{ContinueID: "+123"}, "", false},
		{commonFlags{SessionID: "0"}, "", false},
	} {
		got, err := effectiveSessionID(tc.flags)
		if (err == nil) != tc.valid || (tc.valid && got != tc.want) {
			t.Fatalf("flags %+v = %q, %v", tc.flags, got, err)
		}
	}
}
