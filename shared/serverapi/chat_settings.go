package serverapi

import (
	"errors"
	"fmt"
	"strings"

	"core/shared/runtimeids"
)

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
