package serverapi

import (
	"errors"
	"strings"

	"core/shared/runtimeids"
)

type SessionTransitionAction string

const (
	SessionTransitionActionNone         SessionTransitionAction = "none"
	SessionTransitionActionNewSession   SessionTransitionAction = "new_session"
	SessionTransitionActionResume       SessionTransitionAction = "resume"
	SessionTransitionActionLogout       SessionTransitionAction = "logout"
	SessionTransitionActionForkRollback SessionTransitionAction = "fork_rollback"
	SessionTransitionActionOpenSession  SessionTransitionAction = "open_session"
)

type SessionTransition struct {
	Action                       SessionTransitionAction `json:"action"`
	InitialPrompt                string                  `json:"initial_prompt,omitempty"`
	InitialPromptHistoryRecorded bool                    `json:"initial_prompt_history_recorded,omitempty"`
	InitialInput                 *string                 `json:"initial_input,omitempty"`
	TargetSessionID              string                  `json:"target_session_id,omitempty"`
	ForkRollbackTargetID         string                  `json:"fork_rollback_target_id,omitempty"`
	PreviousSessionID            *runtimeids.SessionID   `json:"previous_session_id,omitempty"`
}

type SessionResolveTransitionRequest struct {
	SessionID  string            `json:"session_id,omitempty"`
	Transition SessionTransition `json:"transition"`
}

type SessionResolveTransitionResponse = SessionDirective

func (r SessionResolveTransitionRequest) Validate() error {
	if strings.TrimSpace(r.SessionID) != "" {
		if err := validateScopedSessionID(r.SessionID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(string(r.Transition.Action)) == "" {
		return errors.New("transition.action is required")
	}
	return nil
}
