package runtime

import (
	"context"
	"errors"

	"core/server/llm"
	"core/server/session"
)

// Environment is part of every complete generation context. A post-completion
// replacement without it contains only the summary and captured shell reminder,
// awaiting the next role's configuration.
func replacementHasBaseMetaContext(items []llm.ResponseItem, mode session.CompactionMode) bool {
	if mode != session.CompactionModeWorkflowPostCompletion {
		return len(items) > 0
	}
	for _, item := range items {
		if item.MessageType != nil && *item.MessageType == llm.MessageTypeEnvironment {
			return true
		}
	}
	return false
}

func (e *Engine) hydrateWorkflowCompactionContext(ctx context.Context, stepID string) error {
	if _, locked := e.lockedContractState().Snapshot(); locked {
		return errors.New("workflow compaction context must be hydrated before model dispatch")
	}
	items, replacementEnd := e.transcriptRuntimeState().SnapshotRequestItems()
	if replacementEnd == nil {
		return errors.New("workflow compaction context requires a committed replacement")
	}
	projection, err := e.compactionGenerationMetaContextProjection(ctx, compactionModeWorkflowPostCompletion)
	if err != nil {
		return err
	}
	compacted := items[:*replacementEnd]
	engine := "local"
	prefixEnd := 0
	environmentIndex := len(compacted)
	for index, item := range compacted {
		if item.Type == llm.ResponseItemTypeCompaction {
			engine = "remote"
			prefixEnd = index
		}
		if item.MessageType != nil && *item.MessageType == llm.MessageTypeCompactionPreservedUserMessage {
			environmentIndex = index
		}
	}
	// Keep the native continuation notice first and the captured shell reminder
	// beside the unchanged checkpoint. Only this undispatched generation is
	// assembled here; no earlier model input is rewritten.
	hydrated := append([]llm.ResponseItem(nil), compacted[:prefixEnd]...)
	hydrated = append(hydrated, llm.ItemsFromMessages(projection.StablePrefix)...)
	hydrated = append(hydrated, compacted[prefixEnd:environmentIndex]...)
	hydrated = append(hydrated, llm.ItemsFromMessages(projection.Environment)...)
	hydrated = append(hydrated, items[environmentIndex:]...)
	receipt, err := e.steerWithCommitReceipt(stepID, steerHistoryReplacementIntent(
		engine,
		compactionModeWorkflowPostCompletion,
		e.compactionRuntimeState().Count(),
		e.LastCommittedAssistantFinalAnswer(),
		hydrated,
	))
	if err != nil {
		return err
	}
	if !receipt.Committed {
		return errors.New("workflow compaction context replacement was not committed")
	}
	return nil
}
