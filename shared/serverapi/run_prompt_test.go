package serverapi

import (
	"encoding/json"
	"testing"
)

func TestRunPromptOverridesAgentRoleSetJSONRoundTrip(t *testing.T) {
	req := SessionPlanRequest{
		ClientRequestID: "req-1",
		Mode:            SessionLaunchModeHeadless,
		ForceNewSession: true,
		Overrides: RunPromptOverrides{
			AgentRoleSet: true,
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got SessionPlanRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.Overrides.AgentRoleSet {
		t.Fatalf("AgentRoleSet = false after round trip: %s", data)
	}
	if !got.Overrides.HasAny() {
		t.Fatal("default-role override presence should count as an override")
	}
}

func TestRunPromptRequestParentSessionIDJSONRoundTrip(t *testing.T) {
	req := RunPromptRequest{
		ClientRequestID:   "req-1",
		ParentSessionID:   "parent-session",
		SelectedSessionID: "selected-session",
		Prompt:            "hello",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got RunPromptRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ParentSessionID != "parent-session" {
		t.Fatalf("ParentSessionID = %q, want parent-session", got.ParentSessionID)
	}
}

func TestRunPromptOverridesAgentRoleCompatJSON(t *testing.T) {
	var got SessionPlanRequest
	if err := json.Unmarshal([]byte(`{"ClientRequestID":"req-1","Mode":"headless","ForceNewSession":true,"Overrides":{"AgentRole":"worker"}}`), &got); err != nil {
		t.Fatalf("Unmarshal legacy request: %v", err)
	}
	if got.Overrides.AgentRole != "worker" {
		t.Fatalf("AgentRole = %q, want worker", got.Overrides.AgentRole)
	}
	if got.Overrides.AgentRoleSet {
		t.Fatal("legacy JSON without AgentRoleSet should leave AgentRoleSet false")
	}
	if !got.Overrides.HasAny() {
		t.Fatal("legacy AgentRole should still count as an override")
	}
}
