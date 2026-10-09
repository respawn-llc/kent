package protoapi

import (
	"errors"
	"fmt"

	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

func ConnectionSelectionToProto(selection *config.ConnectionSelection) *authpb.ConnectionSelection {
	if selection == nil {
		return nil
	}
	ids := make([]string, len(*selection))
	for index, id := range *selection {
		ids[index] = string(id)
	}
	return &authpb.ConnectionSelection{Ids: ids}
}

func ConnectionSelectionFromProto(value *authpb.ConnectionSelection) (*config.ConnectionSelection, error) {
	if value == nil {
		return nil, nil
	}
	if len(value.Ids) == 0 {
		return nil, errors.New("connection selection must contain at least one ID")
	}
	selection := make(config.ConnectionSelection, 0, len(value.Ids))
	seen := make(map[config.ConnectionID]bool, len(value.Ids))
	for _, raw := range value.Ids {
		id, err := config.ParseConnectionID(raw)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate connection ID %q", id)
		}
		seen[id] = true
		selection = append(selection, id)
	}
	return &selection, nil
}

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
