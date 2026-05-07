package serverapi

import (
	"context"
	"errors"
	"strings"
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
	Action                   SessionTransitionAction `json:"action"`
	InitialPrompt            string                  `json:"initial_prompt,omitempty"`
	InitialInput             string                  `json:"initial_input,omitempty"`
	TargetSessionID          string                  `json:"target_session_id,omitempty"`
	ForkUserMessageIndex     int                     `json:"fork_user_message_index,omitempty"`
	ForkTranscriptEntryIndex *int                    `json:"fork_transcript_entry_index,omitempty"`
	ParentSessionID          string                  `json:"parent_session_id,omitempty"`
}

type SessionInitialInputRequest struct {
	SessionID       string `json:"session_id,omitempty"`
	TransitionInput string `json:"transition_input,omitempty"`
}

type SessionInitialInputResponse struct {
	Input string `json:"input"`
}

type SessionPersistInputDraftRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Input             string `json:"input,omitempty"`
}

type SessionPersistInputDraftResponse struct{}

type SessionRetargetWorkspaceRequest struct {
	ClientRequestID string `json:"client_request_id"`
	SessionID       string `json:"session_id"`
	WorkspaceRoot   string `json:"workspace_root"`
}

type SessionRetargetWorkspaceResponse struct {
	Binding ProjectBinding `json:"binding"`
}

type SessionResolveTransitionRequest struct {
	ClientRequestID   string            `json:"client_request_id"`
	SessionID         string            `json:"session_id,omitempty"`
	ControllerLeaseID string            `json:"controller_lease_id,omitempty"`
	Transition        SessionTransition `json:"transition"`
}

type SessionResolveTransitionResponse struct {
	NextSessionID   string `json:"next_session_id,omitempty"`
	InitialPrompt   string `json:"initial_prompt,omitempty"`
	InitialInput    string `json:"initial_input,omitempty"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	ForceNewSession bool   `json:"force_new_session,omitempty"`
	ShouldContinue  bool   `json:"should_continue,omitempty"`
	RequiresReauth  bool   `json:"requires_reauth,omitempty"`
}

type SessionLifecycleService interface {
	GetInitialInput(ctx context.Context, req SessionInitialInputRequest) (SessionInitialInputResponse, error)
	PersistInputDraft(ctx context.Context, req SessionPersistInputDraftRequest) (SessionPersistInputDraftResponse, error)
	RetargetSessionWorkspace(ctx context.Context, req SessionRetargetWorkspaceRequest) (SessionRetargetWorkspaceResponse, error)
	ResolveTransition(ctx context.Context, req SessionResolveTransitionRequest) (SessionResolveTransitionResponse, error)
}

func (r SessionPersistInputDraftRequest) Validate() error {
	if strings.TrimSpace(r.ClientRequestID) == "" {
		return errors.New("client_request_id is required")
	}
	if err := validateLifecycleSessionID(r.SessionID); err != nil {
		return err
	}
	return validateControllerLeaseID(r.ControllerLeaseID)
}

func (r SessionInitialInputRequest) Validate() error {
	if strings.TrimSpace(r.SessionID) == "" {
		return nil
	}
	return validateLifecycleSessionID(r.SessionID)
}

func (r SessionRetargetWorkspaceRequest) Validate() error {
	if strings.TrimSpace(r.ClientRequestID) == "" {
		return errors.New("client_request_id is required")
	}
	if err := validateLifecycleSessionID(r.SessionID); err != nil {
		return err
	}
	if strings.TrimSpace(r.WorkspaceRoot) == "" {
		return errors.New("workspace_root is required")
	}
	return nil
}

func (r SessionResolveTransitionRequest) Validate() error {
	if strings.TrimSpace(r.ClientRequestID) == "" {
		return errors.New("client_request_id is required")
	}
	if strings.TrimSpace(r.SessionID) != "" {
		if err := validateLifecycleSessionID(r.SessionID); err != nil {
			return err
		}
		if err := validateControllerLeaseID(r.ControllerLeaseID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(string(r.Transition.Action)) == "" {
		return errors.New("transition.action is required")
	}
	return nil
}

func validateLifecycleSessionID(sessionID string) error {
	return validateScopedSessionID(sessionID)
}
