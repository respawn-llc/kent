package protoapi

import (
	"errors"

	pb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/sessioncontract"

	"google.golang.org/protobuf/proto"
)

type ChatPreparationFailure interface {
	proto.Message
	GetCode() string
	GetWorkspaceNotRegistered() *pb.WorkspaceNotRegisteredDetails
	GetSessionNotFound() *pb.SessionNotFoundDetails
	GetChatSettingsAgentPreparation() *pb.AgentPreparationDetails
}

func ChatSettingsErrorFromProto[T ChatPreparationFailure](value T) error {
	switch {
	case value.GetWorkspaceNotRegistered() != nil:
		return serverapi.ErrWorkspaceNotRegistered
	case value.GetSessionNotFound() != nil:
		detail := value.GetSessionNotFound()
		cause := &sessioncontract.SessionNotFoundError{}
		if detail.SessionTarget == nil {
			return cause
		}
		captured := SessionTargetContextErrorFromProto(detail.SessionTarget, cause)
		var target *serverapi.SessionTargetError
		if errors.As(captured, &target) {
			cause.SessionID = OptionalSessionIDToProto(target.SessionID())
		}
		return captured
	case value.GetChatSettingsAgentPreparation() != nil:
		detail := value.GetChatSettingsAgentPreparation()
		return &serverapi.ChatSettingsAgentPreparationError{
			Agent: detail.Agent,
			Category: map[pb.AgentPreparationCategory]serverapi.ChatSettingsAgentPreparationCategory{
				pb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_INVALID_CONFIGURATION: serverapi.ChatSettingsAgentInvalidConfiguration,
				pb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_PROVIDER_UNAVAILABLE:  serverapi.ChatSettingsAgentProviderUnavailable,
				pb.AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_INTERNAL_PREPARATION:  serverapi.ChatSettingsAgentInternalPreparation,
			}[detail.Category],
		}
	default:
		return errors.New(value.GetCode())
	}
}
