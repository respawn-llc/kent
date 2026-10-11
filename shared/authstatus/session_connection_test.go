package authstatus

import (
	"testing"

	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

func TestSessionConnectionUsesSavedBindingAndGlobalCount(t *testing.T) {
	saved := config.ConnectionID("saved")
	settings := config.Settings{
		Connection: config.SingleConnection("default"),
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"default": {Protocol: config.ConnectionChatGPT},
			saved:     {Protocol: config.ConnectionResponses, EnvironmentVariable: testString("KEY")},
		},
	}
	facts, err := SessionConnection(settings, &saved)
	if err != nil || facts == nil || facts.ConnectionId != string(saved) || facts.Method != authpb.AuthMethod_AUTH_METHOD_API_KEY {
		t.Fatalf("saved binding = %+v, %v", facts, err)
	}
	delete(settings.Connections, "default")
	if facts, err := SessionConnection(settings, &saved); err != nil || facts != nil {
		t.Fatalf("single configured Connection = %+v, %v", facts, err)
	}
}

func TestSessionConnectionModesAndUnavailableBindings(t *testing.T) {
	saved := config.ConnectionID("saved")
	for _, tc := range []struct {
		definition config.ProviderConnection
		method     authpb.AuthMethod
	}{
		{config.ProviderConnection{Protocol: config.ConnectionGrokOAuthAPI}, authpb.AuthMethod_AUTH_METHOD_OAUTH},
		{config.ProviderConnection{Protocol: config.ConnectionGrokAPIKey, EnvironmentVariable: testString("KEY")}, authpb.AuthMethod_AUTH_METHOD_API_KEY},
		{config.ProviderConnection{Protocol: config.ConnectionResponses}, authpb.AuthMethod_AUTH_METHOD_NONE},
	} {
		settings := config.Settings{Connections: map[config.ConnectionID]config.ProviderConnection{
			saved: tc.definition, "other": {Protocol: config.ConnectionChatGPT},
		}}
		facts, err := SessionConnection(settings, &saved)
		if err != nil || facts == nil || facts.Method != tc.method {
			t.Fatalf("Connection mode = %+v, %v", facts, err)
		}
		if facts, err := SessionConnection(settings, nil); err != nil || facts != nil {
			t.Fatalf("unbound Session = %+v, %v", facts, err)
		}
		missing := config.ConnectionID("deleted")
		if facts, err := SessionConnection(settings, &missing); err == nil || facts != nil {
			t.Fatalf("deleted Connection = %+v, %v", facts, err)
		}
	}
}
