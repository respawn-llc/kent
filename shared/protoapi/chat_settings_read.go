package protoapi

import (
	"errors"

	pb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

func ChatSettingsErrorFromProto(value *pb.ReadError) error {
	switch detail := value.Detail.(type) {
	case *pb.ReadError_WorkspaceNotRegistered:
		return serverapi.ErrWorkspaceNotRegistered
	case *pb.ReadError_SessionNotFound:
		cause := &sessioncontract.SessionNotFoundError{}
		if detail.SessionNotFound.SessionTarget == nil {
			return cause
		}
		captured := SessionTargetContextErrorFromProto(detail.SessionNotFound.SessionTarget, cause)
		var target *serverapi.SessionTargetError
		if errors.As(captured, &target) {
			cause.SessionID = OptionalSessionIDToProto(target.SessionID())
		}
		return captured
	case *pb.ReadError_ChatSettingsAgentPreparation:
		return &serverapi.ChatSettingsAgentPreparationError{
			Agent: detail.ChatSettingsAgentPreparation.Agent,
			Category: map[pb.AgentPreparationCategory]serverapi.ChatSettingsAgentPreparationCategory{
				pb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_INVALID_CONFIGURATION: serverapi.ChatSettingsAgentInvalidConfiguration,
				pb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_PROVIDER_UNAVAILABLE:  serverapi.ChatSettingsAgentProviderUnavailable,
				pb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_INTERNAL_PREPARATION:  serverapi.ChatSettingsAgentInternalPreparation,
			}[detail.ChatSettingsAgentPreparation.Category],
		}
	default:
		return errors.New(value.Code)
	}
}
