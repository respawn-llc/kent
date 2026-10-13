package main

import (
	"testing"

	"core/shared/sessionenv"
)

func TestSessionRetargetRuntimeOriginCarriesInvokingSession(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "33333333-3333-4333-8333-333333333333")
	t.Setenv(sessionenv.RunIDEnv, "11111111-1111-4111-8111-111111111111")
	t.Setenv(sessionenv.StepIDEnv, "22222222-2222-4222-8222-222222222222")

	origin, err := sessionRetargetRuntimeOrigin()
	if err != nil {
		t.Fatalf("sessionRetargetRuntimeOrigin matching Session: %v", err)
	}
	if origin == nil {
		t.Fatal("matching Session omitted runtime origin")
	}

	origin, err = sessionRetargetRuntimeOrigin()
	if err != nil {
		t.Fatalf("sessionRetargetRuntimeOrigin other Session: %v", err)
	}
	if origin == nil || origin.SessionId != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("other Session origin = %+v, want invoking identity", origin)
	}
}
