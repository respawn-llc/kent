package testsetup

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"core/shared/config"

	"github.com/BurntSushi/toml"
)

// ProviderSettings supplies the ordinary first-party Responses test connection.
// Tests for missing selections use unmodified settings instead.
func ProviderSettings(settings config.Settings) config.Settings {
	if settings.Connection != nil {
		return settings
	}
	id, endpoint := config.ConnectionID("test"), "https://api.openai.com/v1"
	settings.Connection = &id
	settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		id: {Protocol: config.ConnectionResponses, Endpoint: &endpoint},
	}
	return settings
}

func WithResponsesProvider(settings config.Settings, endpoint string) config.Settings {
	settings = ProviderSettings(settings)
	settings.Connections = maps.Clone(settings.Connections)
	settings.Connections[*settings.Connection] = config.ProviderConnection{
		Protocol: config.ConnectionResponses, Endpoint: &endpoint,
	}
	return settings
}

func WriteProviderSettings(t *testing.T, root string, settings config.Settings) config.Settings {
	t.Helper()
	settings = ProviderSettings(settings)
	path := filepath.Join(root, "config.toml")
	document := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		if _, err := toml.Decode(string(data), &document); err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	connections := make(map[string]any)
	for id, definition := range settings.Connections {
		values := map[string]any{"protocol": string(definition.Protocol)}
		if definition.Endpoint != nil {
			values["endpoint"] = *definition.Endpoint
		}
		if definition.EnvironmentVariable != nil {
			values["environment_variable"] = *definition.EnvironmentVariable
		}
		if definition.Capabilities.ProviderID != "" {
			values["provider_capabilities"] = definition.Capabilities
		}
		connections[string(id)] = values
	}
	document["connections"], document["connection"] = connections, string(*settings.Connection)
	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(document); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return settings
}

// ProgrammaticConfig supplies declaration evidence for a fully specified Go
// fixture. These values are explicit inputs, not inferred configuration defaults.
func ProgrammaticConfig(t *testing.T, settings config.Settings) config.App {
	t.Helper()
	workspace := t.TempDir()
	root := t.TempDir()
	settings = WriteProviderSettings(t, root, settings)
	app, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	app.Settings = ProviderSettings(settings)
	for key := range app.Source.Sources {
		app.Source.Sources[key] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: key}}
	}
	for key := range settings.SkillToggles {
		address := "skills." + key
		app.Source.Sources[address] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: address}}
	}
	return app
}
