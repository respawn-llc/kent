package runtime

import "core/server/llm"

// The dispatch snapshot owns this slice. Payloads and the retained chat-store
// source remain unchanged, so switching back can reuse compatible reasoning.
func prepareRetainedContext(items []llm.ResponseItem, destination llm.ProviderCapabilities) ([]llm.ResponseItem, int, error) {
	destinationType := llm.LookupProviderReasoningType(destination.ProviderID)
	knownMismatch := func(item llm.ResponseItem) bool {
		return destinationType != nil && item.Attribution != nil && item.Attribution.Type != nil && *item.Attribution.Type != *destinationType
	}
	for _, item := range items {
		if item.Type == llm.ResponseItemTypeCompaction && (!destination.SupportsResponsesAPI || knownMismatch(item)) {
			return nil, 0, &llm.RetainedContextCompatibilityError{ItemType: item.Type}
		}
	}
	kept := 0
	for _, item := range items {
		if item.Type == llm.ResponseItemTypeReasoning && item.EncryptedContent != nil &&
			(!destination.SupportsReasoningEncrypted || knownMismatch(item)) {
			continue
		}
		items[kept] = item
		kept++
	}
	omitted := len(items) - kept
	clear(items[kept:])
	return items[:kept], omitted, nil
}
