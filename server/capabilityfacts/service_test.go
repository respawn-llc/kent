package capabilityfacts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/onboardingimports"
	"core/shared/config"
	capabilitypb "core/shared/protoapi/gen/kent/api/capability"
	onboardingpb "core/shared/protoapi/gen/kent/api/onboarding"
	"core/shared/serverapi"
	"core/shared/toolspec"
)

func TestGrokCatalogContextUsesActualConnection(t *testing.T) {
	for _, protocol := range []config.ConnectionProtocol{config.ConnectionGrokCLIProxy, config.ConnectionGrokOAuthAPI, config.ConnectionGrokAPIKey} {
		t.Run(string(protocol), func(t *testing.T) {
			id := config.ConnectionID("selected")
			definition := config.ProviderConnection{Protocol: protocol, Capabilities: config.ProviderCapabilitiesOverride{
				ProviderID: "chatgpt-codex", SupportsResponsesAPI: true,
			}}
			if protocol == config.ConnectionGrokAPIKey {
				definition.EnvironmentVariable = ptr("GROK_KEY")
			}
			service := NewService(Options{Config: testConfig(t, config.Settings{
				Model: "grok-4.7", Connection: &id, Connections: map[config.ConnectionID]config.ProviderConnection{id: definition},
			})})
			facts, err := service.GetFacts(t.Context(), &capabilitypb.GetFactsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			model := knownModelFact(facts.Models.KnownModels, "grok-4.7")
			if model == nil || model.ContextWindowTokens == nil || !model.SupportsThinking ||
				!model.SupportsReasoningSummary || !model.SupportsVisionInputs || model.Verbosity.Supported {
				t.Fatalf("Grok model facts = %+v", model)
			}
			if protocol == config.ConnectionGrokCLIProxy {
				if *model.ContextWindowTokens != 256_000 || model.LargeWindow == nil || model.LargeWindow.Tokens != 500_000 {
					t.Fatalf("proxy context = %+v", model)
				}
				older := knownModelFact(facts.Models.KnownModels, "grok-4.6")
				if older == nil || older.ContextWindowTokens != nil || older.LargeWindow != nil {
					t.Fatalf("proxy 4.6 borrowed public limits: %+v", older)
				}
			} else if *model.ContextWindowTokens != 500_000 || model.LargeWindow != nil {
				t.Fatalf("public context = %+v", model)
			}
			if knownModelFact(facts.Models.KnownModels, "grok-4.5") != nil {
				t.Fatal("removed Grok model is catalogued")
			}
		})
	}
}

func TestImportErrorFactsPreserveItemKind(t *testing.T) {
	command := onboardingimports.ItemKindCommand
	facts := importErrorFacts([]onboardingimports.Error{{
		Code:     "provider_discovery_failed",
		ItemKind: &command,
	}})
	if len(facts) != 1 || facts[0].ItemKind == nil {
		t.Fatalf("import error facts = %+v, want command item kind", facts)
	}
	if *facts[0].ItemKind != capabilitypb.ImportItemKind_IMPORT_ITEM_KIND_COMMAND {
		t.Fatalf("item kind = %q, want command", *facts[0].ItemKind)
	}
}

func TestServiceProjectsModelCatalogAndUnknownFallback(t *testing.T) {
	service := NewService(Options{Config: testConfig(t, config.Settings{Model: "gpt-6-sol"})})

	resp, err := service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
	if err != nil {
		t.Fatalf("GetCapabilityFacts: %v", err)
	}

	if len(resp.Models.KnownModels) == 0 {
		t.Fatal("expected known model facts")
	}
	gpt56 := knownModelFact(resp.Models.KnownModels, "gpt-5.6-sol")
	if gpt56 == nil || !gpt56.Known || gpt56.LargeWindow != nil {
		t.Fatalf("gpt-5.6-sol fact = %+v, want known without a redundant large-window choice", gpt56)
	}
	newSol := knownModelFact(resp.Models.KnownModels, "gpt-6.1-sol")
	if newSol == nil || !newSol.Known || newSol.ContextWindowTokens == nil ||
		*newSol.ContextWindowTokens != 272_000 || newSol.LargeWindow == nil ||
		newSol.LargeWindow.Tokens != 1_050_000 ||
		len(newSol.SupportedThinkingLevels) != 5 {
		t.Fatalf("gpt-6.1-sol fact = %+v, want known model with context and effort support", newSol)
	}
	sol := knownModelFact(resp.Models.KnownModels, "gpt-6-sol")
	if sol == nil || sol.ContextWindowTokens == nil || sol.LargeWindow == nil || sol.LargeWindow.Tokens <= *sol.ContextWindowTokens {
		t.Fatalf("gpt-6-sol fact = %+v, want a strictly larger optional window", sol)
	}
	if sol.DefaultContextWindowMode == nil || *sol.DefaultContextWindowMode != "standard" {
		t.Fatalf("gpt-6-sol default context mode = %#v, want standard", sol.DefaultContextWindowMode)
	}

	fallback := resp.Models.UnknownFallback
	if fallback.Known || fallback.ModelId != nil || !fallback.SupportsThinking ||
		fallback.SupportsReasoningSummary || fallback.SupportsVisionInputs ||
		fallback.Verbosity.Source != "provider_default" || !fallback.Verbosity.Supported || len(fallback.Verbosity.Levels) == 0 {
		t.Fatalf("unknown fallback = %+v, want provider-default thinking and verbosity without catalog-only capabilities", fallback)
	}
}

func TestServiceProjectsContextWindowsForSelectedConnection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		connection config.ProviderConnection
		large      uint32
	}{
		{name: "API", connection: config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: ptr("https://api.openai.com/v1")}, large: 1_050_000},
		{name: "subscription", connection: config.ProviderConnection{Protocol: config.ConnectionChatGPT}, large: 872_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := config.ConnectionID("selected")
			service := NewService(Options{Config: testConfig(t, config.Settings{
				Model: "gpt-6-sol", Connection: &id,
				Connections: map[config.ConnectionID]config.ProviderConnection{id: tc.connection},
			})})
			facts, err := service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			for _, model := range []string{"gpt-6-sol", "gpt-6.1-sol", "gpt-6-luna", "gpt-6-astra"} {
				fact := knownModelFact(facts.Models.KnownModels, model)
				if fact == nil || fact.ContextWindowTokens == nil || fact.LargeWindow == nil {
					t.Fatalf("%s missing context window choices: %+v", model, fact)
				}
				if *fact.ContextWindowTokens != 272_000 || fact.LargeWindow.Tokens != tc.large {
					t.Errorf("%s context windows = %d/%d, want %d/%d", model, *fact.ContextWindowTokens, fact.LargeWindow.Tokens, 272_000, tc.large)
				}
			}
		})
	}
}

