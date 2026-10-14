package serverapi

import (
	"errors"
	"fmt"
	"strings"

	"core/shared/config"
	"core/shared/runtimeids"
)

type SessionRuntimeActivateRequest struct {
	SessionID                runtimeids.SessionSelector
	OwnerID                  string
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

type SessionRuntimeAttachment struct {
	SessionID  string
	Generation uint64
}

type SessionRuntimeReleaseRequest struct {
	Attachment  SessionRuntimeAttachment
	DropOwner   bool
	ClosePolicy SessionRuntimeReleaseClosePolicy
	OwnerID     string
}

type SessionRuntimeReleaseClosePolicy string

const (
	SessionRuntimeReleaseClosePolicyCloseIfIdle SessionRuntimeReleaseClosePolicy = "close_if_idle"
	SessionRuntimeReleaseClosePolicyDetachOnly  SessionRuntimeReleaseClosePolicy = "detach_only"
)

func (r SessionRuntimeActivateRequest) Validate() error {
	if r.ExplicitToolSelection != nil {
		if err := r.ExplicitToolSelection.Validate(); err != nil {
			return err
		}
	}
	if r.SessionID == nil {
		return errors.New("Session selector is required")
	}
	if _, err := runtimeids.ParseSessionSelector(r.SessionID.String()); err != nil {
		return err
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

func (r SessionRuntimeAttachment) ValidateForSession(sessionID string) error {
	expected := strings.TrimSpace(sessionID)
	if r.SessionID != expected {
		return fmt.Errorf(
			"session runtime activation returned attachment for session %q, want %q",
			r.SessionID,
			expected,
		)
	}
	return nil
}

func (r SessionRuntimeReleaseRequest) Validate() error {
	switch r.ClosePolicy {
	case "", SessionRuntimeReleaseClosePolicyCloseIfIdle:
	case SessionRuntimeReleaseClosePolicyDetachOnly:
		if !r.DropOwner {
			return errors.New("detach_only release requires drop_owner")
		}
	default:
		return errors.New("invalid session runtime release close_policy")
	}
	return nil
}

func (r SessionRuntimeReleaseRequest) EffectiveClosePolicy() SessionRuntimeReleaseClosePolicy {
	return r.ClosePolicy
}
