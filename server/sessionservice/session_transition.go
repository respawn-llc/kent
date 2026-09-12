package sessionservice

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"core/server/session"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"google.golang.org/protobuf/types/known/emptypb"
)

type sessionTransition struct {
	Action                       sessionlaunchpb.SessionTransitionAction
	InitialPrompt                string
	InitialPromptHistoryRecorded bool
	InitialInput                 *string
	TargetSessionID              string
	ForkUserMessageSeq           int64
	PreviousSessionID            *runtimeids.SessionID
}

type sessionTransitionResolveRequest struct {
	Store        *session.Store
	Transition   sessionTransition
	ForkThinking session.ForkThinking
}

func sessionTransitionFromProto(value *sessionlaunchpb.SessionTransition) (sessionTransition, error) {
	transition := sessionTransition{
		Action: value.Action, InitialPrompt: value.InitialPrompt,
		InitialPromptHistoryRecorded: value.InitialPromptHistoryRecorded,
		InitialInput:                 value.InitialInput, TargetSessionID: value.GetTargetSessionId(),
	}
	if value.PreviousSessionId != nil {
		id, err := runtimeids.ParseSessionID(*value.PreviousSessionId)
		if err != nil {
			return sessionTransition{}, err
		}
		transition.PreviousSessionID = &id
	}
	return transition, nil
}

func initialSessionInput(meta session.Meta, transitionInput string) string {
	if draft := meta.InputDraft; draft != "" {
		return draft
	}
	return transitionInput
}

func persistSessionInputDraft(store *session.Store, input string) error {
	if store == nil {
		return nil
	}
	return store.SetInputDraft(input)
}

func resolveSessionTransition(_ context.Context, req sessionTransitionResolveRequest) (*sessionlaunchpb.SessionDirective, error) {
	switch req.Transition.Action {
	case sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_NEW_SESSION:
		prompt, err := sessionInitialPromptMetadata(req.Transition.InitialPrompt, req.Transition.InitialPromptHistoryRecorded)
		if err != nil {
			return &sessionlaunchpb.SessionDirective{}, err
		}
		origin := serverapi.IndependentSessionCreateOrigin()
		if req.Transition.PreviousSessionID != nil {
			origin = serverapi.PreviousSessionCreateOrigin(*req.Transition.PreviousSessionID)
		}
		return protoapi.SessionLaunchDirectiveToProto(
			serverapi.CreateNewSessionLaunchIntent(origin),
			sessionLaunchPreparation(prompt, nil, sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH),
		)
	case sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_RESUME:
		return &sessionlaunchpb.SessionDirective{Directive: &sessionlaunchpb.SessionDirective_SelectSession{
			SelectSession: &sessionlaunchpb.SessionSelectDirective{Auth: sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH},
		}}, nil
	case sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_OPEN_SESSION:
		targetID, err := runtimeids.ParseSessionID(strings.TrimSpace(req.Transition.TargetSessionID))
		if err != nil {
			return &sessionlaunchpb.SessionDirective{}, err
		}
		return protoapi.SessionLaunchDirectiveToProto(
			serverapi.OpenExistingSessionLaunchIntent(targetID),
			sessionLaunchPreparation(nil, req.Transition.InitialInput, sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH),
		)
	case sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK:
		return resolveForkRollback(req)
	default:
		return &sessionlaunchpb.SessionDirective{Directive: &sessionlaunchpb.SessionDirective_Stop{Stop: &emptypb.Empty{}}}, nil
	}
}

func resolveForkRollback(req sessionTransitionResolveRequest) (*sessionlaunchpb.SessionDirective, error) {
	if req.Store == nil {
		return &sessionlaunchpb.SessionDirective{}, errors.New("current store is required for rollback fork")
	}
	if req.Transition.ForkUserMessageSeq <= 0 {
		return &sessionlaunchpb.SessionDirective{}, errors.New("rollback fork user message seq must be > 0")
	}
	parentMeta := req.Store.Meta()
	baseName := strings.TrimSpace(parentMeta.Name)
	if baseName == "" {
		baseName = parentMeta.SessionID
	}
	eventLog, err := req.Store.MaterializeEventLog()
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	forkedStore, forkOrdinal, err := session.ForkAtUserMessage(eventLog, req.Transition.ForkUserMessageSeq, baseName, sessioncontract.SessionCategoryMain, req.ForkThinking)
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	if err := forkedStore.SetName(strings.TrimSpace(baseName + " \u2192 edit u" + strconv.Itoa(forkOrdinal))); err != nil {
		return &sessionlaunchpb.SessionDirective{}, errors.Join(err, forkedStore.RemoveDurable())
	}
	forkID, err := runtimeids.ParseSessionID(forkedStore.Meta().SessionID)
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, errors.Join(err, forkedStore.RemoveDurable())
	}
	prompt, err := sessionInitialPromptMetadata(req.Transition.InitialPrompt, req.Transition.InitialPromptHistoryRecorded)
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, errors.Join(err, forkedStore.RemoveDurable())
	}
	return protoapi.SessionLaunchDirectiveToProto(
		serverapi.OpenExistingSessionLaunchIntent(forkID),
		sessionLaunchPreparation(prompt, req.Transition.InitialInput, sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH),
	)
}

func sessionLaunchPreparation(prompt *sessionlaunchpb.SessionInitialPromptMetadata, input *string, auth sessionlaunchpb.SessionAuthPreparation) *sessionlaunchpb.SessionLaunchPreparation {
	policy := &sessionlaunchpb.SessionDraftDisposition{Disposition: &sessionlaunchpb.SessionDraftDisposition_RestoreStoredDraft{RestoreStoredDraft: &emptypb.Empty{}}}
	if input != nil {
		policy.Disposition = &sessionlaunchpb.SessionDraftDisposition_OverrideStoredDraft{OverrideStoredDraft: *input}
	}
	return &sessionlaunchpb.SessionLaunchPreparation{InitialPrompt: prompt, InputPolicy: policy, Auth: auth}
}

func sessionInitialPromptMetadata(text string, historyRecorded bool) (*sessionlaunchpb.SessionInitialPromptMetadata, error) {
	if strings.TrimSpace(text) == "" {
		if historyRecorded {
			return nil, errors.New("initial prompt history cannot be recorded without an initial prompt")
		}
		return nil, nil
	}
	prompt := &sessionlaunchpb.SessionInitialPromptMetadata{Text: text, HistoryRecorded: historyRecorded}
	return prompt, nil
}
