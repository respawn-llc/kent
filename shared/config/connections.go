package config

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

type ConnectionID string
type ConnectionSelection []ConnectionID
type ConnectionProtocol string

func SingleConnection(id ConnectionID) *ConnectionSelection {
	selection := ConnectionSelection{id}
	return &selection
}

func (s *ConnectionSelection) ConcreteID() (*ConnectionID, error) {
	if s == nil {
		return nil, &ConnectionReferenceError{}
	}
	if len(*s) != 1 {
		return nil, fmt.Errorf("connection selection must be resolved to one connection before provider preparation")
	}
	return &(*s)[0], nil
}

func (s *ConnectionSelection) TOMLValue() any {
	if len(*s) == 1 {
		return string((*s)[0])
	}
	values := make([]string, len(*s))
	for index, id := range *s {
		values[index] = string(id)
	}
	return values
}

func (s Settings) ConnectionMembers() ([]ConnectionID, error) {
	if s.Connection == nil {
		return nil, &ConnectionReferenceError{}
	}
	members := make([]ConnectionID, 0, len(*s.Connection))
	if len(*s.Connection) == 1 {
		id := (*s.Connection)[0]
		if _, present := s.Connections[id]; present {
			return []ConnectionID{id}, nil
		}
		return nil, &ConnectionReferenceError{Connection: &id}
	}
	for _, id := range s.ConnectionOrder {
		if slices.Contains(*s.Connection, id) {
			members = append(members, id)
		}
	}
	if len(members) == 0 {
		return nil, &ConnectionReferenceError{Selection: s.Connection}
	}
	return members, nil
}

type ConnectionReplacement struct {
	Previous ConnectionID
	Current  ConnectionID
}

const (
	ConnectionResponses            ConnectionProtocol = "responses"
	ConnectionChatGPT              ConnectionProtocol = "chatgpt-codex"
	DefaultOpenAIResponsesEndpoint                    = "https://api.openai.com/v1"
)

type ProviderConnection struct {
	Protocol            ConnectionProtocol
	Endpoint            *string
	EnvironmentVariable *string
	Capabilities        ProviderCapabilitiesOverride
}

type ConnectionReferenceError struct {
	Connection *ConnectionID
	Selection  *ConnectionSelection
}

func (e *ConnectionReferenceError) Error() string {
	if e.Selection != nil {
		return fmt.Sprintf("connection selection %v has no defined members; add its connections to global config.toml or select an existing connection", *e.Selection)
	}
	if e.Connection == nil {
		return "Kent has no provider connection selected. Run kent in an interactive terminal to set one up, or choose a connection in the server's global config.toml. See " + DocsURL + "/authentication/"
	}
	return fmt.Sprintf("Kent cannot use connection %q because it is missing from the server's global config.toml. Add that connection, or select an existing one. See %s/authentication/", *e.Connection, DocsURL)
}

// SelectedConnection reads an already effective reference; role inheritance and
// persisted Session binding selection happen before this lookup.
func (s Settings) SelectedConnection() (ProviderConnection, error) {
	id, err := s.Connection.ConcreteID()
	if err != nil {
		return ProviderConnection{}, err
	}
	connection, present := s.Connections[*id]
	if !present {
		return ProviderConnection{}, &ConnectionReferenceError{Connection: id}
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

func newConnectionReference(key string, allowSet bool, apply func(*settingsState, *ConnectionSelection), get func(settingsState) *ConnectionSelection) scalarSetting[*ConnectionSelection] {
	return scalarSetting[*ConnectionSelection]{
		key: key, apply: apply, get: get,
		equal: func(a, b *ConnectionSelection) bool { return a == b || a != nil && b != nil && slices.Equal(*a, *b) },
		decodeFile: func(raw settingsFile, path []string) (*ConnectionSelection, bool, error) {
			value, present, err := lookupFileValue(raw, path)
			if err != nil || !present {
				return nil, present, err
			}
			var values []any
			switch value := value.(type) {
			case string:
				values = []any{value}
			case []any:
				if !allowSet {
					return nil, false, &SettingsKeyTypeError{Key: key, ExpectedType: "string"}
				}
				values = value
			default:
				return nil, false, &SettingsKeyTypeError{Key: key, ExpectedType: "string or array of strings"}
			}
			if len(values) == 0 {
				return nil, false, fmt.Errorf("%s must contain at least one connection ID", key)
			}
			selection := ConnectionSelection{}
			for _, value := range values {
				text, ok := value.(string)
				if !ok {
					return nil, false, &SettingsKeyTypeError{Key: key, ExpectedType: "array of strings"}
				}
				id, err := ParseConnectionID(text)
				if err != nil {
					return nil, false, err
				}
				if !slices.Contains(selection, id) {
					selection = append(selection, id)
				}
			}
			return &selection, true, nil
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
		{"supports_fast_mode", &result.SupportsFastMode},
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
