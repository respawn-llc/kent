package runtimeview

import (
	"fmt"

	"core/server/llm"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/transcript"
)

func transcriptNoticeMessageType(value llm.MessageType) (transcriptpb.NoticeMessageType, error) {
	types := map[llm.MessageType]transcriptpb.NoticeMessageType{
		llm.MessageTypeAgentsMD:                       transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_AGENTS_MD,
		llm.MessageTypeSkills:                         transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_SKILLS,
		llm.MessageTypeSubagents:                      transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_SUBAGENTS,
		llm.MessageTypeEnvironment:                    transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_ENVIRONMENT,
		llm.MessageTypeCompactionSummary:              transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_SUMMARY,
		llm.MessageTypeInterruption:                   transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_INTERRUPTION,
		llm.MessageTypeErrorFeedback:                  transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_ERROR_FEEDBACK,
		llm.MessageTypeCompactionSoonReminder:         transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_SOON_REMINDER,
		llm.MessageTypeHandoffFutureMessage:           transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_HANDOFF_FUTURE_MESSAGE,
		llm.MessageTypeReviewerFeedback:               transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_REVIEWER_FEEDBACK,
		llm.MessageTypeBackgroundNotice:               transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_BACKGROUND_NOTICE,
		llm.MessageTypeUserShellCommand:               transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_USER_SHELL_COMMAND,
		llm.MessageTypeCustomToolCallOutput:           transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_CUSTOM_TOOL_CALL_OUTPUT,
		llm.MessageTypeCompactionPreservedUserMessage: transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_PRESERVED_USER_MESSAGE,
		llm.MessageTypeHeadlessMode:                   transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_HEADLESS_MODE,
		llm.MessageTypeHeadlessModeExit:               transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_HEADLESS_MODE_EXIT,
		llm.MessageTypeWorkflowMode:                   transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKFLOW_MODE,
		llm.MessageTypeWorkflowModeExit:               transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKFLOW_MODE_EXIT,
		llm.MessageTypeWorktreeMode:                   transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKTREE_MODE,
		llm.MessageTypeWorktreeModeExit:               transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKTREE_MODE_EXIT,
		llm.MessageTypeSessionRebind:                  transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_SESSION_REBIND,
		llm.MessageTypeGoal:                           transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_GOAL,
		llm.MessageTypeActiveGoalContinuation:         transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_ACTIVE_GOAL_CONTINUATION,
		llm.MessageTypeAgentSteer:                     transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_AGENT_STEER,
	}
	result, ok := types[value]
	if !ok {
		return transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_UNSPECIFIED, fmt.Errorf("invalid notice message type %q", value)
	}
	return result, nil
}

func transcriptVisibility(value transcript.EntryVisibility) (transcriptpb.EntryVisibility, error) {
	switch transcript.NormalizeEntryVisibility(value) {
	case transcript.EntryVisibilityOngoing:
		return transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING, nil
	case transcript.EntryVisibilityOngoingCollapsed:
		return transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED, nil
	case transcript.EntryVisibilityDetail:
		return transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL, nil
	case transcript.EntryVisibilityHidden:
		return transcriptpb.EntryVisibility_ENTRY_VISIBILITY_HIDDEN, nil
	default:
		return transcriptpb.EntryVisibility_ENTRY_VISIBILITY_UNSPECIFIED, fmt.Errorf("invalid projected transcript visibility %q", value)
	}
}

func transcriptIntegrity(value transcript.RowIntegrity) (transcriptpb.RowIntegrity, error) {
	switch value {
	case transcript.RowIntegrityValid:
		return transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID, nil
	case transcript.RowIntegrityRecoverableMalformed:
		return transcriptpb.RowIntegrity_ROW_INTEGRITY_RECOVERABLE_MALFORMED, nil
	case transcript.RowIntegrityUnrecoverableMalformed:
		return transcriptpb.RowIntegrity_ROW_INTEGRITY_UNRECOVERABLE_MALFORMED, nil
	default:
		return transcriptpb.RowIntegrity_ROW_INTEGRITY_UNSPECIFIED, fmt.Errorf("invalid transcript integrity %d", value)
	}
}

func transcriptAssistantPhase(value transcript.AssistantPhase) (transcriptpb.AssistantPhase, error) {
	switch transcript.ClassifyAssistantPhase(string(value)) {
	case transcript.AssistantPhaseCommentary:
		return transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY, nil
	case transcript.AssistantPhaseFinal:
		return transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL, nil
	default:
		return transcriptpb.AssistantPhase_ASSISTANT_PHASE_UNSPECIFIED, fmt.Errorf("invalid assistant phase %q", value)
	}
}