func TestServiceProjectsProviderFactsAndExplicitProviders(t *testing.T) {
	settings := testsetup.WithResponsesProvider(config.Settings{
		Model: "gpt-6-sol",
	}, "https://api.compatible.example/v1")
	service := NewService(Options{Config: testConfig(t, settings)})

	resp, err := service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{ExplicitLlmProviderIds: []string{"openai", "OPENAI"}})
	if err != nil {
		t.Fatalf("GetCapabilityFacts: %v", err)
	}

	if resp.Providers.CurrentEffective == nil {
		t.Fatal("expected current effective provider facts")
	}
	if got := resp.Providers.CurrentEffective.LlmProviderId; got != "openai-compatible" {
		t.Fatalf("current provider id = %q, want openai-compatible", got)
	}
	if resp.Providers.CurrentEffective.Role != "current_effective" {
		t.Fatalf("current provider role = %q", resp.Providers.CurrentEffective.Role)
	}
	if resp.Providers.CurrentEffective.SupportsProviderVerbosity {
		t.Fatal("remote compatible provider must not expose provider-default verbosity")
	}
	if len(resp.Providers.Explicit) != 1 {
		t.Fatalf("explicit providers = %d, want deduplicated one", len(resp.Providers.Explicit))
	}
	if got := resp.Providers.Explicit[0].LlmProviderId; got != "openai" {
		t.Fatalf("explicit provider id = %q, want openai", got)
	}
	if resp.Providers.Explicit[0].Role != "explicit_catalog" {
		t.Fatalf("explicit provider role = %q", resp.Providers.Explicit[0].Role)
	}
	if !resp.Providers.Explicit[0].SupportsProviderVerbosity {
		t.Fatal("first-party explicit provider should expose provider-default verbosity")
	}
}

