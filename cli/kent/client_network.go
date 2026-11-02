package main

import (
	"core/shared/config"
	"core/shared/proxyhttp"
)

// Each command resolves its initial local connection once before any requests.
// Subsequent dials and reconnects consume the installed process transport.
func loadCommandConnection(workspaceRoot string) (config.Connection, error) {
	connection, err := config.LoadConnectionDiscovery(workspaceRoot, config.LoadOptions{})
	if err != nil {
		return config.Connection{}, err
	}
	if err := proxyhttp.Configure(connection.Proxy); err != nil {
		return config.Connection{}, err
	}
	return connection, nil
}
