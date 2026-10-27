package serverapi

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"core/shared/config"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
)

type RunSelectionRejectedError struct {
	Reason chatsettingspb.MutationRejectionReason
}

func (*RunSelectionRejectedError) Error() string {
	return "Run selection rejected"
}

type RunPromptRequest struct {
	Intent          SessionLaunchIntent `json:"intent"`
	CallerSessionID *string             `json:"caller_session_id,omitempty"`
	Prompt          string              `json:"prompt"`
	Timeout         time.Duration       `json:"timeout"`
	Overrides       RunPromptOverrides  `json:"overrides,omitempty"`
}

type RunPromptOverrides struct {
	AgentRole           *string `json:"agent_role,omitempty"`
	Model               string  `json:"model"`
	ThinkingLevel       string  `json:"thinking_level"`
	Theme               string  `json:"theme"`
	ModelTimeoutSeconds int     `json:"model_timeout_seconds"`
	Tools               string  `json:"tools"`
}

var ErrInvalidRunPromptAgentRole = errors.New("invalid agent role")

type RunPromptAgentRoleOverride struct {
	Present bool
	Default bool
	Role    string
}

func (o RunPromptOverrides) AgentRoleOverride() (RunPromptAgentRoleOverride, error) {
	if o.AgentRole == nil {
		return RunPromptAgentRoleOverride{}, nil
	}
	raw := strings.TrimSpace(*o.AgentRole)
	if raw == "" {
		return RunPromptAgentRoleOverride{}, fmt.Errorf("%w %s", ErrInvalidRunPromptAgentRole, strconv.Quote(*o.AgentRole))
	}
	normalized := strings.ToLower(raw)
	if normalized == config.DefaultSubagentRole {
		return RunPromptAgentRoleOverride{Present: true, Default: true}, nil
	}
	if config.IsReservedSubagentRoleName(normalized) {
		return RunPromptAgentRoleOverride{}, fmt.Errorf("%w %s", ErrInvalidRunPromptAgentRole, strconv.Quote(raw))
	}
	roleName := config.NormalizeSubagentSelector(raw)
	if roleName == "" {
		return RunPromptAgentRoleOverride{}, fmt.Errorf("%w %s", ErrInvalidRunPromptAgentRole, strconv.Quote(raw))
	}
	return RunPromptAgentRoleOverride{Present: true, Role: roleName}, nil
}

func (o RunPromptOverrides) ValidateAgentRoleOverride() error {
	_, err := o.AgentRoleOverride()
	return err
}

func (o RunPromptOverrides) HasAny() bool {
	return o.AgentRole != nil ||
		strings.TrimSpace(o.Model) != "" ||
		strings.TrimSpace(o.ThinkingLevel) != "" ||
		strings.TrimSpace(o.Theme) != "" ||
		o.ModelTimeoutSeconds > 0 ||
		strings.TrimSpace(o.Tools) != ""
}

func (o RunPromptOverrides) HasConfigOverrides() bool {
	return strings.TrimSpace(o.Model) != "" ||
		strings.TrimSpace(o.ThinkingLevel) != "" ||
		strings.TrimSpace(o.Theme) != "" ||
		o.ModelTimeoutSeconds > 0 ||
		strings.TrimSpace(o.Tools) != ""
}

type RunPromptProgressSink interface {
	PublishRunPromptProgress(*runpromptpb.ProgressEvent)
}

type RunPromptProgressFunc func(*runpromptpb.ProgressEvent)

func (fn RunPromptProgressFunc) PublishRunPromptProgress(progress *runpromptpb.ProgressEvent) {
	if fn != nil {
		fn(progress)
	}
}
