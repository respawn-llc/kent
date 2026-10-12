package session

import "strings"

func IsContextBoundary(kind EventKind) bool {
	return kind == EventKindHistoryReplace
}

const LegacyReviewerRollbackHistoryReplacementEngine = "reviewer_rollback"

func IsLegacyReviewerRollbackHistoryReplacementEngine(engine string) bool {
	return strings.TrimSpace(engine) == LegacyReviewerRollbackHistoryReplacementEngine
}
