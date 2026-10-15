package protoapi

import (
	"fmt"

	"core/shared/config"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"core/shared/toolspec"
)

func SessionRuntimeActivateToProto(request serverapi.SessionRuntimeActivateRequest) (*sessionlaunchpb.SessionRuntimeActivateRequest, error) {
	options, err := SessionRuntimePreparationToProto(request.SessionRuntimePreparationOptions)
	if err != nil {
		return nil, err
	}
	return &sessionlaunchpb.SessionRuntimeActivateRequest{
		SessionId: SessionSelectorToProto(request.SessionID),
		Options:   options,
	}, nil
}

func SessionRuntimePreparationToProto(request serverapi.SessionRuntimePreparationOptions) (*sessionlaunchpb.SessionRuntimePreparationOptions, error) {
	settings, err := SessionSettingsToProto(request.ActiveSettings)
	if err != nil {
		return nil, err
	}
	source, err := SessionSourceReportToProto(request.Source)
	if err != nil {
		return nil, err
	}
	result := &sessionlaunchpb.SessionRuntimePreparationOptions{
		ConnectionScope: &sessionlaunchpb.ConnectionScope{
			Selection:       ConnectionSelectionToProto(request.ConnectionScope.Selection),
			UnavailableRole: request.ConnectionScope.UnavailableRole,
		},
		ActiveSettings: settings, Source: source,
		QuestionsEnabled: request.QuestionsEnabled, AutoCompactionEnabled: request.AutoCompactionEnabled,
		ThinkingOverrideExplicit: request.ThinkingOverrideExplicit,
		AgentSelection:           SessionRuntimeAgentSelectionToProto(request.AgentSelection),
	}
	result.ExplicitToolSelection, err = ToolSelectionToProto(request.ExplicitToolSelection)
	if err != nil {
		return nil, err
	}
	for _, id := range request.EnabledToolIDs {
		tool, err := SessionToolIDToProto(toolspec.ID(id))
		if err != nil {
			return nil, err
		}
		result.EnabledToolIds = append(result.EnabledToolIds, tool)
	}
	return result, nil
}

func SessionRuntimeActivateFromProto(request *sessionlaunchpb.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateRequest, error) {
	selector, err := SessionSelectorFromProto(request.SessionId)
	if err != nil {
		return serverapi.SessionRuntimeActivateRequest{}, err
	}
	options, err := SessionRuntimePreparationFromProto(request.Options)
	if err != nil {
		return serverapi.SessionRuntimeActivateRequest{}, err
	}
	return serverapi.SessionRuntimeActivateRequest{SessionID: selector, SessionRuntimePreparationOptions: options}, nil
}

func SessionRuntimePreparationFromProto(request *sessionlaunchpb.SessionRuntimePreparationOptions) (serverapi.SessionRuntimePreparationOptions, error) {
	if err := Validate(request); err != nil {
		return serverapi.SessionRuntimePreparationOptions{}, err
	}
	settings, err := SessionSettingsFromProto(request.ActiveSettings)
	if err != nil {
		return serverapi.SessionRuntimePreparationOptions{}, err
	}
	source, err := SessionSourceReportFromProto(request.Source)
	if err != nil {
		return serverapi.SessionRuntimePreparationOptions{}, err
	}
	selection, err := SessionRuntimeAgentSelectionFromProto(request.AgentSelection)
	if err != nil {
		return serverapi.SessionRuntimePreparationOptions{}, err
	}
	result := serverapi.SessionRuntimePreparationOptions{
		ActiveSettings: settings, Source: source,
		QuestionsEnabled: request.QuestionsEnabled, AutoCompactionEnabled: request.AutoCompactionEnabled,
		ThinkingOverrideExplicit: request.ThinkingOverrideExplicit, AgentSelection: selection,
	}
	if request.ConnectionScope == nil {
		return serverapi.SessionRuntimePreparationOptions{}, fmt.Errorf("connection scope is required")
	}
	scopeSelection, err := ConnectionSelectionFromProto(request.ConnectionScope.Selection)
	if err != nil {
		return serverapi.SessionRuntimePreparationOptions{}, err
	}
	result.ConnectionScope = config.ConnectionScope{Selection: scopeSelection, UnavailableRole: request.ConnectionScope.UnavailableRole}
	result.ExplicitToolSelection, err = ToolSelectionFromProto(request.ExplicitToolSelection)
	if err != nil {
		return serverapi.SessionRuntimePreparationOptions{}, err
	}
	for _, id := range request.EnabledToolIds {
		tool, err := SessionToolIDFromProto(id)
		if err != nil {
			return serverapi.SessionRuntimePreparationOptions{}, err
		}
		result.EnabledToolIDs = append(result.EnabledToolIDs, string(tool))
	}
	return result, nil
}
