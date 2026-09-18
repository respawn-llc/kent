package launch

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/internal/testharness/testsetup"
	"core/shared/config"
	"core/shared/toolspec"
)

func TestOrdinaryOverridesDominateRoleWithoutChangingExplicitSupervisor(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(`
model = "base-model"
[subagents.worker]
model = "role-model"
thinking_level = "high"
[subagents.worker.model_capabilities]
supports_vision_inputs = true
[subagents.worker.tools]
exec_command = true
patch = false
[subagents.worker.reviewer]
model = "supervisor-model"
thinking_level = "medium"
[subagents.worker.reviewer.model_capabilities]
supports_vision_inputs = true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KENT_MODEL", "environment-model")
	t.Setenv("KENT_THINKING_LEVEL", "low")
	t.Setenv("KENT_MODEL_CAPABILITIES_SUPPORTS_VISION_INPUTS", "false")
	t.Setenv("KENT_TOOLS", "patch")
	app, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	effective, source, _, err := resolveSubagentSettingsWithProviderID(app.Settings, app.Source, "worker", "", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Model != "environment-model" || effective.ThinkingLevel != "low" || effective.ModelCapabilities.SupportsVisionInputs {
		t.Fatalf("role must retain ordinary environment winners: model=%s thinking=%s vision=%v", effective.Model, effective.ThinkingLevel, effective.ModelCapabilities.SupportsVisionInputs)
	}
	if !effective.EnabledTools[toolspec.ToolPatch] || len(config.EnabledToolIDs(effective)) != 1 {
		t.Fatalf("explicit list must replace all role tools: %v", effective.EnabledTools)
	}
	if effective.Reviewer.Model != "supervisor-model" || effective.Reviewer.ThinkingLevel != "medium" || !effective.Reviewer.ModelCapabilities.SupportsVisionInputs {
		t.Fatalf("ordinary overrides must not replace explicit Supervisor settings: %+v", effective.Reviewer)
	}
	if source.Sources["model"].Kind != config.SourceEnv || source.Sources["model"].Property.String() != "model" {
		t.Fatalf("model winner origin = %+v", source.Sources["model"])
	}
	app, err = config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root, Model: "cli-model", ThinkingLevel: "xhigh", Tools: "exec_command"})
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"worker", config.BuiltInSubagentRoleFast} {
		effective, source, _, err := resolveSubagentSettingsWithProviderID(app.Settings, app.Source, role, "openai", true, true)
		if err != nil {
			t.Fatal(err)
		}
		if effective.Model != "cli-model" || effective.ThinkingLevel != "xhigh" || source.Sources["model"].Kind != config.SourceCLI ||
			!effective.EnabledTools[toolspec.ToolExecCommand] || len(config.EnabledToolIDs(effective)) != 1 {
			t.Fatalf("%s must retain CLI winners over role declarations and heuristics: model=%s thinking=%s tools=%v", role, effective.Model, effective.ThinkingLevel, effective.EnabledTools)
		}
		if role == config.BuiltInSubagentRoleFast && (effective.Reviewer.Model != "cli-model" || !reflect.DeepEqual(source.Sources["reviewer.model"], source.Sources["model"])) {
			t.Fatalf("omitted Supervisor model must inherit the final model and origin: %+v %+v", effective.Reviewer, source.Sources)
		}
	}
}

func TestEffectiveRoleResolutionRejectsMissingDeclarationEvidence(t *testing.T) {
	app := loadLaunchConfig(t, t.TempDir(), "[subagents.worker]", "model = \"file-model\"")
	app.Source = config.SourceReport{}
	if _, err := ResolveConfiguredSubagentSettings(app, "worker"); err == nil {
		t.Fatal("effective role accepted missing declaration evidence")
	}
	if _, err := config.OverlaySubagentRoleProviderSettings(app, app.Settings.Subagents["worker"]); err == nil {
		t.Fatal("provider projection accepted missing declaration evidence")
	}
}

func TestCloneSettingsCopiesShellPostprocessHook(t *testing.T) {
	hook := "/tmp/role-hook"
	settings := config.Settings{
		Shell: config.ShellSettings{PostprocessHook: &hook},
		Subagents: map[string]config.SubagentRole{
			"worker": {
				Settings: config.Settings{
					Shell: config.ShellSettings{PostprocessHook: &hook},
				},
				Sources: map[string]config.Origin{"shell.postprocess_hook": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "shell.postprocess_hook"}}},
			},
		},
	}

	cloned := cloneSettings(settings)
	if cloned.Shell.PostprocessHook == settings.Shell.PostprocessHook {
		t.Fatal("top-level shell postprocess hook pointer was aliased")
	}
	role := cloned.Subagents["worker"]
	if role.Settings.Shell.PostprocessHook == settings.Subagents["worker"].Settings.Shell.PostprocessHook {
		t.Fatal("role shell postprocess hook pointer was aliased")
	}
	*cloned.Shell.PostprocessHook = "/tmp/changed-main-hook"
	*role.Settings.Shell.PostprocessHook = "/tmp/changed-role-hook"
	if *settings.Shell.PostprocessHook != "/tmp/role-hook" {
		t.Fatalf("source main hook mutated to %q", *settings.Shell.PostprocessHook)
	}
	if *settings.Subagents["worker"].Settings.Shell.PostprocessHook != "/tmp/role-hook" {
		t.Fatalf("source role hook mutated to %q", *settings.Subagents["worker"].Settings.Shell.PostprocessHook)
	}
}

func TestApplyReviewerInheritanceRecomputesDefaultBaseURLWhenReviewerProviderExplicit(t *testing.T) {
	settings := config.Settings{
		ProviderOverride: "openai",
		OpenAIBaseURL:    "http://subagent.local/v1",
		Reviewer: config.ReviewerSettings{
			ProviderOverride: "openai",
			OpenAIBaseURL:    "http://parent.local/v1",
		},
	}
	config.InheritReviewerSettings(&settings, map[string]config.Origin{
		"reviewer.provider_override": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.provider_override"}},

		"reviewer.openai_base_url": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.openai_base_url"}},
	})

	if settings.Reviewer.ProviderOverride != "openai" {
		t.Fatalf("reviewer provider override = %q, want openai", settings.Reviewer.ProviderOverride)
	}
	if settings.Reviewer.OpenAIBaseURL != "http://subagent.local/v1" {
		t.Fatalf("reviewer base URL = %q, want subagent main base URL", settings.Reviewer.OpenAIBaseURL)
	}
}

func TestOverlaySubagentRoleSettingsAppliesRegistryAndDynamicSettings(t *testing.T) {
	base := config.Settings{
		ProviderCapabilities: config.ProviderCapabilitiesOverride{
			ProviderID:                "main-provider",
			SupportsProviderVerbosity: true,
		},
		SkillToggles: map[string]bool{
			"apiresult": false,
			"inherited": false,
			"enabled":   true,
		},
	}
	role := config.SubagentRole{
		Settings: config.Settings{
			ProviderCapabilities: config.ProviderCapabilitiesOverride{
				ProviderID:                "main-provider",
				SupportsProviderVerbosity: false,
			},
			SkillToggles: map[string]bool{
				"apiresult": true,
				"enabled":   false,
			},
		},
		Sources: map[string]config.Origin{
			"provider_capabilities.supports_provider_verbosity": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "provider_capabilities.supports_provider_verbosity"}},

			"skills.apiresult": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "skills.apiresult"}},

			"skills.enabled": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "skills.enabled"}},
		},
	}

	settings, _, err := config.OverlaySubagentRoleSettings(testsetup.ProgrammaticConfig(t, base), role, true)
	if err != nil {
		t.Fatal(err)
	}

	if settings.ProviderCapabilities.SupportsProviderVerbosity {
		t.Fatalf("expected subagent verbosity capability override to apply, got %+v", settings.ProviderCapabilities)
	}
	wantToggles := map[string]bool{"apiresult": true, "inherited": false, "enabled": false}
	if !reflect.DeepEqual(settings.SkillToggles, wantToggles) {
		t.Fatalf("skill toggles = %+v, want %+v", settings.SkillToggles, wantToggles)
	}
}

func TestApplyReviewerInheritanceDoesNotCopyMainProviderCapabilitiesForExplicitReviewerEndpoint(t *testing.T) {
	settings := config.Settings{
		ProviderCapabilities: config.ProviderCapabilitiesOverride{
			ProviderID:               "main-provider",
			SupportsResponsesAPI:     true,
			SupportsPromptCacheKey:   true,
			IsOpenAIFirstParty:       true,
			SupportsNativeWebSearch:  true,
			SupportsResponsesCompact: true,
		},
		Reviewer: config.ReviewerSettings{
			ProviderOverride: "openai",
			OpenAIBaseURL:    "http://reviewer.local/v1",
		},
	}
	sources := reviewerProviderCapabilitySources()
	sources["reviewer.provider_override"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.provider_override"}}

	sources["reviewer.openai_base_url"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.openai_base_url"}}

	config.InheritReviewerSettings(&settings, sources)

	if settings.Reviewer.ProviderCapabilities != (config.ProviderCapabilitiesOverride{}) {
		t.Fatalf("expected reviewer provider capabilities to stay unset for explicit endpoint, got %+v", settings.Reviewer.ProviderCapabilities)
	}
}

func TestApplyReviewerInheritanceCopiesMainProviderCapabilitiesForNoOpReviewerProviderOverride(t *testing.T) {
	settings := config.Settings{
		OpenAIBaseURL: "http://subagent.local/v1",
		ProviderCapabilities: config.ProviderCapabilitiesOverride{
			ProviderID:             "subagent-main-provider",
			SupportsResponsesAPI:   true,
			SupportsPromptCacheKey: true,
		},
		Reviewer: config.ReviewerSettings{
			ProviderOverride: "openai",
			OpenAIBaseURL:    "http://parent.local/v1",
		},
	}
	sources := reviewerProviderCapabilitySources()
	sources["reviewer.provider_override"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.provider_override"}}

	sources["reviewer.openai_base_url"] = config.Origin{Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.openai_base_url"}}

	config.InheritReviewerSettings(&settings, sources)

	if settings.Reviewer.OpenAIBaseURL != "http://subagent.local/v1" {
		t.Fatalf("expected no-op reviewer provider override to inherit subagent main base URL, got %q", settings.Reviewer.OpenAIBaseURL)
	}
	wantCapabilities := config.ProviderCapabilitiesOverride{
		ProviderID:             "subagent-main-provider",
		SupportsResponsesAPI:   true,
		SupportsPromptCacheKey: true,
	}
	if settings.Reviewer.ProviderCapabilities != wantCapabilities {
		t.Fatalf("reviewer provider capabilities = %+v, want %+v", settings.Reviewer.ProviderCapabilities, wantCapabilities)
	}
}

func TestApplyReviewerInheritanceMergesReviewerModelCapabilitiesPerField(t *testing.T) {
	settings := config.Settings{
		ModelCapabilities: config.ModelCapabilitiesOverride{
			SupportsReasoningEffort: true,
			SupportsVisionInputs:    true,
		},
		Reviewer: config.ReviewerSettings{
			ModelCapabilities: config.ModelCapabilitiesOverride{
				SupportsReasoningEffort: false,
				SupportsVisionInputs:    false,
			},
		},
	}
	config.InheritReviewerSettings(&settings, map[string]config.Origin{
		"reviewer.model_capabilities.supports_reasoning_effort": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.model_capabilities.supports_reasoning_effort"}},

		"reviewer.model_capabilities.supports_vision_inputs": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.model_capabilities.supports_vision_inputs"}},
	})

	want := config.ModelCapabilitiesOverride{SupportsVisionInputs: true}
	if settings.Reviewer.ModelCapabilities != want {
		t.Fatalf("reviewer model capabilities = %+v, want %+v", settings.Reviewer.ModelCapabilities, want)
	}
}

func TestApplyReviewerInheritanceMergesReviewerProviderCapabilitiesPerField(t *testing.T) {
	settings := config.Settings{
		ProviderCapabilities: config.ProviderCapabilitiesOverride{
			ProviderID:                    "main-provider",
			SupportsResponsesAPI:          true,
			SupportsResponsesCompact:      true,
			SupportsPromptCacheKey:        true,
			SupportsNativeWebSearch:       true,
			SupportsReasoningEncrypted:    true,
			SupportsServerSideContextEdit: true,
			IsOpenAIFirstParty:            true,
			SupportsProviderVerbosity:     true,
		},
		Reviewer: config.ReviewerSettings{
			ProviderCapabilities: config.ProviderCapabilitiesOverride{
				ProviderID:                "reviewer-provider",
				SupportsResponsesAPI:      false,
				SupportsPromptCacheKey:    false,
				SupportsProviderVerbosity: false,
			},
		},
	}
	sources := reviewerProviderCapabilitySources()
	sources["reviewer.provider_capabilities.provider_id"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.provider_id"}}

	sources["reviewer.provider_capabilities.supports_responses_api"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_responses_api"}}

	sources["reviewer.provider_capabilities.supports_prompt_cache_key"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_prompt_cache_key"}}

	sources["reviewer.provider_capabilities.supports_provider_verbosity"] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_provider_verbosity"}}

	config.InheritReviewerSettings(&settings, sources)

	want := settings.ProviderCapabilities
	want.ProviderID = "reviewer-provider"
	want.SupportsResponsesAPI = false
	want.SupportsPromptCacheKey = false
	want.SupportsProviderVerbosity = false
	if settings.Reviewer.ProviderCapabilities != want {
		t.Fatalf("reviewer provider capabilities = %+v, want %+v", settings.Reviewer.ProviderCapabilities, want)
	}
}

func reviewerProviderCapabilitySources() map[string]config.Origin {
	sources := map[string]config.Origin{
		"reviewer.provider_capabilities.provider_id": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.provider_id"}},

		"reviewer.provider_capabilities.supports_responses_api": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_responses_api"}},

		"reviewer.provider_capabilities.supports_responses_compact": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_responses_compact"}},

		"reviewer.provider_capabilities.supports_prompt_cache_key": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_prompt_cache_key"}},

		"reviewer.provider_capabilities.supports_native_web_search": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_native_web_search"}},

		"reviewer.provider_capabilities.supports_reasoning_encrypted": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_reasoning_encrypted"}},

		"reviewer.provider_capabilities.supports_server_side_context_edit": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_server_side_context_edit"}},

		"reviewer.provider_capabilities.supports_provider_verbosity": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.supports_provider_verbosity"}},

		"reviewer.provider_capabilities.is_openai_first_party": {Kind: config.SourceDefault, Property: config.PropertyAddress{Key: "reviewer.provider_capabilities.is_openai_first_party"}},
	}
	return sources
}
