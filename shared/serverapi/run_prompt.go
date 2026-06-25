package serverapi

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"core/shared/config"
)

type RunPromptRequest struct {
	ClientRequestID   string
	SelectedSessionID string
	ParentSessionID   string
	Prompt            string
	Timeout           time.Duration
	Overrides         RunPromptOverrides
}

func (r RunPromptRequest) Validate() error {
	if strings.TrimSpace(r.ClientRequestID) == "" {
		return errors.New("client_request_id is required")
	}
	if strings.TrimSpace(r.Prompt) == "" {
		return errors.New("prompt is required")
	}
	return r.Overrides.ValidateAgentRoleOverride()
}

type RunPromptOverrides struct {
	AgentRole           string
	Model               string
	ProviderOverride    string
	ThinkingLevel       string
	Theme               string
	ModelTimeoutSeconds int
	Tools               string
	OpenAIBaseURL       string
}

var ErrInvalidRunPromptAgentRole = errors.New("invalid agent role")

type RunPromptAgentRoleOverride struct {
	Present bool
	Default bool
	Role    string
}

func (o RunPromptOverrides) AgentRoleOverride() (RunPromptAgentRoleOverride, error) {
	raw := strings.TrimSpace(o.AgentRole)
	if raw == "" {
		return RunPromptAgentRoleOverride{}, nil
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

func (o RunPromptOverrides) HasAgentRoleOverride() bool {
	return strings.TrimSpace(o.AgentRole) != ""
}

func (o RunPromptOverrides) HasAny() bool {
	return strings.TrimSpace(o.AgentRole) != "" ||
		strings.TrimSpace(o.Model) != "" ||
		strings.TrimSpace(o.ProviderOverride) != "" ||
		strings.TrimSpace(o.ThinkingLevel) != "" ||
		strings.TrimSpace(o.Theme) != "" ||
		o.ModelTimeoutSeconds > 0 ||
		strings.TrimSpace(o.Tools) != "" ||
		strings.TrimSpace(o.OpenAIBaseURL) != ""
}

func (o RunPromptOverrides) HasConfigOverrides() bool {
	return strings.TrimSpace(o.Model) != "" ||
		strings.TrimSpace(o.ProviderOverride) != "" ||
		strings.TrimSpace(o.ThinkingLevel) != "" ||
		strings.TrimSpace(o.Theme) != "" ||
		o.ModelTimeoutSeconds > 0 ||
		strings.TrimSpace(o.Tools) != "" ||
		strings.TrimSpace(o.OpenAIBaseURL) != ""
}

func (o RunPromptOverrides) NeedsAuthState() bool {
	role, err := o.AgentRoleOverride()
	return err == nil && role.Present && !role.Default
}

type RunPromptResponse struct {
	SessionID   string
	SessionName string
	Result      string
	Duration    time.Duration
	Warnings    []string
}

type RunPromptProgress struct {
	Kind    RunPromptProgressKind
	Message string
}

type RunPromptProgressKind string

const (
	RunPromptProgressKindStatus  RunPromptProgressKind = "status"
	RunPromptProgressKindWarning RunPromptProgressKind = "warning"
)

type RunPromptProgressSink interface {
	PublishRunPromptProgress(RunPromptProgress)
}

type RunPromptProgressFunc func(RunPromptProgress)

func (fn RunPromptProgressFunc) PublishRunPromptProgress(progress RunPromptProgress) {
	if fn != nil {
		fn(progress)
	}
}
