package worktreeui

import (
	"errors"
	"strings"

	"core/shared/serverapi"
)

type State struct {
	ErrorText     string
	Resolving     bool
	SubmitPending bool
	Token         uint64
	Resolution    serverapi.WorktreeCreateTargetResolution
}

type ScheduleOutcome struct {
	Token    uint64
	Debounce bool
}

func Schedule(state State, query string) (State, ScheduleOutcome) {
	trimmedQuery := strings.TrimSpace(query)
	state.ErrorText = ""
	state.Token++
	state.Resolution = serverapi.WorktreeCreateTargetResolution{}
	state.Resolving = trimmedQuery != ""
	state.SubmitPending = false
	return state, ScheduleOutcome{Token: state.Token, Debounce: trimmedQuery != ""}
}

type BeginSubmitOutcome struct {
	Token uint64
	Query string
}

func BeginSubmit(state State, query string) (State, BeginSubmitOutcome, error) {
	if strings.TrimSpace(query) == "" {
		err := errors.New("Branch or ref is required")
		state.ErrorText = err.Error()
		return state, BeginSubmitOutcome{}, err
	}
	state.ErrorText = ""
	state.Resolution = serverapi.WorktreeCreateTargetResolution{}
	state.Resolving = true
	state.SubmitPending = true
	state.Token++
	trimmedQuery := strings.TrimSpace(query)
	return state, BeginSubmitOutcome{Token: state.Token, Query: trimmedQuery}, nil
}

type DebounceOutcome struct {
	Ignored bool
	Start   bool
	Token   uint64
	Query   string
}

func DebounceReady(state State, token uint64, query string) (State, DebounceOutcome) {
	if token != state.Token {
		return state, DebounceOutcome{Ignored: true}
	}
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		state.Resolving = false
		state.SubmitPending = false
		state.Resolution = serverapi.WorktreeCreateTargetResolution{}
		state.ErrorText = ""
		return state, DebounceOutcome{}
	}
	return state, DebounceOutcome{Start: true, Token: token, Query: trimmedQuery}
}

type DoneInput struct {
	Token         uint64
	CurrentQuery  string
	ResponseQuery string
	Resolution    serverapi.WorktreeCreateTargetResolution
	HasError      bool
	ErrorText     string
}

type DoneOutcome struct {
	Ignored    bool
	Submit     bool
	SubmitKind serverapi.WorktreeCreateTargetResolutionKind
}

func Done(state State, input DoneInput) (State, DoneOutcome) {
	if input.Token != state.Token {
		return state, DoneOutcome{Ignored: true}
	}
	if strings.TrimSpace(input.CurrentQuery) != strings.TrimSpace(input.ResponseQuery) {
		return state, DoneOutcome{Ignored: true}
	}
	state.Resolving = false
	submitPending := state.SubmitPending
	state.SubmitPending = false
	if input.HasError {
		state.Resolution = serverapi.WorktreeCreateTargetResolution{}
		state.ErrorText = input.ErrorText
		return state, DoneOutcome{}
	}
	state.ErrorText = ""
	state.Resolution = input.Resolution
	if submitPending {
		return state, DoneOutcome{Submit: true, SubmitKind: input.Resolution.Kind}
	}
	return state, DoneOutcome{}
}
