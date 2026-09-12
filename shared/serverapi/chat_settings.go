package serverapi

import (
	"errors"
	"fmt"
	"strings"

	"core/shared/runtimeids"
)

func validateChatTargetID(field string, value *string) error {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(*value) != *value {
		return fmt.Errorf("%s must not have leading or trailing whitespace", field)
	}
	return nil
}

type ChatSettingsTaskIdentity struct {
	TaskID      string
	TaskShortID string
}

func (i ChatSettingsTaskIdentity) Validate() error {
	if _, err := runtimeids.ParseTaskID(i.TaskID); err != nil {
		return fmt.Errorf("task_id: %w", err)
	}
	if strings.TrimSpace(i.TaskShortID) == "" || strings.TrimSpace(i.TaskShortID) != i.TaskShortID {
		return errors.New("task_short_id is invalid")
	}
	return nil
}

type InitialChatSettings struct {
	AgentRole             string  `json:"agent_role"`
	Supervisor            string  `json:"supervisor"`
	Thinking              *string `json:"thinking,omitempty"`
	Fast                  *bool   `json:"fast,omitempty"`
	QuestionsEnabled      bool    `json:"questions_enabled"`
	AutoCompactionEnabled bool    `json:"auto_compaction_enabled"`
}

func (s InitialChatSettings) Validate() error {
	if err := validateChatTargetID("agent_role", &s.AgentRole); err != nil {
		return err
	}
	switch s.Supervisor {
	case "off", "edits", "all":
	default:
		return fmt.Errorf("initial Chat Supervisor %q is invalid", s.Supervisor)
	}
	if s.Thinking != nil {
		return validateChatTargetID("thinking", s.Thinking)
	}
	return nil
}

type ChatSettingsAgentPreparationCategory string

const (
	ChatSettingsAgentInvalidConfiguration ChatSettingsAgentPreparationCategory = "invalid_configuration"
	ChatSettingsAgentProviderUnavailable  ChatSettingsAgentPreparationCategory = "provider_unavailable"
	ChatSettingsAgentInternalPreparation  ChatSettingsAgentPreparationCategory = "internal_preparation"
)

type ChatSettingsAgentPreparationError struct {
	Agent    string                               `json:"agent"`
	Category ChatSettingsAgentPreparationCategory `json:"category"`
}

func (e *ChatSettingsAgentPreparationError) Error() string {
	return fmt.Sprintf("Chat settings Agent preparation failed: %s (%s)", e.Agent, e.Category)
}

func (e *ChatSettingsAgentPreparationError) Validate() error {
	if e == nil || strings.TrimSpace(e.Agent) == "" || strings.TrimSpace(e.Agent) != e.Agent {
		return errors.New("Chat settings Agent is invalid")
	}
	switch e.Category {
	case ChatSettingsAgentInvalidConfiguration,
		ChatSettingsAgentProviderUnavailable,
		ChatSettingsAgentInternalPreparation:
		return nil
	default:
		return errors.New("Chat settings Agent preparation category is invalid")
	}
}
