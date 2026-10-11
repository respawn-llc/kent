package protoapi

import (
	"errors"

	pb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

func ChatSettingsErrorFromProto(value *pb.ReadError) error {
	if err := Validate(value); err != nil {
		return err
	}
	switch detail := value.Detail.(type) {
	case *pb.ReadError_WorkspaceNotRegistered:
		return serverapi.ErrWorkspaceNotRegistered
	case *pb.ReadError_SessionNotFound:
		return &sessioncontract.SessionNotFoundError{SessionID: detail.SessionNotFound.SessionId}
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
