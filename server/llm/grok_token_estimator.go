package llm

// Arithmetic adapted from Grok Build 2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8.
// Copyright 2023–2026 SpaceXAI, Apache-2.0; see third_party/grok-build.
type GrokTokenEstimator struct{}

func (GrokTokenEstimator) EstimateText(text string) int {
	return len(text) / 4
}

func (GrokTokenEstimator) EstimateImageBytes(ImageEstimateInput) int {
	return 765 * 4
}

func (estimator GrokTokenEstimator) EstimateItem(item ResponseItem) int {
	if item.Type == ResponseItemTypeReasoning || item.Type == ResponseItemTypeCompaction {
		plaintext := item
		plaintext.EncryptedContent = nil
		return max(EstimateItemContentBytes(estimator, plaintext), stringBytes(item.EncryptedContent)*3/4) / 4
	}
	return EstimateItemContentBytes(estimator, item) / 4
}
