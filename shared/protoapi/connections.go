package protoapi

import (
	"errors"
	"fmt"

	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

func ConnectionToProto(id config.ConnectionID, definition config.ProviderConnection) *authpb.ConnectionDefinition {
	protocol := authpb.ConnectionProtocol_CONNECTION_PROTOCOL_RESPONSES
	if definition.Protocol == config.ConnectionChatGPT {
		protocol = authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT
	}
	result := &authpb.ConnectionDefinition{Id: string(id), Protocol: protocol, Endpoint: definition.Endpoint, EnvironmentVariable: definition.EnvironmentVariable}
	if definition.Capabilities != (config.ProviderCapabilitiesOverride{}) {
		result.Capabilities = providerCapabilitiesToProto(definition.Capabilities)
	}
	return result
}

func ConnectionFromProto(value *authpb.ConnectionDefinition) (config.ConnectionID, config.ProviderConnection, error) {
	if value == nil {
		return "", config.ProviderConnection{}, errors.New("connection definition is required")
	}
	id, err := config.ParseConnectionID(value.Id)
	if err != nil {
		return "", config.ProviderConnection{}, err
	}
	definition := config.ProviderConnection{Endpoint: value.Endpoint, EnvironmentVariable: value.EnvironmentVariable}
	switch value.Protocol {
	case authpb.ConnectionProtocol_CONNECTION_PROTOCOL_RESPONSES:
		definition.Protocol = config.ConnectionResponses
	case authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT:
		definition.Protocol = config.ConnectionChatGPT
	default:
		return "", config.ProviderConnection{}, fmt.Errorf("unsupported connection protocol %v", value.Protocol)
	}
	if value.Capabilities != nil {
		definition.Capabilities = providerCapabilitiesFromProto(value.Capabilities)
	}
	return id, definition, definition.Validate()
}

func ExistingConnectionTarget(id config.ConnectionID) *authpb.ConnectionTarget {
	return &authpb.ConnectionTarget{Target: &authpb.ConnectionTarget_ConnectionId{ConnectionId: string(id)}}
}
