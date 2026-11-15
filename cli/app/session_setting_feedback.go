package app

import (
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	sessionNameNoticeID    = "session-setting:session_name"
	thinkingNoticeID       = "session-setting:thinking"
	fastModeNoticeID       = "session-setting:fast_mode"
	supervisorNoticeID     = "session-setting:supervisor"
	questionsNoticeID      = "session-setting:questions"
	autoCompactionNoticeID = "session-setting:auto_compaction"
)

func (m *uiModel) applySessionSettingFeedback(feedback *transcriptpb.SessionSettingFeedback) tea.Cmd {
	if !feedback.Changed {
		return nil
	}
	notice, noticeID := sessionSettingFeedbackNotice(feedback)
	return m.sendTransientStatusWithNoticeID(
		notice,
		uiStatusNoticeSuccess,
		transientStatusDuration,
		uiStatusNoticeReplace,
		noticeID,
	)
}

func sessionSettingFeedbackNotice(feedback *transcriptpb.SessionSettingFeedback) (string, string) {
	switch value := feedback.Value.(type) {
	case *transcriptpb.SessionSettingFeedback_SessionName:
		return "Session renamed", sessionNameNoticeID
	case *transcriptpb.SessionSettingFeedback_Thinking:
		return "Thinking: " + value.Thinking, thinkingNoticeID
	case *transcriptpb.SessionSettingFeedback_FastMode:
		return "Fast: " + chatSettingsOnOffValues[value.FastMode], fastModeNoticeID
	case *transcriptpb.SessionSettingFeedback_Supervisor:
		var supervisor chatsettingspb.SupervisorValue
		switch value.Supervisor {
		case "off":
			supervisor = chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_OFF
		case "edits":
			supervisor = chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_AFTER_EDITS
		case "all":
			supervisor = chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_ALWAYS
		default:
			panic("invalid Session supervisor setting")
		}
		return "Supervisor: " + chatSettingsSupervisorNotices[supervisor], supervisorNoticeID
	case *transcriptpb.SessionSettingFeedback_Questions:
		return "Questions: " + chatSettingsOnOffValues[value.Questions], questionsNoticeID
	case *transcriptpb.SessionSettingFeedback_AutoCompaction:
		return "Auto-compaction: " + chatSettingsOnOffValues[value.AutoCompaction], autoCompactionNoticeID
	default:
		panic("validated Session setting feedback value is exhaustive")
	}
}