func TestServiceProjectsProviderVerbosityIndependentlyOfFirstPartyClassification(t *testing.T) {
	tests := []struct {
		name                      string
		isOpenAIFirstParty        bool
		supportsProviderVerbosity bool
	}{
		{name: "enabled for non-first-party provider", isOpenAIFirstParty: false, supportsProviderVerbosity: true},
		{name: "disabled for first-party provider", isOpenAIFirstParty: true, supportsProviderVerbosity: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := testsetup.ProviderSettings(config.Settings{Model: "operator-alias"})
			definition := settings.Connections[*settings.Connection]
			definition.Capabilities = config.ProviderCapabilitiesOverride{
				ProviderID:                "custom-provider",
				IsOpenAIFirstParty:        tt.isOpenAIFirstParty,
				SupportsProviderVerbosity: tt.supportsProviderVerbosity,
			}
			settings.Connections[*settings.Connection] = definition
			service := NewService(Options{Config: testConfig(t, settings)})

			resp, err := service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
			if err != nil {
				t.Fatalf("GetCapabilityFacts: %v", err)
			}
			if got := resp.Providers.CurrentEffective.SupportsProviderVerbosity; got != tt.supportsProviderVerbosity {
				t.Fatalf("provider verbosity = %v, want %v, provider=%+v", got, tt.supportsProviderVerbosity, resp.Providers.CurrentEffective)
			}
		})
	}
}

func TestServiceProjectsFastModeFromTheSelectedConnection(t *testing.T) {
	settings := testsetup.ProviderSettings(config.Settings{Model: "gpt-6-sol"})
	definition := settings.Connections[*settings.Connection]
	definition.Capabilities = config.ProviderCapabilitiesOverride{
		ProviderID: "custom-provider", SupportsResponsesAPI: true, SupportsFastMode: true,
	}
	settings.Connections[*settings.Connection] = definition
	service := NewService(Options{Config: testConfig(t, settings)})

	resp, err := service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
	if err != nil {
		t.Fatalf("GetCapabilityFacts: %v", err)
	}
	if !resp.Providers.CurrentEffective.SupportsFastMode {
		t.Fatalf("selected connection fast-mode capability = %+v", resp.Providers.CurrentEffective)
	}
}

func TestServiceRejectsUnsupportedExplicitProvider(t *testing.T) {
	service := NewService(Options{Config: testConfig(t, config.Settings{Model: "gpt-6-sol"})})

	_, err := service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{ExplicitLlmProviderIds: []string{"missing-provider"}})
	if !errors.Is(err, serverapi.ErrUnsupportedProvider) {
		t.Fatalf("GetCapabilityFacts error = %v, want ErrUnsupportedProvider", err)
	}
}

func TestServiceProjectsDefaults(t *testing.T) {
	service := NewService(Options{Config: testConfig(t, config.Settings{
		Model:              "custom-model",
		ThinkingLevel:      "ultra",
		ModelVerbosity:     config.ModelVerbosityHigh,
		CompactionMode:     config.CompactionModeNative,
		ModelContextWindow: 123000,
		EnabledTools:       map[toolspec.ID]bool{toolspec.ToolAskQuestion: true},
		Reviewer:           config.ReviewerSettings{Frequency: "all"},
	})})

	resp, err := service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
	if err != nil {
		t.Fatalf("GetCapabilityFacts: %v", err)
	}

	if resp.Defaults.PrimaryModelId != "custom-model" {
		t.Fatalf("primary model = %q", resp.Defaults.PrimaryModelId)
	}
	if resp.Defaults.Thinking.Mode != "level" || resp.Defaults.Thinking.Level == nil || *resp.Defaults.Thinking.Level != "ultra" {
		t.Fatalf("thinking default = %+v, want level ultra", resp.Defaults.Thinking)
	}
	if resp.Defaults.Verbosity == nil || resp.Defaults.Verbosity.Level != string(config.ModelVerbosityHigh) {
		t.Fatalf("verbosity default = %+v", resp.Defaults.Verbosity)
	}
	if resp.Defaults.CompactionMode != string(config.CompactionModeNative) {
		t.Fatalf("compaction default = %q", resp.Defaults.CompactionMode)
	}
	if resp.Defaults.GetContextWindowTokens() != 123000 || !resp.Defaults.AskQuestion ||
		resp.Defaults.Supervisor.GetFrequency() != onboardingpb.SupervisorFrequency_SUPERVISOR_FREQUENCY_ALL {
		t.Fatalf("visible onboarding defaults: %+v", resp.Defaults)
	}
	service = NewService(Options{Config: testConfig(t, config.Settings{
		Model: "custom-model",
	})})
	resp, err = service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
	if err != nil {
		t.Fatalf("GetCapabilityFacts: %v", err)
	}
	if resp.Defaults.Verbosity != nil {
		t.Fatalf("verbosity default = %+v, want nil", resp.Defaults.Verbosity)
	}
}

