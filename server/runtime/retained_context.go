package runtime

import (
	"core/server/llm"
	"core/server/session"
)

// Only the latest context boundary can contain an active encrypted checkpoint.
// Retained reasoning remains subject to the ordinary dispatch compatibility path.
func ValidateSessionProviderChange(log session.MaterializedEventLog, destination llm.ProviderCapabilities) error {
	var matchErr error
	window, err := log.ReadNewestSegmentBackward(compactionBoundaryMatcher(&matchErr))
	if err != nil {
		return err
	}
	if matchErr != nil {
		return matchErr
	}
	for _, record := range window.Records {
		payload, err := record.Payload()
		if err != nil {
			return err
		}
		replacement, ok := payload.(session.HistoryReplacementRecord)
		if !ok {
			continue
		}
		items := replacement.Items
		if replacement.CompactedOutput != nil {
			items = replacement.CompactedOutput.Summary
		}
		prepared := make([]llm.ResponseItem, 0, len(items))
		for _, item := range items {
			prepared = append(prepared, llmResponseItemFromSessionHistory(item))
		}
		_, _, err = prepareRetainedContext(prepared, destination)
		return err
	}
	return nil
}

// The dispatch snapshot owns this slice. Payloads and the retained chat-store
// source remain unchanged, so switching back can reuse compatible reasoning.
func prepareRetainedContext(items []llm.ResponseItem, destination llm.ProviderCapabilities) ([]llm.ResponseItem, int, error) {
	destinationType := destination.ReasoningType
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
