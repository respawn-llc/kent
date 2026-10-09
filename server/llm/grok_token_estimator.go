package llm

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
