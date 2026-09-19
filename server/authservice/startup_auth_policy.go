package authservice

import "core/shared/config"

func StartupAuthRequired(settings config.Settings) bool {
	if settings.Connection == nil {
		return false
	}
	connection, present := settings.Connections[*settings.Connection]
	if !present {
		return true
	}
	return connection.Protocol == config.ConnectionChatGPT || connection.EnvironmentVariable != nil
}
