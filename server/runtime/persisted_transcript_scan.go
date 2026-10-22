package runtime

import (
	"fmt"
	"strings"

	"core/server/llm"
	"core/server/session"
	"core/shared/textutil"
	"core/shared/transcript"
	patchformat "core/shared/transcript/patchformat"
)

func clonePersistedChatEntries(entries []ChatEntry) []ChatEntry {
	if len(entries) == 0 {
		return nil
	}
	cloned := make([]ChatEntry, 0, len(entries))
	for _, entry := range entries {
		cloned = append(cloned, clonePersistedChatEntry(entry))
	}
	return cloned
}

func clonePersistedChatEntry(entry ChatEntry) ChatEntry {
	copyEntry := entry
	copyEntry.StepID = cloneOptionalStepID(entry.StepID)
	copyEntry.RollbackTargetID = textutil.Pointer(entry.RollbackTargetID)
	copyEntry.DurationMs = textutil.Pointer(entry.DurationMs)
	copyEntry.ThinkingEffort = textutil.Pointer(entry.ThinkingEffort)
	copyEntry.CompactionNumber = textutil.Pointer(entry.CompactionNumber)
	copyEntry.BackgroundExitCode = textutil.Pointer(entry.BackgroundExitCode)
	copyEntry.WorktreeContext = session.CloneWorktreeContext(entry.WorktreeContext)
	copyEntry.ToolCall = clonePersistedToolCallMeta(entry.ToolCall)
	copyEntry.QuestionAnswer = cloneAskQuestionAnswer(entry.QuestionAnswer)
	copyEntry.CommittedProvenance = cloneTranscriptCommittedRowProvenance(entry.CommittedProvenance)
	if entry.ReviewerFeedback != nil {
		feedback := *entry.ReviewerFeedback
		feedback.Suggestions = append([]string(nil), entry.ReviewerFeedback.Suggestions...)
		copyEntry.ReviewerFeedback = &feedback
	}
	if entry.ReviewerError != nil {
		reviewerError := *entry.ReviewerError
		copyEntry.ReviewerError = &reviewerError
	}
	return copyEntry
}

func clonePersistedToolCallMeta(meta *transcript.ToolCallMeta) *transcript.ToolCallMeta {
	if meta == nil {
		return nil
	}
	copyMeta := *meta
	if len(meta.Suggestions) > 0 {
		copyMeta.Suggestions = append([]string(nil), meta.Suggestions...)
	}
	if meta.RenderHint != nil {
		renderHint := *meta.RenderHint
		copyMeta.RenderHint = &renderHint
	}
	copyMeta.PatchPresentation = patchformat.ClonePresentation(meta.PatchPresentation)
	return &copyMeta
}

func formatPersistedToolCall(call llm.ToolCall) ChatEntry {
	meta := persistedTranscriptToolCallMeta(call)
	text := "tool call"
	if meta != nil {
		text = strings.TrimSpace(meta.Command)
	}
	if text == "" {
		text = "tool call"
	}
	return ChatEntry{
		Visibility: transcript.EntryVisibilityOngoingCollapsed,
		Role:       "tool_call",
		Text:       text,
		ToolCallID: strings.TrimSpace(call.ID),
		ToolCall:   meta,
	}
}

func persistedTranscriptToolCallMeta(call llm.ToolCall) *transcript.ToolCallMeta {
	decoded := transcript.DecodeToolCallMeta(call.Presentation)
	switch decoded.Kind {
	case transcript.ToolCallMetaDecodeCurrent, transcript.ToolCallMetaDecodeLegacyNormalized:
		if decoded.Meta == nil {
			panic(fmt.Sprintf(
				"persisted tool call %q metadata decode outcome %d has no metadata",
				call.ID,
				decoded.Kind,
			))
		}
		return decoded.Meta
	case transcript.ToolCallMetaDecodeInvalid:
	case transcript.ToolCallMetaDecodeAbsent:
	default:
		panic(fmt.Sprintf(
			"persisted tool call %q metadata decode returned unknown outcome %d",
			call.ID,
			decoded.Kind,
		))
	}
	return buildToolCallMeta(call, "")
}
