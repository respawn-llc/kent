package llm

import (
	"slices"
	"testing"
	"time"
)

func TestGPT6Catalog(t *testing.T) {
	for _, test := range []struct {
		model   string
		cutoff  time.Month
		efforts []string
	}{
		{"gpt-6-astra", time.April, []string{"low", "medium", "high", "xhigh", "max"}},
		{"gpt-6-sol", time.April, []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{"gpt-6-luna", time.May, []string{"none", "low", "medium", "high", "xhigh", "max"}},
	} {
		t.Run(test.model, func(t *testing.T) {
			contract, ok := LookupModelCapabilityContract(test.model)
			if !ok {
				t.Fatal("model missing from catalog")
			}
			if !contract.HasKnowledgeCutoff || contract.KnowledgeCutoff != (ModelKnowledgeCutoff{Month: test.cutoff, Year: 2026}) {
				t.Fatalf("knowledge cutoff = %+v", contract.KnowledgeCutoff)
			}
			if !slices.Equal(SupportedThinkingLevelsModel(test.model), test.efforts) {
				t.Fatalf("thinking levels = %v", SupportedThinkingLevelsModel(test.model))
			}
			if !contract.SupportsVisionInputs || !contract.SupportsReasoningSummary || !contract.SupportsVerbosity || !contract.SupportsNativeThinkingUpdates {
				t.Fatalf("missing GPT-6 capabilities: %+v", contract)
			}
		})
	}
}
