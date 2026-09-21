package serverapi

import "testing"

func runPromptStringPtr(value string) *string { return &value }

func TestRunPromptOverridesAgentRoleContract(t *testing.T) {
	got := RunPromptRequest{
		Intent:    CreateNewSessionLaunchIntent(IndependentSessionCreateOrigin()),
		Prompt:    "hello",
		Overrides: RunPromptOverrides{AgentRole: runPromptStringPtr("worker")},
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate request: %v", err)
	}
	if !got.Overrides.HasAny() {
		t.Fatal("AgentRole should count as an override")
	}
	if !got.Overrides.HasAgentRoleOverride() {
		t.Fatal("AgentRole should count as a role override")
	}
}

func TestRunPromptOverridesRolePresenceAndAuth(t *testing.T) {
	tests := []struct {
		name         string
		overrides    RunPromptOverrides
		wantAny      bool
		wantRole     bool
		wantAuth     bool
		wantDefault  bool
		wantRoleName string
	}{
		{name: "empty", overrides: RunPromptOverrides{}, wantAny: false, wantRole: false},
		{name: "config only", overrides: RunPromptOverrides{Model: "gpt-5.6-sol"}, wantAny: true, wantRole: false},
		{name: "default", overrides: RunPromptOverrides{AgentRole: runPromptStringPtr("default")}, wantAny: true, wantRole: true, wantAuth: false, wantDefault: true},
		{name: "named", overrides: RunPromptOverrides{AgentRole: runPromptStringPtr(" Worker ")}, wantAny: true, wantRole: true, wantAuth: true, wantRoleName: "worker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.overrides.HasAny(); got != tt.wantAny {
				t.Fatalf("HasAny = %t, want %t", got, tt.wantAny)
			}
			if got := tt.overrides.HasAgentRoleOverride(); got != tt.wantRole {
				t.Fatalf("HasAgentRoleOverride = %t, want %t", got, tt.wantRole)
			}
			role, err := tt.overrides.AgentRoleOverride()
			if err != nil {
				t.Fatalf("AgentRoleOverride: %v", err)
			}
			if role.Default != tt.wantDefault || role.Role != tt.wantRoleName {
				t.Fatalf("AgentRoleOverride = %+v, want default=%t role=%q", role, tt.wantDefault, tt.wantRoleName)
			}
		})
	}
}

func TestRunPromptOverridesRejectReservedNonDefaultRoles(t *testing.T) {
	for _, role := range []string{"none", "self"} {
		t.Run(role, func(t *testing.T) {
			if err := (RunPromptOverrides{AgentRole: runPromptStringPtr(role)}).ValidateAgentRoleOverride(); err == nil {
				t.Fatal("expected reserved non-default role to be invalid")
			}
		})
	}
}
