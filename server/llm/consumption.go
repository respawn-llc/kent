package llm

import (
	"fmt"
	"math"
)

// TokenPricing uses integer nanodollars per token.
type TokenPricing struct {
	Input       int64
	CachedInput int64
	Output      int64
}

func EstimateConsumption(model string, usage Usage) (int64, error) {
	price := TokenPricing{Input: 1_000, CachedInput: 100, Output: 10_000}
	if contract, known := LookupModelCapabilityContract(model); known && contract.TokenPricing != nil {
		price = *contract.TokenPricing
	}
	var input, cached, output int64
	if usage.InputTokens != nil {
		input = int64(*usage.InputTokens)
	}
	if usage.CachedInputTokens != nil {
		cached = int64(*usage.CachedInputTokens)
	}
	if usage.OutputTokens != nil {
		output = int64(*usage.OutputTokens)
	}
	if input < 0 || cached < 0 || output < 0 {
		return 0, fmt.Errorf("negative token usage for model %q", model)
	}
	var total int64
	for _, category := range []struct{ tokens, rate int64 }{
		{max(input-cached, 0), price.Input}, {cached, price.CachedInput}, {output, price.Output},
	} {
		if category.rate < 0 || (category.tokens > 0 && category.rate > (math.MaxInt64-total)/category.tokens) {
			return 0, fmt.Errorf("unrepresentable consumption estimate for model %q", model)
		}
		total += category.tokens * category.rate
	}
	return total, nil
}
