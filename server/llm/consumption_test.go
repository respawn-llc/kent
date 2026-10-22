package llm_test

import (
	"math"
	"testing"

	"core/server/llm"
	"core/shared/textutil"
)

func TestConsumptionRejectsInvalidArithmetic(t *testing.T) {
	for _, usage := range []llm.Usage{
		{InputTokens: textutil.Value(-1)},
		{OutputTokens: textutil.Value(math.MaxInt)},
	} {
		if _, err := llm.EstimateConsumption("unknown-model", usage); err == nil {
			t.Fatal("expected invalid estimate to fail")
		}
	}
}

func TestConsumptionUncachedInput(t *testing.T) {
	score, err := llm.EstimateConsumption("unknown-model", llm.Usage{InputTokens: textutil.Value(12)})
	if err != nil || score != 12_000 {
		t.Fatalf("score = %d, error = %v", score, err)
	}
}

func TestConsumptionOutputAndMissingFields(t *testing.T) {
	for _, tc := range []struct {
		usage llm.Usage
		want  int64
	}{
		{llm.Usage{OutputTokens: textutil.Value(3)}, 30_000},
		{llm.Usage{CachedInputTokens: textutil.Value(5)}, 500},
		{llm.Usage{}, 0},
	} {
		score, err := llm.EstimateConsumption("unknown-model", tc.usage)
		if err != nil || score != tc.want {
			t.Fatalf("score = %d, want %d, error = %v", score, tc.want, err)
		}
		if tc.usage.InputTokens != nil {
			t.Fatal("estimate must not fill missing input usage")
		}
	}
}

func TestConsumptionCachedInputCountedOnce(t *testing.T) {
	for _, input := range []int{100, 60} {
		score, err := llm.EstimateConsumption("unknown-model", llm.Usage{
			InputTokens: textutil.Value(input), CachedInputTokens: textutil.Value(80),
		})
		want := int64(max(input-80, 0)*1_000 + 80*100)
		if err != nil || score != want {
			t.Fatalf("input %d: score = %d, want %d, error = %v", input, score, want, err)
		}
	}
}

func TestConsumptionResolvesModelPricing(t *testing.T) {
	contract, known := llm.LookupModelCapabilityContract("gpt-6-astra")
	if !known || contract.TokenPricing == nil {
		t.Fatal("verified model pricing is missing")
	}
	price := contract.TokenPricing
	score, err := llm.EstimateConsumption("gpt-6-astra", llm.Usage{
		InputTokens: textutil.Value(10), CachedInputTokens: textutil.Value(6), OutputTokens: textutil.Value(2),
	})
	want := 4*price.Input + 6*price.CachedInput + 2*price.Output
	if err != nil || score != want || score == 24_600 {
		t.Fatalf("score = %d, want verified %d, error = %v", score, want, err)
	}
}
