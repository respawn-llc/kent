package onboarding_test

import (
	"testing"

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
