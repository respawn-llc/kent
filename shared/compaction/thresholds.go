package compaction

const (
	MinimumWindowPercent         = 50
	DefaultPreSubmitRunwayTokens = 35_000
	EstimatedTokensPerToolCall   = 1_400

	estimatedToolCallCorpusStartDate              = "2026-04-12"
	estimatedToolCallCorpusCompactionModeFilter   = "auto,handoff"
	estimatedToolCallCorpusSampleSize             = 67
	estimatedToolCallCorpusThresholdTokens        = 258_400
	estimatedToolCallCorpusMedianDistance         = 187
	estimatedToolCallCorpusMeanDistanceCenticalls = 18_491
)

func MinimumThresholdTokens(window int) int {
	if window <= 0 {
		return 0
	}
	threshold := window * MinimumWindowPercent / 100
	if threshold < 1 {
		return 1
	}
	return threshold
}

func EffectivePreSubmitThresholdTokens(autoThreshold int, runwayTokens int) int {
	if autoThreshold <= 0 {
		return 0
	}
	threshold := autoThreshold - runwayTokens
	if threshold < 1 {
		return 1
	}
	return threshold
}

// EstimatedToolCallsForTokenBudget converts a token budget into an estimated
// number of assistant tool calls before that budget is exhausted.
//
// EstimatedTokensPerToolCall is derived from the local Kent session corpus:
// history_replaced events after estimatedToolCallCorpusStartDate, filtered to
// estimatedToolCallCorpusCompactionModeFilter to exclude manual /compact runs,
// produced estimatedToolCallCorpusSampleSize data points. The active compaction
// threshold was estimatedToolCallCorpusThresholdTokens. Median distance was
// estimatedToolCallCorpusMedianDistance tool calls, and mean distance was
// estimatedToolCallCorpusMeanDistanceCenticalls / 100. The resulting ratios are
// 258,400 / 187 = 1,382 tokens/call and 258,400 / 184.91 = 1,397 tokens/call,
// rounded to 1,400 for a stable heuristic.
func EstimatedToolCallsForTokenBudget(tokenBudget int) int {
	if tokenBudget <= 0 {
		return 0
	}
	quotient := tokenBudget / EstimatedTokensPerToolCall
	remainder := tokenBudget % EstimatedTokensPerToolCall
	if remainder >= EstimatedTokensPerToolCall/2 && quotient < int(^uint(0)>>1) {
		quotient++
	}
	return quotient
}

func EstimatedToolCallsForCompactionThreshold(thresholdTokens int) int {
	return EstimatedToolCallsForTokenBudget(thresholdTokens)
}

func EstimatedToolCallsForContextWindow(windowTokens int, compactionThresholdPercent int) int {
	if windowTokens <= 0 || compactionThresholdPercent <= 0 {
		return 0
	}
	if compactionThresholdPercent > 100 {
		compactionThresholdPercent = 100
	}
	budget := percentOfInt(windowTokens, compactionThresholdPercent)
	return EstimatedToolCallsForTokenBudget(clampInt64ToInt(budget))
}

func percentOfInt(value int, percent int) int64 {
	whole := int64(value / 100)
	remainder := int64(value % 100)
	return whole*int64(percent) + remainder*int64(percent)/100
}

func clampInt64ToInt(value int64) int {
	if value <= 0 {
		return 0
	}
	limit := int64(int(^uint(0) >> 1))
	if value > limit {
		return int(limit)
	}
	return int(value)
}
