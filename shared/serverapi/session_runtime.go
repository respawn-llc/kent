package serverapi

import (
	"errors"
	"strings"

	"core/shared/config"
)

type SessionRuntimeActivateRequest struct {
	ClientRequestID string              `json:"client_request_id"`
	SessionID       string              `json:"session_id"`
	OwnerID         string              `json:"owner_id,omitempty"`
	ActiveSettings  config.Settings     `json:"active_settings"`
	EnabledToolIDs  []string            `json:"enabled_tool_ids"`
	Source          config.SourceReport `json:"source"`
}

type SessionRuntimeActivateResponse struct {
	LeaseID           string                    `json:"lease_id"`
	Mode              SessionRuntimeAttachMode  `json:"mode,omitempty"`
	AllowedOperations []SessionRuntimeOperation `json:"allowed_operations,omitempty"`
	ReadOnly          bool                      `json:"read_only,omitempty"`
}

type SessionRuntimeAttachMode string

const (
	SessionRuntimeAttachModeController    SessionRuntimeAttachMode = "controller"
	SessionRuntimeAttachModeCollaborative SessionRuntimeAttachMode = "collaborative"
	SessionRuntimeAttachModeNoControl     SessionRuntimeAttachMode = "no_control"
)

type SessionRuntimeOperation string

const (
	SessionRuntimeOperationSubmitUserTurn           SessionRuntimeOperation = "runtime.submit_user_turn"
	SessionRuntimeOperationQueueUserMessage         SessionRuntimeOperation = "runtime.queue_user_message"
	SessionRuntimeOperationSubmitQueuedUserMessages SessionRuntimeOperation = "runtime.submit_queued_user_messages"
	SessionRuntimeOperationDiscardQueuedUserMessage SessionRuntimeOperation = "runtime.discard_queued_user_message"
	SessionRuntimeOperationRecordPromptHistory      SessionRuntimeOperation = "runtime.record_prompt_history"
	SessionRuntimeOperationPromptAnswer             SessionRuntimeOperation = "prompt.answer"
	SessionRuntimeOperationSettingsSessionName      SessionRuntimeOperation = "settings.session_name"
	SessionRuntimeOperationSettingsThinkingLevel    SessionRuntimeOperation = "settings.thinking_level"
	SessionRuntimeOperationSettingsFastMode         SessionRuntimeOperation = "settings.fast_mode"
	SessionRuntimeOperationSettingsQuestions        SessionRuntimeOperation = "settings.questions"
	SessionRuntimeOperationSettingsAutoCompaction   SessionRuntimeOperation = "settings.auto_compaction"
	SessionRuntimeOperationCompactManual            SessionRuntimeOperation = "runtime.compact_manual"
	SessionRuntimeOperationCompactPreSubmit         SessionRuntimeOperation = "runtime.compact_pre_submit"
	SessionRuntimeOperationWorktreeManage           SessionRuntimeOperation = "worktree.manage"
	SessionRuntimeOperationProcessView              SessionRuntimeOperation = "process.view"
	SessionRuntimeOperationGoalManage               SessionRuntimeOperation = "goal.manage"
)

// CollaborativeSessionRuntimeOperations is the set of controls a limited-control attach may
// drive on an active session runtime owned by a run. Workflow-task sessions steer as usual
// (issue #364): goal control is included for every limited-control attach. The only workflow
// limit lives elsewhere — the model cannot submit a structured-output final answer that is
// invalid for the node — which is not a runtime operation and so is not gated here.
func CollaborativeSessionRuntimeOperations() []SessionRuntimeOperation {
	return []SessionRuntimeOperation{
		SessionRuntimeOperationSubmitUserTurn,
		SessionRuntimeOperationQueueUserMessage,
		SessionRuntimeOperationSubmitQueuedUserMessages,
		SessionRuntimeOperationDiscardQueuedUserMessage,
		SessionRuntimeOperationRecordPromptHistory,
		SessionRuntimeOperationPromptAnswer,
		SessionRuntimeOperationSettingsSessionName,
		SessionRuntimeOperationSettingsThinkingLevel,
		SessionRuntimeOperationSettingsFastMode,
		SessionRuntimeOperationSettingsQuestions,
		SessionRuntimeOperationSettingsAutoCompaction,
		SessionRuntimeOperationCompactManual,
		SessionRuntimeOperationCompactPreSubmit,
		SessionRuntimeOperationWorktreeManage,
		SessionRuntimeOperationProcessView,
		SessionRuntimeOperationGoalManage,
	}
}

type SessionRuntimeReleaseRequest struct {
	ClientRequestID string `json:"client_request_id"`
	SessionID       string `json:"session_id"`
	LeaseID         string `json:"lease_id"`
	OnlyIfIdle      bool   `json:"only_if_idle,omitempty"`
	DropOwner       bool   `json:"drop_owner,omitempty"`
	OwnerID         string `json:"owner_id,omitempty"`
}

type SessionRuntimeReleaseResponse struct {
	Released bool `json:"released"`
	Active   bool `json:"active,omitempty"`
}

func (r SessionRuntimeActivateRequest) Validate() error {
	if strings.TrimSpace(r.ClientRequestID) == "" {
		return errors.New("client_request_id is required")
	}
	if err := validateScopedSessionID(r.SessionID); err != nil {
		return err
	}
	return nil
}

func (r SessionRuntimeReleaseRequest) Validate() error {
	if strings.TrimSpace(r.ClientRequestID) == "" {
		return errors.New("client_request_id is required")
	}
	if err := validateScopedSessionID(r.SessionID); err != nil {
		return err
	}
	if strings.TrimSpace(r.LeaseID) == "" {
		return errors.New("lease_id is required")
	}
	return nil
}
