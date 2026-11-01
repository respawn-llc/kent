package runtime

import (
	"sync"

	"core/shared/textutil"
)

type usageEstimateBaseline struct {
	reportedContextTokens   *int
	estimatedProviderTokens int
}

type tokenUsageTracker struct {
	mu sync.Mutex

	usageBaseline usageEstimateBaseline
}

func newTokenUsageTracker() *tokenUsageTracker {
	return &tokenUsageTracker{}
}

func (t *tokenUsageTracker) storeUsageBaseline(reportedContextTokens *int, estimatedProviderTokens int) {
	if t == nil {
		return
	}
	if estimatedProviderTokens < 0 {
		estimatedProviderTokens = 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.usageBaseline = usageEstimateBaseline{
		reportedContextTokens:   textutil.Pointer(reportedContextTokens),
		estimatedProviderTokens: estimatedProviderTokens,
	}
}

func (t *tokenUsageTracker) estimateCurrentContextTokens(currentEstimatedProviderTokens int) (int, bool) {
	if t == nil {
		return 0, false
	}
	if currentEstimatedProviderTokens < 0 {
		currentEstimatedProviderTokens = 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	baseline := t.usageBaseline
	if baseline.reportedContextTokens == nil {
		if currentEstimatedProviderTokens <= 0 {
			return 0, false
		}
		return currentEstimatedProviderTokens, true
	}
	delta := currentEstimatedProviderTokens - baseline.estimatedProviderTokens
	if delta <= 0 {
		return *baseline.reportedContextTokens, true
	}
	return *baseline.reportedContextTokens + delta, true
}
