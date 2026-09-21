package authservice

import (
	"testing"

	"core/shared/config"
	"core/shared/textutil"
)

func TestStartupAuthRequiredUsesDeclaredConnection(t *testing.T) {
	id := config.ConnectionID("selected")
	for _, tc := range []struct {
		connection config.ProviderConnection
		required   bool
	}{
		{config.ProviderConnection{Protocol: config.ConnectionChatGPT}, true},
		{config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://localhost:1234")}, false},
		{config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://localhost:1234"), EnvironmentVariable: textutil.Value("KEY")}, true},
	} {
		settings := config.Settings{Connection: &id, Connections: map[config.ConnectionID]config.ProviderConnection{id: tc.connection}}
		if got := StartupAuthRequired(settings); got != tc.required {
			t.Fatalf("auth required = %v, want %v", got, tc.required)
		}
	}
}
