package serverapi

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"core/shared/config"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
)

type RunPromptRequest struct {
	Intent          SessionLaunchIntent `json:"intent"`
	CallerSessionID *string             `json:"caller_session_id,omitempty"`
	Prompt          string              `json:"prompt"`
	Timeout         time.Duration       `json:"timeout"`
	Overrides       RunPromptOverrides  `json:"overrides,omitempty"`
}

type OptionalStringKey struct {
	Present bool
	Value   string
}

type RunPromptOverridesKey struct {
	AgentRole           OptionalStringKey
	Model               string
	ThinkingLevel       string
	Theme               string
	ModelTimeoutSeconds int
	Tools               string
}

func (o RunPromptOverrides) CanonicalKey() (RunPromptOverridesKey, error) {
	role, err := o.AgentRoleOverride()
	if err != nil {
		return RunPromptOverridesKey{}, err
	}
	key := RunPromptOverridesKey{
		Model:               strings.TrimSpace(o.Model),
		ThinkingLevel:       strings.TrimSpace(o.ThinkingLevel),
		Theme:               strings.TrimSpace(o.Theme),
		ModelTimeoutSeconds: o.ModelTimeoutSeconds,
		Tools:               strings.TrimSpace(o.Tools),
	}
	if role.Present {
		value := role.Role
		if role.Default {
			value = config.DefaultSubagentRole
		}
		key.AgentRole = OptionalStringKey{Present: true, Value: value}
	}
	return key, nil
}

func CanonicalOptionalString(value *string) OptionalStringKey {
	if value == nil {
		return OptionalStringKey{}
	}
	return OptionalStringKey{Present: true, Value: strings.TrimSpace(*value)}
}

func ValidateOptionalIdentifier(field string, value *string) error {
	if value == nil {
		return nil
	}
	if strings.TrimSpace(*value) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	return nil
}

func (r RunPromptRequest) Validate() error {
	if strings.TrimSpace(r.Prompt) == "" {
		return errors.New("prompt is required")
	}
	if err := r.Intent.Validate(); err != nil {
		return fmt.Errorf("intent: %w", err)
	}
	if err := ValidateOptionalIdentifier("caller_session_id", r.CallerSessionID); err != nil {
		return err
	}
	return r.Overrides.ValidateAgentRoleOverride()
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

func (o RunPromptOverrides) HasAgentRoleOverride() bool {
	return o.AgentRole != nil
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
