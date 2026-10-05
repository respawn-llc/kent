package app

import (
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *uiModel) applySessionSettingFeedback(feedback *transcriptpb.SessionSettingFeedback) tea.Cmd {
	if !feedback.Changed {
		return nil
	}
	return m.sendTransientStatusWithNoticeID(
		sessionSettingFeedbackNotice(feedback),
		uiStatusNoticeSuccess,
		transientStatusDuration,
		uiStatusNoticeReplace,
		sessionSettingNoticeID(feedback.Kind),
	)
}

func sessionSettingFeedbackNotice(feedback *transcriptpb.SessionSettingFeedback) string {
	switch feedback.Kind {
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_SESSION_NAME:
		if feedback.GetSessionName() == "" {
			return "Session name reset"
		}
		return "Session name: " + feedback.GetSessionName()
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_THINKING:
		return "Thinking: " + feedback.GetThinking()
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_FAST_MODE:
		return "Fast: " + chatSettingsOnOffValues[feedback.GetFastMode()]
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_SUPERVISOR:
		var supervisor chatsettingspb.SupervisorValue
		switch feedback.GetSupervisor() {
		case "off":
			supervisor = chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_OFF
		case "edits":
			supervisor = chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_AFTER_EDITS
		case "all":
			supervisor = chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_ALWAYS
		default:
			panic("invalid Session supervisor setting")
		}
		return "Supervisor: " + chatSettingsSupervisorNotices[supervisor]
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_QUESTIONS:
		return "Questions: " + chatSettingsOnOffValues[feedback.GetQuestions()]
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_AUTO_COMPACTION:
		return "Auto-compaction: " + chatSettingsOnOffValues[feedback.GetAutoCompaction()]
	default:
		panic("validated Session setting feedback kind is exhaustive")
	}
}

func sessionSettingNoticeID(kind transcriptpb.SessionSettingKind) string {
	switch kind {
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_SESSION_NAME:
		return "session-setting:session_name"
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_THINKING:
		return "session-setting:thinking"
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_FAST_MODE:
		return "session-setting:fast_mode"
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_SUPERVISOR:
		return "session-setting:supervisor"
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_QUESTIONS:
		return "session-setting:questions"
	case transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_AUTO_COMPACTION:
		return "session-setting:auto_compaction"
	default:
		panic("invalid Session setting kind")
	}
}
