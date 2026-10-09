package config

import (
	"fmt"
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
	return editConnectionDocument(path, id, func(document *settingsDocument, existing map[ConnectionID]ProviderConnection) error {
		if _, present := existing[id]; present {
			return &ConnectionAlreadyExistsError{ID: id}
		}
		if len(existing) == 0 {
			if _, selected := document.get([]string{"connection"}); !selected {
				if err := document.set([]string{"connection"}, string(id)); err != nil {
					return err
				}
			}
		}
		return document.set([]string{"connections", string(id)}, connectionSettingsTable(definition))
	})
}

func SetProviderConnectionEnvironment(path string, id ConnectionID, name string) error {
	return editConnectionDocument(path, id, func(document *settingsDocument, existing map[ConnectionID]ProviderConnection) error {
		connection, present := existing[id]
		if !present {
			return &ConnectionReferenceError{Connection: &id}
		}
		if connection.Protocol != ConnectionResponses || connection.EnvironmentVariable == nil {
			return &ConnectionNotAPIKeyError{ID: id}
		}
		if *connection.EnvironmentVariable == name {
			return nil
		}
		connection.EnvironmentVariable = &name
		if err := connection.Validate(); err != nil {
			return err
		}
		return document.set([]string{"connections", string(id), "environment_variable"}, name)
	})
}

func SetDefaultProviderConnection(path string, id ConnectionID) error {
	return editConnectionDocument(path, id, func(document *settingsDocument, existing map[ConnectionID]ProviderConnection) error {
		if _, present := existing[id]; !present {
			return &ConnectionReferenceError{Connection: &id}
		}
		if value, present := document.get([]string{"connection"}); present {
			if current, ok := value.(string); ok && current == string(id) {
				return nil
			}
		}
		return document.set([]string{"connection"}, string(id))
	})
}

func editConnectionDocument(path string, id ConnectionID, edit func(*settingsDocument, map[ConnectionID]ProviderConnection) error) error {
	if _, err := ParseConnectionID(string(id)); err != nil {
		return err
	}
	document, raw, err := readSettingsDocument(path)
	if err != nil {
		return err
	}
	existing, err := readConnectionDefinitions(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := edit(document, existing); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := document.save(); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
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
