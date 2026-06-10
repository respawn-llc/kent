package tui

import "builder/shared/transcript"

const roleManualCompactionCarryover = TranscriptRoleManualCompactionCarryover
const roleCompactionSummary = TranscriptRoleCompactionSummary
const roleDeveloperContext = TranscriptRoleDeveloperContext
const roleDeveloperFeedback = TranscriptRoleDeveloperFeedback
const roleDeveloperErrorFeedback = TranscriptRoleDeveloperErrorFeedback
const roleInterruption = TranscriptRoleInterruption

const interruptionUserVisibleText = "You interrupted"

func isVisibleInOngoing(entry TranscriptEntry) bool {
	switch entryVisibility(entry) {
	case transcript.EntryVisibilityDetailOnly:
		return false
	default:
		return true
	}
}

func entryVisibility(entry TranscriptEntry) transcript.EntryVisibility {
	if explicit := transcript.NormalizeEntryVisibility(entry.Visibility); explicit != transcript.EntryVisibilityAuto {
		return explicit
	}
	return defaultEntryVisibilityForRole(TranscriptRoleFromWire(string(entry.Role)))
}

func defaultEntryVisibilityForRole(role TranscriptRole) transcript.EntryVisibility {
	switch role {
	case TranscriptRoleThinking, TranscriptRoleThinkingTrace, TranscriptRoleReasoning, TranscriptRoleCompactionSummary, TranscriptRoleDeveloperContext, TranscriptRoleManualCompactionCarryover, TranscriptRoleInterruption, TranscriptRoleError, TranscriptRoleWarning, TranscriptRoleCacheWarning:
		return transcript.EntryVisibilityDetailOnly
	default:
		if transcriptMessageStyleForIntent(role.DisplayIntent("")) == transcriptMessageStyleWarning {
			return transcript.EntryVisibilityDetailOnly
		}
		return transcript.EntryVisibilityAll
	}
}

func isStyledMetaRole(role RenderIntent) bool {
	return TranscriptRole(role).IsCompaction() || transcriptMessageStyleForIntent(role) != transcriptMessageStyleNone || role == RenderIntentDeveloperContext || role == RenderIntentDeveloperFeedback || role == RenderIntentInterruption
}

func transcriptDisplayText(role RenderIntent, text string) string {
	switch role {
	case RenderIntentInterruption:
		return interruptionUserVisibleText
	default:
		return text
	}
}
