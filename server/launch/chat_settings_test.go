package launch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/session"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestSupportedChatThinkingValuesUsesKnownModelContract(t *testing.T) {
	got := supportedChatThinkingValues("gpt-5", "ultra")
	if slices.Contains(got, "ultra") {
		t.Fatalf("supported thinking values = %v, unexpectedly included configured value outside the known model contract", got)
	}
}

func TestSessionAgentChoicesPreserveOnlyKnownRoleFacts(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		authored    bool
		environment bool
	}{
		{name: "inherited"},
		{name: "role", authored: true},
		{name: "environment", environment: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root, workspace := t.TempDir(), t.TempDir()
			declarations := ""
			if scenario.authored {
				declarations = "model = \"gpt-5-mini\"\nthinking_level = \"high\""
			}
			if scenario.environment {
				t.Setenv("KENT_MODEL", "gpt-5-mini")
				t.Setenv("KENT_THINKING_LEVEL", "high")
			}
			if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(fmt.Sprintf(`
connection = "work"
model = "gpt-5"
thinking_level = "medium"
[connections.work]
protocol = "responses"
endpoint = "http://127.0.0.1:1/v1"
[subagents.worker]
connection = "missing"
%s
[subagents.fast]
connection = "missing"
%s
`, declarations, declarations)), 0o600); err != nil {
				t.Fatal(err)
			}
			app, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			meta := session.Meta{ConnectionID: textutil.Value(config.ConnectionID("work"))}
			for _, available := range []bool{false, true} {
				if available {
					app.Settings.Connections["missing"] = app.Settings.Connections["work"]
				}
				catalog, err := PrepareSessionChatAgentCatalog(app, meta)
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"worker", "fast"} {
					entry, ok := catalog.Lookup(name)
					if !ok {
						t.Fatalf("choice %s absent", name)
					}
					if available != (entry.SelectionError == nil) || available != (entry.Settings != nil) {
						t.Fatalf("choice %s availability: %+v", name, entry)
					}
					model, thinking := "gpt-5", "medium"
					if scenario.authored || scenario.environment {
						model, thinking = "gpt-5-mini", "high"
					} else if name == "fast" && !available {
						if entry.Choice.Model != nil || entry.Choice.Thinking != nil {
							t.Fatalf("unresolved built-in defaults became facts: %+v", entry.Choice)
						}
						continue
					}
					if entry.Choice.GetModel() != model || entry.Choice.GetThinking() != thinking {
						t.Fatalf("choice %s lost authoritative facts: %+v", name, entry.Choice)
					}
				}
			}
		})
	}
}

func TestSupportedChatThinkingValuesPreservesConfiguredUnknownModelValue(t *testing.T) {
	got := supportedChatThinkingValues("custom-model", "ultra")
	if !slices.Contains(got, "ultra") {
		t.Fatalf("supported thinking values = %v, want configured unknown-model value", got)
	}
}

func TestPrepareChatAgentCatalogProjectsChoicesAndOmitsEquivalentAgents(t *testing.T) {
	settings := config.DefaultOnboardingSettings()
	settings.Model = "gpt-5"
	settings.ThinkingLevel = "medium"
	settings.EnabledTools = map[toolspec.ID]bool{
		toolspec.ToolExecCommand: true,
		toolspec.ToolViewImage:   true,
	}
	settings.Subagents = map[string]config.SubagentRole{
		"equivalent": {
			Settings: config.Settings{Model: "gpt-5", ThinkingLevel: "medium"},
			Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}},
		},
		"worker": {
			Settings: config.Settings{
				Model:         "worker-model",
				ThinkingLevel: "high",
				SystemPromptFile: &config.SystemPromptFile{
					Path: "/worker.md", Scope: config.SystemPromptFileScopeSubagent,
				},
				EnabledTools: map[toolspec.ID]bool{
					toolspec.ToolExecCommand: true,
					toolspec.ToolViewImage:   true,
				},
				ModelCapabilities: config.ModelCapabilitiesOverride{
					SupportsReasoningEffort: true,
				},
			},
			Sources: map[string]config.Origin{
				"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}, "system_prompt_file": {Kind: config.SourceInput,
					Property: config.PropertyAddress{Key: "system_prompt_file"},
				},

				"model_capabilities.supports_reasoning_effort": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model_capabilities.supports_reasoning_effort"}}, "agent_callable": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "agent_callable"}},
			},

			AgentCallable: false,
		},
	}
	catalog, err := PrepareChatAgentCatalog(testsetup.ProgrammaticConfig(t, settings), true)
	if err != nil {
		t.Fatalf("PrepareChatAgentCatalog: %v", err)
	}
	choices := catalog.Choices()
	if len(choices) != 3 || choices[0].Role != "default" || choices[1].Role != "fast" || choices[2].Role != "worker" {
		t.Fatalf("choices = %+v", choices)
	}
	worker := choices[2]
	if worker.GetModel() != "worker-model" || worker.GetThinking() != "high" ||
		!worker.CustomSystemPrompt || !worker.CustomCapabilities || worker.AgentCallable ||
		!slices.Equal(worker.Tools, []string{"exec_command", "view_image"}) {
		t.Fatalf("worker = %+v", worker)
	}

	settings.Subagents["broken"] = config.SubagentRole{
		Settings: config.Settings{ThinkingLevel: " "},
		Sources:  map[string]config.Origin{"thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}},
	}
	_, err = PrepareChatAgentCatalog(testsetup.ProgrammaticConfig(t, settings), true)
	var typed *serverapi.ChatSettingsAgentPreparationError
	if !errors.As(err, &typed) ||
		typed.Agent != "broken" ||
		typed.Category != serverapi.ChatSettingsAgentInvalidConfiguration {
		t.Fatalf("preparation error = %T %v", err, err)
	}
}
