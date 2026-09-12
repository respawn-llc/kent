package protoapi

import (
	"errors"
	"fmt"

	pb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

func ChatSettingsErrorFromProto(value *pb.ReadError) error {
	if err := Validate(value); err != nil {
		return err
	}
	switch detail := value.Detail.(type) {
	case *pb.ReadError_AuthRequired:
		return serverapi.ErrServerAuthRequired
	case *pb.ReadError_ServerNotReady:
		return ServerNotReadyFromProto(detail.ServerNotReady)
	case *pb.ReadError_WorkspaceNotRegistered:
		return serverapi.ErrWorkspaceNotRegistered
	case *pb.ReadError_SessionNotFound:
		sessionID, err := runtimeids.ParseSessionID(detail.SessionNotFound.SessionId)
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: %s", sessioncontract.ErrSessionNotFound, sessionID)
	case *pb.ReadError_InternalFailure:
		return InternalFailureFromProto(detail.InternalFailure)
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
