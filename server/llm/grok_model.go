package llm

// Grok 4.5 remains manually selectable but cannot use native Web Search.
// Other uncatalogued models retain provider validation.
func SupportsNativeWebSearchModel(model string, capabilities ProviderCapabilities) bool {
	return capabilities.SupportsNativeWebSearch && model != "grok-4.5"
}
