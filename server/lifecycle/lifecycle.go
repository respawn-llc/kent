package lifecycle

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"builder/server/session"
)

type Action string

const (
	ActionNone         Action = "none"
	ActionNewSession   Action = "new_session"
	ActionResume       Action = "resume"
	ActionForkRollback Action = "fork_rollback"
	ActionOpenSession  Action = "open_session"
)

type Transition struct {
	Action               Action
	InitialPrompt        string
	InitialInput         string
	TargetSessionID      string
	ForkUserMessageIndex int
	ParentSessionID      string
}

type ResolveRequest struct {
	Store      *session.Store
	Transition Transition
}

type Resolved struct {
	NextSessionID   string
	InitialPrompt   string
	InitialInput    string
	ParentSessionID string
	ForceNewSession bool
	ShouldContinue  bool
}

func InitialInput(store *session.Store, transitionInput string) string {
	if store == nil {
		return transitionInput
	}
	if draft := store.Meta().InputDraft; draft != "" {
		return draft
	}
	return transitionInput
}

func PersistInputDraft(store *session.Store, input string) error {
	if store == nil {
		return nil
	}
	return store.SetInputDraft(input)
}

func Resolve(ctx context.Context, req ResolveRequest) (Resolved, error) {
	switch req.Transition.Action {
	case ActionNewSession:
		return Resolved{
			InitialPrompt:   req.Transition.InitialPrompt,
			ParentSessionID: req.Transition.ParentSessionID,
			ForceNewSession: true,
			ShouldContinue:  true,
		}, nil
	case ActionResume:
		return Resolved{ShouldContinue: true}, nil
	case ActionOpenSession:
		return Resolved{
			NextSessionID:  strings.TrimSpace(req.Transition.TargetSessionID),
			InitialInput:   req.Transition.InitialInput,
			ShouldContinue: true,
		}, nil
	case ActionForkRollback:
		return resolveForkRollback(req)
	default:
		return Resolved{}, nil
	}
}

func resolveForkRollback(req ResolveRequest) (Resolved, error) {
	if req.Store == nil {
		return Resolved{}, errors.New("current store is required for rollback fork")
	}
	if req.Transition.ForkUserMessageIndex <= 0 {
		return Resolved{}, errors.New("rollback fork user message index must be > 0")
	}
	parentMeta := req.Store.Meta()
	baseName := strings.TrimSpace(parentMeta.Name)
	if baseName == "" {
		baseName = parentMeta.SessionID
	}
	forkName := strings.TrimSpace(baseName + " \u2192 edit u" + strconv.Itoa(req.Transition.ForkUserMessageIndex))
	forkedStore, err := session.ForkAtUserMessage(req.Store, req.Transition.ForkUserMessageIndex, forkName)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{
		NextSessionID:  forkedStore.Meta().SessionID,
		InitialPrompt:  req.Transition.InitialPrompt,
		ShouldContinue: true,
	}, nil
}
