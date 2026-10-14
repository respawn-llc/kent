package protoapi

import (
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"core/shared/toolspec"
)

func SessionRuntimeActivateToProto(request serverapi.SessionRuntimeActivateRequest) (*sessionlaunchpb.SessionRuntimeActivateRequest, error) {
	settings, err := SessionSettingsToProto(request.ActiveSettings)
	if err != nil {
		return nil, err
	}
	source, err := SessionSourceReportToProto(request.Source)
	if err != nil {
		return nil, err
	}
	result := &sessionlaunchpb.SessionRuntimeActivateRequest{
		SessionId: SessionSelectorToProto(request.SessionID), ActiveSettings: settings, Source: source,
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
	settings, err := SessionSettingsFromProto(request.ActiveSettings)
	if err != nil {
		return serverapi.SessionRuntimeActivateRequest{}, err
	}
	source, err := SessionSourceReportFromProto(request.Source)
	if err != nil {
		return serverapi.SessionRuntimeActivateRequest{}, err
	}
	selection, err := SessionRuntimeAgentSelectionFromProto(request.AgentSelection)
	if err != nil {
		return serverapi.SessionRuntimeActivateRequest{}, err
	}
	result := serverapi.SessionRuntimeActivateRequest{
		SessionID: selector, ActiveSettings: settings, Source: source,
		QuestionsEnabled: request.QuestionsEnabled, AutoCompactionEnabled: request.AutoCompactionEnabled,
		ThinkingOverrideExplicit: request.ThinkingOverrideExplicit, AgentSelection: selection,
	}
	result.ExplicitToolSelection, err = ToolSelectionFromProto(request.ExplicitToolSelection)
	if err != nil {
		return serverapi.SessionRuntimeActivateRequest{}, err
	}
	for _, id := range request.EnabledToolIds {
		tool, err := SessionToolIDFromProto(id)
		if err != nil {
			return serverapi.SessionRuntimeActivateRequest{}, err
		}
		result.EnabledToolIDs = append(result.EnabledToolIDs, string(tool))
	}
	return result, nil
}

func SessionRuntimeAttachmentToProto(attachment serverapi.SessionRuntimeAttachment) *sessionlaunchpb.SessionRuntimeAttachment {
	return &sessionlaunchpb.SessionRuntimeAttachment{SessionId: attachment.SessionID, Generation: attachment.Generation}
}

func SessionRuntimeAttachmentFromProto(attachment *sessionlaunchpb.SessionRuntimeAttachment) serverapi.SessionRuntimeAttachment {
	return serverapi.SessionRuntimeAttachment{SessionID: attachment.SessionId, Generation: attachment.Generation}
}

func SessionRuntimeReleaseToProto(request serverapi.SessionRuntimeReleaseRequest) (*sessionlaunchpb.SessionRuntimeReleaseRequest, error) {
	result := &sessionlaunchpb.SessionRuntimeReleaseRequest{
		Attachment: SessionRuntimeAttachmentToProto(request.Attachment),
		DropOwner:  request.DropOwner,
	}
	if request.ClosePolicy != "" {
		policy, err := enumToProto(releaseClosePolicyValues, request.ClosePolicy, "Runtime release close policy")
		if err != nil {
			return nil, err
		}
		result.ClosePolicy = policy.Enum()
	}
	return result, nil
}

var releaseClosePolicyValues = map[sessionlaunchpb.SessionRuntimeReleaseClosePolicy]serverapi.SessionRuntimeReleaseClosePolicy{
	sessionlaunchpb.SessionRuntimeReleaseClosePolicy_SESSION_RUNTIME_RELEASE_CLOSE_POLICY_CLOSE_IF_IDLE: serverapi.SessionRuntimeReleaseClosePolicyCloseIfIdle,
	sessionlaunchpb.SessionRuntimeReleaseClosePolicy_SESSION_RUNTIME_RELEASE_CLOSE_POLICY_DETACH_ONLY:   serverapi.SessionRuntimeReleaseClosePolicyDetachOnly,
}

func SessionRuntimeReleaseFromProto(request *sessionlaunchpb.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseRequest, error) {
	result := serverapi.SessionRuntimeReleaseRequest{
		Attachment: SessionRuntimeAttachmentFromProto(request.Attachment),
		DropOwner:  request.DropOwner,
	}
	if request.ClosePolicy != nil {
		result.ClosePolicy = releaseClosePolicyValues[*request.ClosePolicy]
	}
	return result, nil
}
