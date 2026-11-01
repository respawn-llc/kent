package config

import "testing"

func TestCyberAccessIsScopedToSelectedRole(t *testing.T) {
	_, _, app := loadConfigTestFileApp(t, `
[subagents.cyber]
model = "gpt-6-sol"
connection = "codex"
model_context_window = 272000
context_compaction_threshold_tokens = 252000
agent_callable = false
workflow_subagent = false
[subagents.cyber.model_capabilities]
cyber_access = true
[subagents.cyber.reviewer.model_capabilities]
cyber_access = false
[connections.codex]
protocol = "chatgpt-codex"
`, LoadOptions{})
	if app.Settings.ModelCapabilities.CyberAccess {
		t.Fatal("role cyber access leaked into default Agent")
	}
	role := LookupSubagentRole(app.Settings, "cyber").Role
	effective, _, err := OverlaySubagentRoleSettings(app, role, true)
	if err != nil {
		t.Fatal(err)
	}
	if !effective.ModelCapabilities.CyberAccess || effective.Model != "gpt-6-sol" {
		t.Fatal("role cyber access was not selected")
	}
	if effective.Reviewer.ModelCapabilities.CyberAccess {
		t.Fatal("explicit reviewer opt-out was ignored")
	}
	members, err := effective.ConnectionMembers()
	if err != nil || len(members) != 1 || members[0] != "codex" {
		t.Fatalf("role connection selection = %v, %v", members, err)
	}
	if SubagentRoleCallable(role) || SubagentRoleWorkflowCallable(role) {
		t.Fatal("interactive role exposed for delegation")
	}
}
