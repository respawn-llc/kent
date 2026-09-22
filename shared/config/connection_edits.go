package config

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
)

type ConnectionAlreadyExistsError struct{ ID ConnectionID }

func (e *ConnectionAlreadyExistsError) Error() string {
	return fmt.Sprintf("connection %q already exists", e.ID)
}

type ConnectionNotAPIKeyError struct{ ID ConnectionID }

func (e *ConnectionNotAPIKeyError) Error() string {
	return fmt.Sprintf("connection %q is not API-key-backed", e.ID)
}

func AddProviderConnection(path string, id ConnectionID, definition ProviderConnection) error {
	if err := definition.Validate(); err != nil {
		return err
	}
	return editConnectionFile(path, id, func(raw, definitions settingsFile, existing map[ConnectionID]ProviderConnection) error {
		if _, present := existing[id]; present {
			return &ConnectionAlreadyExistsError{ID: id}
		}
		definitions[string(id)] = connectionSettingsTable(definition)
		raw["connections"] = map[string]any(definitions)
		return nil
	})
}

func SetProviderConnectionEnvironment(path string, id ConnectionID, name string) error {
	return editConnectionFile(path, id, func(_ settingsFile, definitions settingsFile, existing map[ConnectionID]ProviderConnection) error {
		connection, present := existing[id]
		if !present {
			return &ConnectionReferenceError{Connection: &id}
		}
		if connection.Protocol != ConnectionResponses || connection.EnvironmentVariable == nil {
			return &ConnectionNotAPIKeyError{ID: id}
		}
		connection.EnvironmentVariable = &name
		if err := connection.Validate(); err != nil {
			return err
		}
		table, _, err := lookupFileTable(definitions, []string{string(id)})
		if err != nil {
			return err
		}
		table["environment_variable"] = name
		return nil
	})
}

func SetDefaultProviderConnection(path string, id ConnectionID) error {
	return editConnectionFile(path, id, func(raw, _ settingsFile, existing map[ConnectionID]ProviderConnection) error {
		if _, present := existing[id]; !present {
			return &ConnectionReferenceError{Connection: &id}
		}
		raw["connection"] = string(id)
		return nil
	})
}

func editConnectionFile(path string, id ConnectionID, edit func(settingsFile, settingsFile, map[ConnectionID]ProviderConnection) error) error {
	if _, err := ParseConnectionID(string(id)); err != nil {
		return err
	}
	raw, err := readSettingsFile(path)
	if err != nil {
		return err
	}
	existing, err := readConnectionDefinitions(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	definitions, _, err := lookupFileTable(raw, []string{"connections"})
	if err != nil {
		return err
	}
	if definitions == nil {
		definitions = settingsFile{}
	}
	if err := edit(raw, definitions, existing); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return writeSettingsTable(path, raw)
}

func readConnectionDefinitions(raw settingsFile) (map[ConnectionID]ProviderConnection, error) {
	state := configRegistry.defaultState()
	if err := (connectionsSetting{}).applyFile(raw, SourceFile{}, &state, map[string]Origin{}); err != nil {
		return nil, err
	}
	return state.Settings.Connections, nil
}

func connectionSettingsTable(connection ProviderConnection) map[string]any {
	table := map[string]any{"protocol": string(connection.Protocol)}
	if connection.Endpoint != nil {
		table["endpoint"] = *connection.Endpoint
	}
	if connection.EnvironmentVariable != nil {
		table["environment_variable"] = *connection.EnvironmentVariable
	}
	if connection.Capabilities != (ProviderCapabilitiesOverride{}) {
		table["provider_capabilities"] = connection.Capabilities
	}
	return table
}

func writeSettingsTable(path string, raw settingsFile) error {
	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(raw); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := replaceSettingsFile(path, output.String()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
