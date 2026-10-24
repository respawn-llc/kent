package modelcontract

type ModelMetadata struct {
	ContextWindowTokens      int
	LargeContextWindowTokens *int
}

type ProviderCapabilities struct {
	ProviderID string
	// ReasoningType is a live transport fact, not a configurable or locked capability.
	ReasoningType *ReasoningType
	// SupportsSpeechToText is a live connection fact, never a locked capability.
	SupportsSpeechToText          bool
	SupportsResponsesAPI          bool
	SupportsFastMode              bool
	SupportsNativeThinkingUpdates bool
	SupportsResponsesCompact      bool
	SupportsPromptCacheKey        bool
	SupportsNativeWebSearch       bool
	SupportsReasoningEncrypted    bool
	SupportsServerSideContextEdit bool
	SupportsProviderVerbosity     bool
	IsOpenAIFirstParty            bool
}
