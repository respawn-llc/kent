package main

import "testing"

func TestReadTaskBodyFlagRequiresInlineOrFileBody(t *testing.T) {
	if _, err := readTaskBodyFlag(" \t\n", ""); err == nil {
		t.Fatal("expected missing body flags to fail")
	}
}
