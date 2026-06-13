package runtime

import (
	"bytes"
	"encoding/json"
	"strings"

	"core/server/llm"
)

const legacyHistoryReplacementEngineReviewerRollback = "reviewer_rollback"

type historyReplacementEnvelope struct {
	Engine        string          `json:"engine"`
	Mode          string          `json:"mode"`
	WorkflowRunID string          `json:"workflow_run_id"`
	Items         json.RawMessage `json:"items"`
}

func normalizeHistoryReplacementEngine(engine string) string {
	engine = strings.TrimSpace(engine)
	if engine == legacyHistoryReplacementEngineReviewerRollback {
		return ""
	}
	return engine
}

func decodePersistedHistoryReplacementPayload(payload []byte) (historyReplacementPayload, bool, error) {
	var envelope historyReplacementEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return historyReplacementPayload{}, false, err
	}
	engine := strings.TrimSpace(envelope.Engine)
	if engine == legacyHistoryReplacementEngineReviewerRollback {
		return historyReplacementPayload{Engine: engine, Mode: strings.TrimSpace(envelope.Mode)}, true, nil
	}
	decoded := historyReplacementPayload{
		Engine:        engine,
		Mode:          strings.TrimSpace(envelope.Mode),
		WorkflowRunID: strings.TrimSpace(envelope.WorkflowRunID),
	}
	trimmedItems := bytes.TrimSpace(envelope.Items)
	if len(trimmedItems) == 0 || bytes.Equal(trimmedItems, []byte("null")) {
		return decoded, false, nil
	}
	if err := json.Unmarshal(trimmedItems, &decoded.Items); err != nil {
		return historyReplacementPayload{}, false, err
	}
	return decoded, false, nil
}

func transcriptEntriesFromHistoryReplacement(items []llm.ResponseItem) []ChatEntry {
	if len(items) == 0 {
		return nil
	}
	entries := visibleChatEntriesFromResponseItems(items)
	if len(entries) == 0 {
		return nil
	}
	out := make([]ChatEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, clonePersistedChatEntry(entry))
	}
	return out
}
