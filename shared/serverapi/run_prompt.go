package serverapi

import (
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

type RunPromptOverrides struct {
	AgentRole           string
	AgentRoleSet        bool
	Model               string
	ProviderOverride    string
	ThinkingLevel       string
	Theme               string
	ModelTimeoutSeconds int
	Tools               string
	OpenAIBaseURL       string
}

func (o RunPromptOverrides) HasAny() bool {
	return o.AgentRoleSet ||
		strings.TrimSpace(o.AgentRole) != "" ||
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
	return config.NormalizeSubagentSelector(o.AgentRole) != ""
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
