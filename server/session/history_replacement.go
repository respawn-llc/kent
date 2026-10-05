package session

import "strings"

// ProjectCompactedMeta describes the new generation before compaction runs.
// Planning can resolve its next Agent without mutating the outgoing contract.
func ProjectCompactedMeta(meta Meta) Meta {
	meta = cloneMeta(meta)
	meta.UsageState = nil
	meta.OriginalThinkingEffort = nil
	meta.Locked = nil
	return meta
}

const LegacyReviewerRollbackHistoryReplacementEngine = "reviewer_rollback"

func IsLegacyReviewerRollbackHistoryReplacementEngine(engine string) bool {
	return strings.TrimSpace(engine) == LegacyReviewerRollbackHistoryReplacementEngine
}
