package sessionruntime

import (
	"errors"
	"strings"

	"core/server/launch"
	"core/server/session"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func ActivationRequestFromSessionPlan(
	plan launch.SessionPlan,
	ownerID string,
) (serverapi.SessionRuntimeActivateRequest, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return serverapi.SessionRuntimeActivateRequest{}, errors.New("runtime owner id is required")
	}
	agentSelection, err := AgentSelectionFromState(plan.ActivationAgentSelection)
	if err != nil {
		return serverapi.SessionRuntimeActivateRequest{}, err
	}
	request := serverapi.SessionRuntimeActivateRequest{
		SessionID:                plan.Descriptor.SessionID().String(),
		OwnerID:                  ownerID,
		ActiveSettings:           plan.ActiveSettings,
		EnabledToolIDs:           toolspec.IDStrings(plan.EnabledTools),
		QuestionsEnabled:         textutil.Value(plan.QuestionsEnabled),
		AutoCompactionEnabled:    textutil.Value(plan.AutoCompactionEnabled),
		ThinkingOverrideExplicit: plan.ThinkingOverrideExplicit,
		AgentSelection:           agentSelection,
		Source:                   plan.Source,
	}
	if err := request.Validate(); err != nil {
		return serverapi.SessionRuntimeActivateRequest{}, err
	}
	return request, nil
}

func AgentSelectionFromState(state *session.ChatSettingsState) (*serverapi.SessionRuntimeAgentSelection, error) {
	if state != nil {
		settings := state.Settings
		if settings == nil ||
			settings.Supervisor == nil ||
			settings.Thinking == nil ||
			settings.Fast == nil ||
			settings.Questions == nil ||
			settings.AutoCompaction == nil {
			return nil, errors.New("complete Runtime Agent selection is required")
		}
		return &serverapi.SessionRuntimeAgentSelection{
			Agent: state.Agent,
			Baseline: serverapi.SessionRuntimeChatSettings{
				Supervisor:     *settings.Supervisor,
				Thinking:       *settings.Thinking,
				Fast:           *settings.Fast,
				Questions:      *settings.Questions,
				AutoCompaction: *settings.AutoCompaction,
			},
		}, nil
	}
	return nil, nil
}
