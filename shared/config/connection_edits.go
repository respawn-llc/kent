package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
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
		if connection.EnvironmentVariable == nil {
			return &ConnectionNotAPIKeyError{ID: id}
		}
		if *connection.EnvironmentVariable == name {
			return nil
		}
		return document.set([]string{"connections", string(id), "environment_variable"}, name)
	})
}

// DeepSeekEndpointChoice is the explicit base for a DeepSeek reference edit.
// Official selects the default base. A custom choice carries the absolute base to store.
type DeepSeekEndpointChoice struct {
	Official bool
	Endpoint string
}

func (choice DeepSeekEndpointChoice) Base() (string, error) {
	if choice.Official {
		if strings.TrimSpace(choice.Endpoint) != "" {
			return "", fmt.Errorf("official DeepSeek base does not include an endpoint")
		}
		return DefaultDeepSeekResponsesEndpoint, nil
	}
	return NormalizeDeepSeekBase(choice.Endpoint)
}

func SetDeepSeekConnectionReference(path string, id ConnectionID, name string, choice DeepSeekEndpointChoice) error {
	return editConnectionDocument(path, id, func(document *settingsDocument, existing map[ConnectionID]ProviderConnection) error {
		connection, present := existing[id]
		if !present {
			return &ConnectionReferenceError{Connection: &id}
		}
		if connection.EnvironmentVariable == nil {
			return &ConnectionNotAPIKeyError{ID: id}
		}
		if connection.Protocol != ConnectionDeepSeekAPIKey {
			return fmt.Errorf("connection %q cannot change its endpoint", id)
		}
		base, err := choice.Base()
		if err != nil {
			return err
		}
		persisted, err := PersistDeepSeekEndpoint(&base)
		if err != nil {
			return err
		}
		if *connection.EnvironmentVariable != name {
			if err := document.set([]string{"connections", string(id), "environment_variable"}, name); err != nil {
				return err
			}
		}
		endpointPath := []string{"connections", string(id), "endpoint"}
		raw, rawPresent := document.get(endpointPath)
		rawText, rawIsText := raw.(string)
		switch {
		case persisted == nil && rawPresent:
			return document.delete(endpointPath)
		case persisted != nil && (!rawPresent || !rawIsText || rawText != *persisted):
			return document.set(endpointPath, *persisted)
		default:
			return nil
		}
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

func RemoveProviderConnection(path string, id ConnectionID) error {
	err := editConnectionDocument(path, id, func(document *settingsDocument, _ map[ConnectionID]ProviderConnection) error {
		return document.delete([]string{"connections", string(id)})
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func editConnectionDocument(path string, id ConnectionID, edit func(*settingsDocument, map[ConnectionID]ProviderConnection) error) error {
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
	if connection.SpeechToTextModel != nil {
		table["speech_to_text_model"] = *connection.SpeechToTextModel
	}
	if connection.Capabilities != (ProviderCapabilitiesOverride{}) {
		table["provider_capabilities"] = connection.Capabilities
	}
	return table
}
