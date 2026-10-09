package onboarding_test

import (
	"testing"

	"core/shared/config"
	onboardingpb "core/shared/protoapi/gen/kent/api/onboarding"
)

func TestDisabledThinkingPersistsProviderEffort(t *testing.T) {
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna"} {
		t.Run(model, func(t *testing.T) {
			root := t.TempDir()
			_, err := newTestFinalizer(t, root, t.TempDir()).Finalize(t.Context(), &onboardingpb.FinalizeRequest{
				Model:    &onboardingpb.ModelChoice{Kind: onboardingpb.ModelKind_MODEL_KIND_KNOWN, ModelId: &model},
				Thinking: &onboardingpb.ThinkingChoice{Kind: onboardingpb.ThinkingKind_THINKING_KIND_DISABLED},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := loadFinalizedConfig(t, root).Settings.ThinkingLevel; got != "none" {
				t.Fatalf("persisted disabled Thinking = %q", got)
			}
		})
	}
}

func TestOmittedSupervisorDefaultPreservesExplicitThinkingChoice(t *testing.T) {
	root := t.TempDir()
	model := "gpt-6-sol"
	_, err := newTestFinalizer(t, root, t.TempDir()).Finalize(t.Context(), &onboardingpb.FinalizeRequest{
		Model:    &onboardingpb.ModelChoice{Kind: onboardingpb.ModelKind_MODEL_KIND_KNOWN, ModelId: &model},
		Thinking: &onboardingpb.ThinkingChoice{Kind: onboardingpb.ThinkingKind_THINKING_KIND_DISABLED},
	})
	if err != nil {
		t.Fatal(err)
	}

	app := loadFinalizedConfig(t, root)
	settings := app.Settings
	if settings.Model != model || settings.ThinkingLevel != "none" || settings.Reviewer.Model != "gpt-6-luna" {
		t.Fatalf("finalized model/thinking/Supervisor = %q/%q/%q, want %q/none/gpt-6-luna",
			settings.Model, settings.ThinkingLevel, settings.Reviewer.Model, model)
	}
	thinkingOrigin, ok := app.Source.Sources["thinking_level"]
	if !ok || thinkingOrigin.Kind != config.SourceFileKind {
		t.Fatalf("explicit disabled Thinking origin = %+v (present %t), want persisted file setting", thinkingOrigin, ok)
	}
}

func TestDisabledThinkingReviewerPersistsIndependentEffort(t *testing.T) {
	root := t.TempDir()
	_, err := newTestFinalizer(t, root, t.TempDir()).Finalize(t.Context(), &onboardingpb.FinalizeRequest{
		Model:    &onboardingpb.ModelChoice{Kind: onboardingpb.ModelKind_MODEL_KIND_KNOWN, ModelId: ptr("gpt-6-astra")},
		Thinking: &onboardingpb.ThinkingChoice{Kind: onboardingpb.ThinkingKind_THINKING_KIND_LEVEL, Level: ptr("high")},
		Supervisor: &onboardingpb.SupervisorChoice{
			Frequency: onboardingpb.SupervisorFrequency_SUPERVISOR_FREQUENCY_ALL,
			Model:     &onboardingpb.ModelChoice{Kind: onboardingpb.ModelKind_MODEL_KIND_KNOWN, ModelId: ptr("gpt-6-luna")},
			Thinking:  &onboardingpb.ThinkingChoice{Kind: onboardingpb.ThinkingKind_THINKING_KIND_DISABLED},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := loadFinalizedConfig(t, root).Settings
	if settings.ThinkingLevel != "high" || settings.Reviewer.ThinkingLevel != "none" {
		t.Fatalf("main/reviewer Thinking = %q/%q", settings.ThinkingLevel, settings.Reviewer.ThinkingLevel)
	}
}
