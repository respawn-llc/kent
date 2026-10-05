package app

import (
	"context"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/capabilityfacts"
	"core/shared/config"
	capabilitypb "core/shared/protoapi/gen/kent/api/capability"
)

func newOnboardingModelForWorkspace(_ string, _ string, state onboardingFlowState) *onboardingModel {
	return newOnboardingModel(nil, state)
}

func testOnboardingFlowState(t *testing.T, mutate func(*config.App), suppliedFacts ...*capabilitypb.Facts) onboardingFlowState {
	t.Helper()
	cfg := onboardingSeedConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	normalizeOnboardingReviewerSeedInheritance(&cfg)
	facts := testOnboardingCapabilityFacts()
	if len(suppliedFacts) > 0 {
		facts = suppliedFacts[0]
	}
	state, err := testOnboardingStateFromServerConfig(t, cfg, facts)
	if err != nil {
		t.Fatalf("construct test onboarding state: %v", err)
	}
	return state
}

func testOnboardingStateFromServerConfig(t *testing.T, cfg config.App, facts *capabilitypb.Facts) (onboardingFlowState, error) {
	t.Helper()
	cfg.PersistenceRoot = t.TempDir()
	cfg.Settings = testsetup.ProviderSettings(cfg.Settings)
	serverFacts, err := capabilityfacts.NewService(capabilityfacts.Options{Config: cfg, HomeDir: t.TempDir()}).GetFacts(
		context.Background(), &capabilitypb.GetFactsRequest{},
	)
	if err != nil {
		return onboardingFlowState{}, err
	}
	facts.Defaults = serverFacts.Defaults
	return newOnboardingFlowState(config.LocalPreferences{Theme: cfg.Settings.Theme, Debug: cfg.Settings.Debug}, facts)
}
func testOnboardingFlowStatePtr(t *testing.T, mutate func(*config.App), suppliedFacts ...*capabilitypb.Facts) *onboardingFlowState {
	t.Helper()
	state := testOnboardingFlowState(t, mutate, suppliedFacts...)
	return &state
}

func emptyOnboardingCapabilityFacts() *capabilitypb.Facts {
	return &capabilitypb.Facts{
		Models: &capabilitypb.ModelFacts{
			UnknownFallback: &capabilitypb.ModelFact{
				Verbosity: &capabilitypb.ModelVerbosityFact{Source: "test"},
			},
		},
		Providers: &capabilitypb.ProviderFacts{},
		Imports: &capabilitypb.ImportFacts{
			Workspace:       &capabilitypb.ImportWorkspaceFact{},
			Skills:          &capabilitypb.ImportItemGroupFact{Target: &capabilitypb.ImportTargetFact{}},
			Commands:        &capabilitypb.ImportItemGroupFact{Target: &capabilitypb.ImportTargetFact{}},
			Recommendations: &capabilitypb.ImportRecommendationFacts{},
		},
		Defaults: &capabilitypb.DefaultFacts{
			PrimaryModelId: "test-model",
			Thinking:       &capabilitypb.ThinkingDefaultFact{Mode: "default"},
			CompactionMode: "native",
		},
		Recommendations: &capabilitypb.RecommendationFacts{},
	}
}

func emptyOnboardingImportFacts() *capabilitypb.ImportFacts {
	return emptyOnboardingCapabilityFacts().Imports
}

func normalizeOnboardingReviewerSeedInheritance(cfg *config.App) {
	if cfg.Source.Sources["reviewer.model"].Inherited("reviewer.model") {
		cfg.Settings.Reviewer.Model = cfg.Settings.Model
	}
	if cfg.Source.Sources["reviewer.thinking_level"].Inherited("reviewer.thinking_level") {
		cfg.Settings.Reviewer.ThinkingLevel = cfg.Settings.ThinkingLevel
	}
}
