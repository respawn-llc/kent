package llm

import (
	"slices"
	"testing"
	"time"
)

func TestGPT6Catalog(t *testing.T) {
	for _, test := range []struct {
		model               string
		cutoff              time.Month
		efforts             []string
		subscriptionContext int
	}{
		{"gpt-6-astra", time.April, []string{"low", "medium", "high", "xhigh", "max"}, 272_000},
		{"gpt-6.1-sol", time.April, []string{"low", "medium", "high", "xhigh", "max"}, 272_000},
		{"gpt-6.1-terra", time.June, []string{"none", "low", "medium", "high", "xhigh", "max"}, 500_000},
		{"gpt-6-sol", time.April, []string{"none", "low", "medium", "high", "xhigh", "max"}, 272_000},
		{"gpt-6-luna", time.May, []string{"none", "low", "medium", "high", "xhigh", "max"}, 272_000},
	} {
		t.Run(test.model, func(t *testing.T) {
			contract, ok := LookupModelCapabilityContract(test.model)
			if !ok {
				t.Fatal("model missing from catalog")
			}
			if contract.KnowledgeCutoff == nil || *contract.KnowledgeCutoff != (ModelKnowledgeCutoff{Month: test.cutoff, Year: 2026}) {
				t.Fatalf("knowledge cutoff = %+v", contract.KnowledgeCutoff)
			}
			if !slices.Equal(SupportedThinkingLevelsModel(test.model), test.efforts) {
				t.Fatalf("thinking levels = %v", SupportedThinkingLevelsModel(test.model))
			}
			if !contract.SupportsVisionInputs || !contract.SupportsReasoningSummary || !contract.SupportsVerbosity || !contract.SupportsNativeThinkingUpdates {
				t.Fatalf("missing GPT-6 capabilities: %+v", contract)
			}
			if contract.ContextWindowTokens != 272_000 || contract.LargeContextWindowTokens == nil || *contract.LargeContextWindowTokens != 1_050_000 {
				t.Fatalf("context windows = %d/%v, want 272000/1050000", contract.ContextWindowTokens, contract.LargeContextWindowTokens)
			}
			subscription := contract.ContextMetadata("chatgpt-codex")
			if subscription == nil || subscription.ContextWindowTokens != test.subscriptionContext ||
				subscription.LargeContextWindowTokens == nil || *subscription.LargeContextWindowTokens != 872_000 {
				t.Fatalf("subscription context = %+v, want %d/872000", subscription, test.subscriptionContext)
			}
		})
	}
}
