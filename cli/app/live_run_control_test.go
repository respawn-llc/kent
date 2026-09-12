package app

import (
	"context"
	"testing"

	"core/shared/runtimeids"
)

func TestLiveSteerCallerSessionIDParsesOptionalEnvironmentContext(t *testing.T) {
	t.Setenv("KENT_SESSION_ID", "018fdd67-89ab-4cde-8123-456789abcdef")
	callerID, err := LiveSteerCallerSessionID()
	if err != nil {
		t.Fatalf("liveSteerCallerSessionID: %v", err)
	}
	if callerID == nil || *callerID != "018fdd67-89ab-4cde-8123-456789abcdef" {
		t.Fatalf("caller ID = %v, want configured Session ID", callerID)
	}
}

func TestLiveSteerCallerSessionIDRejectsMalformedPresentContext(t *testing.T) {
	t.Setenv("KENT_SESSION_ID", "not-a-uuid")
	if _, err := LiveSteerCallerSessionID(); err == nil {
		t.Fatal("liveSteerCallerSessionID unexpectedly accepted malformed context")
	}
}

func TestLiveSteerCallerSessionIDRejectsLegacyPresentContext(t *testing.T) {
	t.Setenv("KENT_SESSION_ID", "legacy_session.2024")
	if _, err := LiveSteerCallerSessionID(); err == nil {
		t.Fatal("liveSteerCallerSessionID unexpectedly accepted legacy context")
	}
}

func TestLiveSteerCallerSessionIDOmittedForBlankContext(t *testing.T) {
	t.Setenv("KENT_SESSION_ID", " \t ")
	callerID, err := LiveSteerCallerSessionID()
	if err != nil {
		t.Fatalf("liveSteerCallerSessionID: %v", err)
	}
	if callerID != nil {
		t.Fatalf("caller ID = %v, want nil", callerID)
	}
}

func TestRunLiveSteerRejectsMalformedPresentContextBeforeConnecting(t *testing.T) {
	t.Setenv("KENT_SESSION_ID", "not-a-uuid")
	if _, err := RunLiveSteer(context.Background(), Options{}, runtimeids.NewSessionID(), "probe"); err == nil {
		t.Fatal("RunLiveSteer unexpectedly connected with malformed invoking context")
	}
}
