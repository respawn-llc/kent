package config

import (
	"net"
	"os"
	"strconv"

	"core/shared/protocol"
)

// Connection contains only the inputs needed to target a running server.
type Connection struct {
	WorkspaceRoot   string
	PersistenceRoot string
	ServerHost      string
	ServerPort      int
	Source          SourceReport
}

func (connection Connection) RPCURL() string {
	return "ws://" + net.JoinHostPort(connection.ServerHost, strconv.Itoa(connection.ServerPort)) + protocol.RPCPath
}

type LocalPreferences struct {
	Theme                string
	Debug                bool
	NotificationMethod   string
	TUINativeProgressBar bool
	Client               ClientSettings
}

var connectionSettings = clientSettingsRegistry("server_host", "server_port")
var interactiveSettings = clientSettingsRegistry(
	"server_host", "server_port", "theme", "debug", "notification_method",
	"tui_native_progress_bar", "hooks.client.lifecycle",
)

func clientSettingsRegistry(keys ...string) settingsRegistry {
	selected := settingsRegistry{}
	for _, key := range keys {
		for _, setting := range configRegistry.settings {
			if keyed, ok := setting.(keyedRegistrySetting); ok && keyed.registryKey() == key {
				selected.settings = append(selected.settings, setting)
				break
			}
		}
	}
	return selected
}

func resolveClientConfiguration(sharedRoot string, opts LoadOptions, registry settingsRegistry) (Connection, LocalPreferences, error) {
	state := registry.defaultState()
	sources := registry.defaultSourceMap()
	locations, err := readConfigurationSources(&workspaceConfigRoots{Shared: sharedRoot}, opts, func(raw settingsFile, file SourceFile) (bool, error) {
		for _, setting := range registry.settings {
			if settingAppliesToFileLayer(setting, file.Layer) {
				if err := setting.applyFile(raw, file, &state, sources); err != nil {
					return false, err
				}
			}
		}
		return fileHasDeclarations(file, sources), nil
	})
	if err != nil {
		return Connection{}, LocalPreferences{}, err
	}
	sources["persistence_root"] = locations.rootOrigin
	if err := registry.applyEnv(os.LookupEnv, &state, sources); err != nil {
		return Connection{}, LocalPreferences{}, err
	}
	if err := registry.applyCLI(opts, &state, sources); err != nil {
		return Connection{}, LocalPreferences{}, err
	}
	return Connection{
		WorkspaceRoot: locations.workspaceRoot, PersistenceRoot: locations.persistenceRoot,
		ServerHost: state.Settings.ServerHost, ServerPort: state.Settings.ServerPort,
		Source: SourceReport{Files: locations.files, Sources: sources},
	}, LocalPreferences{
		Theme: state.Settings.Theme, Debug: state.Settings.Debug,
		NotificationMethod:   state.Settings.NotificationMethod,
		TUINativeProgressBar: state.Settings.TUINativeProgressBar,
		Client:               state.Client,
	}, nil
}
