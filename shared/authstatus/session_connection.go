package authstatus

import (
	"fmt"

	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

func ConnectionAuthMethod(connection config.ProviderConnection) authpb.AuthMethod {
	if connection.Protocol.IsSubscription() {
		return authpb.AuthMethod_AUTH_METHOD_OAUTH
	}
	if connection.EnvironmentVariable != nil {
		return authpb.AuthMethod_AUTH_METHOD_API_KEY
	}
	return authpb.AuthMethod_AUTH_METHOD_NONE
}

func SessionConnection(settings config.Settings, binding *config.ConnectionID) (*authpb.ConnectionFacts, error) {
	if binding == nil {
		return nil, nil
	}
	definition, present := settings.Connections[*binding]
	if !present {
		return nil, fmt.Errorf("Session Connection %s is not configured", *binding)
	}
	if len(settings.Connections) <= 1 {
		return nil, nil
	}
	return &authpb.ConnectionFacts{
		ConnectionId: string(*binding),
		Method:       ConnectionAuthMethod(definition),
	}, nil
}
