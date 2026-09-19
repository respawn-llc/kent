package config

import (
	"fmt"
	"net/url"
	"strings"
)

type ConnectionID string
type ConnectionProtocol string

type ConnectionReplacement struct {
	Previous ConnectionID
	Current  ConnectionID
}

const (
	ConnectionResponses ConnectionProtocol = "responses"
	ConnectionChatGPT   ConnectionProtocol = "chatgpt-codex"
)

type ProviderConnection struct {
	Protocol            ConnectionProtocol
	Endpoint            *string
	EnvironmentVariable *string
	Capabilities        ProviderCapabilitiesOverride
}

// SelectedConnection reads an already effective reference; role inheritance and
// persisted Session binding selection happen before this lookup.
func (s Settings) SelectedConnection() (ProviderConnection, error) {
	if s.Connection == nil {
		return ProviderConnection{}, fmt.Errorf("select a provider connection with the connection setting")
	}
	connection, present := s.Connections[*s.Connection]
	if !present {
		return ProviderConnection{}, fmt.Errorf("provider connection %q is not defined in the server global configuration", *s.Connection)
	}
	return connection, connection.Validate()
}

func (c ProviderConnection) Validate() error {
	switch c.Protocol {
	case ConnectionChatGPT:
		if c.Endpoint != nil || c.EnvironmentVariable != nil {
			return fmt.Errorf("ChatGPT connections use subscription sign-in, without an endpoint or environment variable")
		}
	case ConnectionResponses:
		if c.Endpoint == nil {
			return fmt.Errorf("Responses connections require an endpoint")
		}
		endpoint, err := url.Parse(*c.Endpoint)
		if err != nil || endpoint.Host == "" || endpoint.Scheme != "http" && endpoint.Scheme != "https" {
			return fmt.Errorf("connection endpoint must be an absolute HTTP or HTTPS URL")
		}
		if c.EnvironmentVariable != nil && strings.TrimSpace(*c.EnvironmentVariable) == "" {
			return fmt.Errorf("environment_variable cannot be empty; omit it for auth-less access")
		}
	default:
		return fmt.Errorf("connection protocol must be responses or chatgpt-codex")
	}
	return nil
}

func ParseConnectionID(value string) (ConnectionID, error) {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return "", fmt.Errorf("connection ID %q must begin with a lowercase ASCII letter", value)
	}
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			continue
		}
		return "", fmt.Errorf("connection ID %q must contain only lowercase ASCII letters, digits, hyphens and underscores", value)
	}
	return ConnectionID(value), nil
}

func newConnectionReference(key string, apply func(*settingsState, *ConnectionID), get func(settingsState) *ConnectionID) scalarSetting[*ConnectionID] {
	return scalarSetting[*ConnectionID]{
		key: key, apply: apply, get: get,
		equal: func(a, b *ConnectionID) bool { return a == b || a != nil && b != nil && *a == *b },
		decodeFile: func(raw settingsFile, path []string) (*ConnectionID, bool, error) {
			value, present, err := lookupFileValue(raw, path)
			if err != nil || !present {
				return nil, present, err
			}
			text, ok := value.(string)
			if !ok {
				return nil, false, &SettingsKeyTypeError{Key: key, ExpectedType: "string"}
			}
			id, err := ParseConnectionID(text)
			return &id, true, err
		},
		doc: settingDocOptions{omitInTOML: true},
	}
}

type connectionsSetting struct{}

func (connectionsSetting) registryKey() string                     { return "connections" }
func (connectionsSetting) appliesToSubagentRole() bool             { return false }
func (connectionsSetting) appliesToFileLayer(layer FileLayer) bool { return layer == FileGlobal }
func (connectionsSetting) applyDefault(state *settingsState) {
	state.Settings.Connections = make(map[ConnectionID]ProviderConnection)
}

func (connectionsSetting) registerFileKeys(tree *fileKeyTree) {
	template := newFileKeyTree()
	for _, key := range []string{"protocol", "endpoint", "environment_variable"} {
		template.allowPath([]string{key})
	}
	for _, key := range providerCapabilityKeys {
		template.allowPath(strings.Split(key, "."))
	}
	tree.allowDynamicChildren([]string{"connections"}, func(string) bool { return true }, template)
}

func (connectionsSetting) applyFile(raw settingsFile, file SourceFile, state *settingsState, sources map[string]Origin) error {
	table, present, err := lookupFileTable(raw, []string{"connections"})
	if err != nil || !present {
		return err
	}
	for name := range table {
		id, err := ParseConnectionID(name)
		if err != nil {
			return err
		}
		definition, _, err := lookupFileTable(table, []string{name})
		if err != nil {
			return err
		}
		protocol, _, err := lookupFileStringAllowEmpty(definition, []string{"protocol"})
		if err != nil {
			return err
		}
		connection := ProviderConnection{Protocol: ConnectionProtocol(protocol)}
		for _, field := range []struct {
			key string
			to  **string
		}{{"endpoint", &connection.Endpoint}, {"environment_variable", &connection.EnvironmentVariable}} {
			value, present, err := lookupFileStringAllowEmpty(definition, []string{field.key})
			if err != nil {
				return err
			}
			if present {
				*field.to = &value
			}
		}
		connection.Capabilities, err = readConnectionCapabilities(definition)
		if err != nil {
			return fmt.Errorf("connections.%s: %w", name, err)
		}
		if err := connection.Validate(); err != nil {
			return fmt.Errorf("connections.%s: %w", name, err)
		}
		state.Settings.Connections[id] = connection
		key := "connections." + name
		sources[key] = fileOrigin(key, file)
	}
	return nil
}

func readConnectionCapabilities(definition settingsFile) (ProviderCapabilitiesOverride, error) {
	var result ProviderCapabilitiesOverride
	raw, present, err := lookupFileTable(definition, []string{"provider_capabilities"})
	if err != nil || !present {
		return result, err
	}
	result.ProviderID, present, err = lookupFileString(raw, []string{"provider_id"})
	if err != nil {
		return result, err
	}
	if !present {
		return result, errProviderCapabilitiesNeedID
	}
	for _, field := range []struct {
		key string
		to  *bool
	}{
		{"supports_responses_api", &result.SupportsResponsesAPI},
		{"supports_responses_compact", &result.SupportsResponsesCompact},
		{"supports_prompt_cache_key", &result.SupportsPromptCacheKey},
		{"supports_native_web_search", &result.SupportsNativeWebSearch},
		{"supports_reasoning_encrypted", &result.SupportsReasoningEncrypted},
		{"supports_server_side_context_edit", &result.SupportsServerSideContextEdit},
		{"supports_provider_verbosity", &result.SupportsProviderVerbosity},
		{"is_openai_first_party", &result.IsOpenAIFirstParty},
	} {
		*field.to, _, err = lookupFileBool(raw, []string{field.key})
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
