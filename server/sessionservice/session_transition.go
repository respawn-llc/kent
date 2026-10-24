package sessionservice

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"core/server/launch"
	"core/server/session"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
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
	Store      *session.Store
	Transition sessionTransition
	ForkTarget launch.InteractiveForkTarget
}

func sessionTransitionFromProto(value *sessionlaunchpb.SessionTransition) sessionTransition {
	transition := sessionTransition{
		Action: value.Action, InitialPrompt: value.InitialPrompt,
		InitialPromptHistoryRecorded: value.InitialPromptHistoryRecorded,
		InitialInput:                 value.InitialInput, TargetSessionID: value.GetTargetSessionId(),
	}
	if value.PreviousSessionId != nil {
		id := runtimeids.SessionIDFromProto(*value.PreviousSessionId)
		transition.PreviousSessionID = &id
	}
	return transition
}

func initialSessionInput(meta session.Meta, transitionInput string) string {
	if draft := meta.InputDraft; draft != "" {
		return draft
	}
	return transitionInput
}

func resolveSessionTransition(_ context.Context, req sessionTransitionResolveRequest) (*sessionlaunchpb.SessionDirective, error) {
	switch req.Transition.Action {
	case sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_NEW_SESSION:
		prompt := sessionInitialPromptMetadata(req.Transition.InitialPrompt, req.Transition.InitialPromptHistoryRecorded)
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
		targetID := runtimeids.SessionIDFromProto(req.Transition.TargetSessionID)
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
	parentMeta := req.Store.Meta()
	baseName := parentMeta.SessionID
	if parentMeta.Name != nil {
		baseName = *parentMeta.Name
	}
	eventLog, err := req.Store.MaterializeEventLog()
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	forkedStore, forkOrdinal, err := session.ForkAtUserMessage(eventLog, req.Transition.ForkUserMessageSeq, baseName, sessioncontract.SessionCategoryMain, req.ForkTarget.Thinking)
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	if err := forkedStore.SetConnectionID(req.ForkTarget.ConnectionID); err != nil {
		return &sessionlaunchpb.SessionDirective{}, errors.Join(err, forkedStore.RemoveDurable())
	}
	if err := forkedStore.SetName(textutil.Value(strings.TrimSpace(baseName + " \u2192 edit u" + strconv.Itoa(forkOrdinal)))); err != nil {
		return &sessionlaunchpb.SessionDirective{}, errors.Join(err, forkedStore.RemoveDurable())
	}
	forkID := runtimeids.SessionIDFromProto(forkedStore.Meta().SessionID)
	prompt := sessionInitialPromptMetadata(req.Transition.InitialPrompt, req.Transition.InitialPromptHistoryRecorded)
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

func sessionInitialPromptMetadata(text string, historyRecorded bool) *sessionlaunchpb.SessionInitialPromptMetadata {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	prompt := &sessionlaunchpb.SessionInitialPromptMetadata{Text: text, HistoryRecorded: historyRecorded}
	return prompt
}
