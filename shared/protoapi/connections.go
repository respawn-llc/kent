package protoapi

import (
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

func ConnectionSelectionFromProto(value *authpb.ConnectionSelection) *config.ConnectionSelection {
	if value == nil {
		return nil
	}
	selection := make(config.ConnectionSelection, 0, len(value.Ids))
	for _, raw := range value.Ids {
		selection = append(selection, config.ConnectionID(raw))
	}
	return &selection
}

var connectionProtocolValues = map[authpb.ConnectionProtocol]config.ConnectionProtocol{
	authpb.ConnectionProtocol_CONNECTION_PROTOCOL_RESPONSES:        config.ConnectionResponses,
	authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT:          config.ConnectionChatGPT,
	authpb.ConnectionProtocol_CONNECTION_PROTOCOL_GROK_CLI_PROXY:   config.ConnectionGrokCLIProxy,
	authpb.ConnectionProtocol_CONNECTION_PROTOCOL_GROK_OAUTH_API:   config.ConnectionGrokOAuthAPI,
	authpb.ConnectionProtocol_CONNECTION_PROTOCOL_GROK_API_KEY:     config.ConnectionGrokAPIKey,
	authpb.ConnectionProtocol_CONNECTION_PROTOCOL_DEEPSEEK_API_KEY: config.ConnectionDeepSeekAPIKey,
}

func ConnectionToProto(id config.ConnectionID, definition config.ProviderConnection) *authpb.ConnectionDefinition {
	var protocol authpb.ConnectionProtocol
	for wire, native := range connectionProtocolValues {
		if native == definition.Protocol {
			protocol = wire
			break
		}
	}
	result := &authpb.ConnectionDefinition{Id: string(id), Protocol: protocol, Endpoint: definition.Endpoint, EnvironmentVariable: definition.EnvironmentVariable, SpeechToTextModel: definition.SpeechToTextModel}
	if definition.Capabilities != (config.ProviderCapabilitiesOverride{}) {
		result.Capabilities = providerCapabilitiesToProto(definition.Capabilities)
	}
	return result
}

func ConnectionFromProto(value *authpb.ConnectionDefinition) (config.ConnectionID, config.ProviderConnection, error) {
	id := config.ConnectionID(value.Id)
	definition := config.ProviderConnection{Endpoint: value.Endpoint, EnvironmentVariable: value.EnvironmentVariable, SpeechToTextModel: value.SpeechToTextModel}
	definition.Protocol = connectionProtocolValues[value.Protocol]
	if definition.Protocol == config.ConnectionResponses {
		if err := config.ValidateResponsesEndpoint(*definition.Endpoint); err != nil {
			return "", config.ProviderConnection{}, err
		}
	}
	if definition.Protocol == config.ConnectionDeepSeekAPIKey {
		persisted, err := config.PersistDeepSeekEndpoint(definition.Endpoint)
		if err != nil {
			return "", config.ProviderConnection{}, err
		}
		definition.Endpoint = persisted
	}
	if value.Capabilities != nil {
		definition.Capabilities = providerCapabilitiesFromProto(value.Capabilities)
	}
	return id, definition, nil
}

func DeepSeekEndpointChoiceFromProto(choice *authpb.DeepSeekEndpointChoice) (config.DeepSeekEndpointChoice, error) {
	if choice == nil {
		return config.DeepSeekEndpointChoice{}, fmt.Errorf("DeepSeek endpoint choice is required")
	}
	switch selected := choice.Choice.(type) {
	case *authpb.DeepSeekEndpointChoice_Official:
		return config.DeepSeekEndpointChoice{Official: true}, nil
	case *authpb.DeepSeekEndpointChoice_Endpoint:
		return config.DeepSeekEndpointChoice{Endpoint: selected.Endpoint}, nil
	default:
		return config.DeepSeekEndpointChoice{}, fmt.Errorf("DeepSeek endpoint choice is required")
	}
}

func ExistingConnectionTarget(id config.ConnectionID) *authpb.ConnectionTarget {
	return &authpb.ConnectionTarget{Target: &authpb.ConnectionTarget_ConnectionId{ConnectionId: string(id)}}
}
