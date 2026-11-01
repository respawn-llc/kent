package serverapi

import (
	"errors"

	"core/shared/config"
	"core/shared/runtimeids"
)

type SessionRuntimeActivateRequest struct {
	SessionID runtimeids.SessionSelector
	SessionRuntimePreparationOptions
}

type SessionRuntimePreparationOptions struct {
	ConnectionScope          config.ConnectionScope
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
	return r.SessionRuntimePreparationOptions.Validate()
}

func (r SessionRuntimePreparationOptions) Validate() error {
	if r.AgentSelection != nil {
		if r.AgentSelection.AgentRole != nil && config.NormalizeSubagentSelector(*r.AgentSelection.AgentRole) == "" {
			return errors.New("agent_selection.agent_role must be a valid role when present")
		}
	}
	return nil
}
