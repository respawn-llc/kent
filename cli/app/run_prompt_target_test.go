package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"builder/server/session"
	"builder/shared/config"
)

func TestValidateRunPromptAgentRoleBlocksNonCallableRoleForBuilderSession(t *testing.T) {
	settings := config.Settings{Subagents: map[string]config.SubagentRole{
		"worker": {AgentCallable: false, AgentCallableSet: true, Sources: map[string]string{"model": "file"}},
	}}

	err := validateRunPromptAgentRole(settings, "worker", true, "")
	if err == nil {
		t.Fatal("expected non-callable role to fail for Builder session")
	}
	if err.Error() != nonCallableSubagentRoleMessage {
		t.Fatalf("error = %q, want non-callable message", err.Error())
	}
	if err := validateRunPromptAgentRole(settings, "worker", false, ""); err != nil {
		t.Fatalf("human/no-session role validation failed: %v", err)
	}
}

func TestValidateRunPromptAgentRoleUnknownRoleListsCallableRolesForBuilderSession(t *testing.T) {
	settings := config.Settings{
		Model:         "gpt-5.5",
		ThinkingLevel: "medium",
		Subagents: map[string]config.SubagentRole{
			"callable":    {Settings: config.Settings{Model: "gpt-5.4-mini"}, Sources: map[string]string{"model": "file"}},
			"noncallable": {Settings: config.Settings{Model: "gpt-5.4-mini"}, Sources: map[string]string{"model": "file"}, AgentCallable: false, AgentCallableSet: true},
		},
	}

	err := validateRunPromptAgentRole(settings, "missing", true, "")
	if err == nil {
		t.Fatal("expected unknown role to fail")
	}
	text := err.Error()
	for _, want := range []string{`Unrecognized role "missing"`, "Available roles: [fast, callable]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("error = %q, want %q", text, want)
		}
	}
	if strings.Contains(text, "noncallable") {
		t.Fatalf("builder-session available list should omit non-callable role: %q", text)
	}
}

func TestStartRunPromptClientUnknownRoleBuilderSessionErrorUsesCallableAvailableRoles(t *testing.T) {
	home := newAppTestHome(t)
	workspace := t.TempDir()
	configPath := filepath.Join(home, ".builder", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	contents := strings.Join([]string{
		"[subagents.worker]",
		"model = \"gpt-5.4-mini\"",
		"",
		"[subagents.blocked]",
		"model = \"gpt-5.4-mini\"",
		"agent_callable = false",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := startRunPromptClient(context.Background(), Options{
		WorkspaceRoot:             workspace,
		WorkspaceRootExplicit:     true,
		WorkspaceContextSessionID: "session-from-env",
		AgentRole:                 "missing",
	})
	if err == nil {
		t.Fatal("expected unknown role error")
	}
	want := `Unrecognized role "missing". It may have been removed by the user during the session. Available roles: [fast, worker]`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestStartRunPromptClientDefaultAliasBlocksNonCallableContextRole(t *testing.T) {
	home := newAppTestHome(t)
	workspace := t.TempDir()
	configPath := filepath.Join(home, ".builder", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	contents := strings.Join([]string{
		"[subagents.blocked]",
		"model = \"gpt-5.4-mini\"",
		"agent_callable = false",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	registerAppWorkspace(t, cfg.WorkspaceRoot)
	parent := createAuthoritativeAppSession(t, cfg.PersistenceRoot, cfg.WorkspaceRoot)
	if err := parent.SetContinuationContext(session.ContinuationContext{AgentRole: "blocked"}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}

	_, _, err := startRunPromptClient(context.Background(), Options{
		WorkspaceRoot:             cfg.WorkspaceRoot,
		WorkspaceRootExplicit:     true,
		WorkspaceContextSessionID: parent.Meta().SessionID,
		AgentRole:                 "default",
	})
	if err == nil {
		t.Fatal("expected default alias to fail from non-callable context role")
	}
	if err.Error() != nonCallableSubagentRoleMessage {
		t.Fatalf("error = %q, want non-callable message", err.Error())
	}
}

func TestValidateRunPromptAgentRoleAliasesDefaultSelectors(t *testing.T) {
	settings := config.Settings{Subagents: map[string]config.SubagentRole{}}
	for _, alias := range []string{"default", "none", "self"} {
		t.Run(alias, func(t *testing.T) {
			if err := validateRunPromptAgentRole(settings, alias, true, ""); err != nil {
				t.Fatalf("validateRunPromptAgentRole(%q): %v", alias, err)
			}
		})
	}
}

func TestValidateRunPromptAgentRoleBlocksDefaultAliasFromNonCallableContextRole(t *testing.T) {
	settings := config.Settings{Subagents: map[string]config.SubagentRole{
		"blocked": {AgentCallable: false, AgentCallableSet: true, Sources: map[string]string{"model": "file"}},
	}}
	for _, rawRole := range []string{"", "default", "none", "self"} {
		t.Run(rawRole, func(t *testing.T) {
			err := validateRunPromptAgentRole(settings, rawRole, true, "blocked")
			if err == nil {
				t.Fatal("expected non-callable context role to block default invocation")
			}
			if err.Error() != nonCallableSubagentRoleMessage {
				t.Fatalf("error = %q, want non-callable message", err.Error())
			}
		})
	}
	if err := validateRunPromptAgentRole(settings, "default", false, "blocked"); err != nil {
		t.Fatalf("human/no-session default alias should not enforce context role: %v", err)
	}
}