func TestServiceProjectsImportDomainFacts(t *testing.T) {
	home := t.TempDir()
	configRoot := t.TempDir()
	writeProviderSkill(t, home, ".claude", "skills", "helper", "Helper")
	service := NewService(Options{
		Config:  testConfigAt(configRoot, config.Settings{Model: "gpt-6-sol"}),
		HomeDir: home,
	})

	resp, err := service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
	if err != nil {
		t.Fatalf("GetCapabilityFacts: %v", err)
	}

	if len(resp.Imports.Skills.Choices) < 2 {
		t.Fatalf("expected none plus provider skill import choice, got %+v", resp.Imports.Skills.Choices)
	}
	if resp.Imports.Recommendations.Skills == nil || resp.Imports.Recommendations.Skills.ItemCount != 1 {
		t.Fatalf("skill recommendation = %+v", resp.Imports.Recommendations.Skills)
	}
	var found bool
	for _, item := range resp.Imports.Skills.Items {
		if item.Ref.ImportProviderId != nil && *item.Ref.ImportProviderId == "claude_code" && item.Ref.TargetName == "helper" {
			found = true
		}
	}
	if !found {
		t.Fatalf("projected skill items missing claude_code helper: %+v", resp.Imports.Skills.Items)
	}
	if len(resp.Imports.SkillEnablement) == 0 {
		t.Fatal("expected skill enablement projections")
	}
	if err := os.MkdirAll(filepath.Join(configRoot, "skills", "existing"), 0o755); err != nil {
		t.Fatalf("mkdir existing skills target: %v", err)
	}
	resp, err = service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
	if err != nil {
		t.Fatalf("GetCapabilityFacts: %v", err)
	}
	if !resp.Imports.Skills.Target.Skip || len(resp.Imports.Skills.Target.Conflicts) == 0 {
		t.Fatalf("expected skill target skip facts, got %+v", resp.Imports.Skills.Target)
	}
}

func TestServiceProjectsHomeResolutionFailureAsImportErrorFact(t *testing.T) {
	t.Setenv("HOME", "")
	service := NewService(Options{Config: testConfigAt(t.TempDir(), config.Settings{Model: "gpt-6-sol"})})

	resp, err := service.GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
	if err != nil {
		t.Fatalf("GetCapabilityFacts: %v", err)
	}
	if len(resp.Models.KnownModels) == 0 {
		t.Fatal("expected non-import fact groups to still be projected")
	}
	var found bool
	for _, importErr := range resp.Imports.Errors {
		if importErr.Code == "home_dir_resolution_failed" && importErr.Operation == "resolve_home_dir" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected home-dir import error fact, got %+v", resp.Imports.Errors)
	}
}

func testConfig(t *testing.T, settings config.Settings) config.App {
	t.Helper()
	return testConfigAt(t.TempDir(), settings)
}

func knownModelFact(facts []*capabilitypb.ModelFact, modelID string) *capabilitypb.ModelFact {
	for _, fact := range facts {
		if fact.ModelId != nil && *fact.ModelId == modelID {
			return fact
		}
	}
	return nil
}

func testConfigAt(root string, settings config.Settings) config.App {
	return config.App{PersistenceRoot: root, Settings: testsetup.ProviderSettings(settings)}
}

func writeProviderSkill(t *testing.T, home string, providerHome string, sourceDir string, dirName string, name string) {
	t.Helper()
	path := filepath.Join(home, providerHome, sourceDir, dirName)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	body := "---\nname: " + name + "\ndescription: helper\n---\n"
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}
