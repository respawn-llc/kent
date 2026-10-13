package serverapi

import (
	"errors"
	"strings"

	"core/shared/config"
	"core/shared/runtimeids"
)

type SessionRuntimeActivateRequest struct {
	SessionID runtimeids.SessionSelector
	SessionRuntimePreparationOptions
}

type SessionRuntimePreparationOptions struct {
	ActiveSettings           config.Settings
	EnabledToolIDs           []string
	QuestionsEnabled         *bool
	AutoCompactionEnabled    *bool
	ThinkingOverrideExplicit bool
	AgentSelection           *SessionRuntimeAgentSelection
	ExplicitToolSelection    *config.ToolSelection
	Source                   config.SourceReport
}

type SessionRuntimeAgentSelection struct {
	AgentRole *string
	Baseline  SessionRuntimeChatSettings
}

type SessionRuntimeChatSettings struct {
	Supervisor     string
	Thinking       string
	Fast           bool
	Questions      bool
	AutoCompaction bool
}

func (r SessionRuntimeActivateRequest) Validate() error {
	if r.SessionID == nil {
		return errors.New("Session selector is required")
	}
	if _, err := runtimeids.ParseSessionSelector(r.SessionID.String()); err != nil {
		return err
	}
	return r.SessionRuntimePreparationOptions.Validate()
}

func (r SessionRuntimePreparationOptions) Validate() error {
	if r.ExplicitToolSelection != nil {
		if err := r.ExplicitToolSelection.Validate(); err != nil {
			return err
		}
	}
	if r.QuestionsEnabled == nil {
		return errors.New("questions_enabled is required")
	}
	if r.AutoCompactionEnabled == nil {
		return errors.New("auto_compaction_enabled is required")
	}
	if r.AgentSelection != nil {
		if r.AgentSelection.AgentRole != nil && config.NormalizeSubagentSelector(*r.AgentSelection.AgentRole) == "" {
			return errors.New("agent_selection.agent_role must be a valid role when present")
		}
		if strings.TrimSpace(r.AgentSelection.Baseline.Supervisor) == "" {
			return errors.New("agent_selection.baseline.supervisor is required")
		}
		if strings.TrimSpace(r.AgentSelection.Baseline.Thinking) == "" {
			return errors.New("agent_selection.baseline.thinking is required")
		}
	}
	return nil
}
