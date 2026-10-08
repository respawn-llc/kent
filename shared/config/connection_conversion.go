package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// LegacyConnectionAuth is supplied by the server's one-time credential cutover.
// Configuration conversion never reads credentials or adopts environment keys.
type LegacyConnectionAuth string

const (
	LegacyConnectionAnonymous LegacyConnectionAuth = "anonymous"
	LegacyConnectionOAuth     LegacyConnectionAuth = "oauth"
	LegacyConnectionAPIKey    LegacyConnectionAuth = "api-key"
)

type connectionConversion struct {
	reference  ConnectionID
	definition *connectionDefinition
}

type connectionDefinition struct {
	id       ConnectionID
	settings map[string]any
}

func ConvertGlobalConnections(path string, selection *LegacyConnectionAuth) (bool, error) {
	document, raw, err := readSettingsDocument(path)
	if err != nil {
		return false, err
	}
	changed, err := convertConnectionScopes(document, raw, selection)
	if err != nil {
		return false, fmt.Errorf("convert %s: %w", path, err)
	}
	if !changed {
		return false, nil
	}
	if err := document.save(); err != nil {
		return false, fmt.Errorf("convert %s: %w", path, err)
	}
	return true, nil
}

var legacyAccessKeys = []string{"provider_override", "openai_base_url", "provider_capabilities"}

type ConnectionConversionRequiredError struct {
	Path string
	Keys []string
}

func (e *ConnectionConversionRequiredError) Error() string {
	return fmt.Sprintf("%s: manually replace old provider-access settings %s with connection references; define the named connections in the server global config", e.Path, strings.Join(e.Keys, ", "))
}

func rejectWorkspaceLegacyAccess(raw settingsFile, path string) error {
	var keys []string
	inspect := func(table settingsFile, prefix []string) error {
		for _, key := range legacyAccessKeys {
			if _, present := table[key]; present {
				keys = append(keys, strings.Join(append(slices.Clone(prefix), key), "."))
			}
		}
		reviewer, present, err := lookupFileTable(table, []string{"reviewer"})
		if err != nil {
			return err
		}
		if present {
			for _, key := range legacyAccessKeys {
				if _, present := reviewer[key]; present {
					keys = append(keys, strings.Join(append(slices.Clone(prefix), "reviewer", key), "."))
				}
			}
		}
		return nil
	}
	if err := inspect(raw, nil); err != nil {
		return err
	}
	roles, _, err := lookupFileTable(raw, []string{"subagents"})
	if err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(roles)) {
		role, _, err := lookupFileTable(roles, []string{name})
		if err != nil {
			return err
		}
		if err := inspect(role, []string{"subagents", name}); err != nil {
			return err
		}
	}
	if len(keys) > 0 {
		return &ConnectionConversionRequiredError{Path: path, Keys: keys}
	}
	return nil
}

func hasLegacyAccess(raw settingsFile) bool {
	for _, key := range legacyAccessKeys {
		if _, present := raw[key]; present {
			return true
		}
	}
	return false
}

func inheritedLegacyAccess(base, overlay settingsFile) (settingsFile, error) {
	result := settingsFile{}
	for _, source := range []settingsFile{base, overlay} {
		for _, key := range legacyAccessKeys {
			if value, present := source[key]; present {
				if key == "provider_capabilities" {
					table, _, err := lookupFileTable(source, []string{key})
					if err != nil {
						return nil, err
					}
					merged := map[string]any{}
					if old, ok := result[key]; ok {
						maps.Copy(merged, old.(map[string]any))
					}
					maps.Copy(merged, table)
					result[key] = merged
				} else {
					if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
						if _, inherited := result[key]; inherited {
							continue
						}
					}
					result[key] = value
				}
			}
		}
	}
	return result, nil
}

func convertConnectionScopes(document *settingsDocument, raw settingsFile, selection *LegacyConnectionAuth) (bool, error) {
	mainAccess, err := inheritedLegacyAccess(nil, raw)
	if err != nil {
		return false, err
	}
	reviewer, _, err := lookupFileTable(raw, []string{"reviewer"})
	if err != nil {
		return false, err
	}
	reviewerAccess, err := inheritedLegacyAccess(nil, reviewer)
	if err != nil {
		return false, err
	}
	changed := false
	conversion, err := convertMainConnection(raw, selection, nil)
	if err != nil {
		return false, err
	}
	if conversion != nil {
		if err := applyConnectionConversion(document, nil, conversion); err != nil {
			return false, err
		}
		changed = true
	}
	current, err := document.settings()
	if err != nil {
		return false, err
	}
	var inheritedEnvironment *string
	if reference, present, err := lookupFileString(current, []string{"connection"}); err != nil {
		return false, err
	} else if present {
		name, present, err := lookupFileString(current, []string{"connections", reference, "environment_variable"})
		if err != nil {
			return false, err
		}
		if present {
			inheritedEnvironment = &name
		}
	}
	convert := func(target, effective settingsFile, path []string, scope string) error {
		current, err := document.settings()
		if err != nil {
			return fmt.Errorf("%s: %w", scope, err)
		}
		if definitions, present := current["connections"]; present {
			effective["connections"] = definitions
		}
		if reference, present := target["connection"]; present {
			effective["connection"] = reference
		}
		conversion, err := convertMainConnection(effective, selection, inheritedEnvironment)
		if err != nil {
			return fmt.Errorf("%s: %w", scope, err)
		}
		if conversion != nil {
			if err := applyConnectionConversion(document, path, conversion); err != nil {
				return fmt.Errorf("%s: %w", scope, err)
			}
			changed = true
		}
		return nil
	}
	convertReviewer := func(target, agent, declared settingsFile, path []string, scope string) error {
		if !hasLegacyAccess(declared) {
			return nil
		}
		base := maps.Clone(agent)
		// Explicit Supervisor provider selection did not inherit capability
		// overrides. Endpoint/provider inheritance itself remains field-wise.
		provider, _, err := lookupFileString(declared, []string{"provider_override"})
		if err != nil {
			return err
		}
		mainProvider, _, err := lookupFileString(agent, []string{"provider_override"})
		if err != nil {
			return err
		}
		if provider != "" && mainProvider != "" && !strings.EqualFold(provider, mainProvider) {
			delete(base, "provider_capabilities")
		}
		if _, endpoint, err := lookupFileString(declared, []string{"openai_base_url"}); err != nil {
			return err
		} else if endpoint {
			delete(base, "provider_capabilities")
		}
		effective, err := inheritedLegacyAccess(base, declared)
		if err != nil {
			return fmt.Errorf("%s: %w", scope, err)
		}
		return convert(target, effective, path, scope)
	}
	if err := convertReviewer(reviewer, mainAccess, reviewerAccess, []string{"reviewer"}, "reviewer"); err != nil {
		return false, err
	}
	roles, _, err := lookupFileTable(raw, []string{"subagents"})
	if err != nil {
		return false, err
	}
	for _, name := range slices.Sorted(maps.Keys(roles)) {
		role, _, err := lookupFileTable(roles, []string{name})
		if err != nil {
			return false, err
		}
		effective, err := inheritedLegacyAccess(mainAccess, role)
		if err != nil {
			return false, fmt.Errorf("subagents.%s: %w", name, err)
		}
		if hasLegacyAccess(role) {
			if err := convert(role, maps.Clone(effective), []string{"subagents", name}, "subagents."+name); err != nil {
				return false, err
			}
		}
		roleReviewer, present, err := lookupFileTable(role, []string{"reviewer"})
		if err != nil {
			return false, err
		}
		declared, err := inheritedLegacyAccess(reviewerAccess, roleReviewer)
		if err != nil {
			return false, err
		}
		if hasLegacyAccess(declared) {
			if !present {
				roleReviewer = settingsFile{}
			}
			if err := convertReviewer(roleReviewer, effective, declared, []string{"subagents", name, "reviewer"}, "subagents."+name+".reviewer"); err != nil {
				return false, err
			}
		}
	}
	return changed, nil
}

func applyConnectionConversion(document *settingsDocument, scope []string, conversion *connectionConversion) error {
	if conversion.definition != nil {
		path := []string{"connections", string(conversion.definition.id)}
		if err := document.set(path, conversion.definition.settings); err != nil {
			return err
		}
	}
	referencePath := append(slices.Clone(scope), "connection")
	current, present := document.get(referencePath)
	if !present {
		if err := document.set(referencePath, string(conversion.reference)); err != nil {
			return err
		}
	} else if reference, ok := current.(string); !ok || reference != string(conversion.reference) {
		if err := document.set(referencePath, string(conversion.reference)); err != nil {
			return err
		}
	}
	for _, key := range legacyAccessKeys {
		path := append(slices.Clone(scope), key)
		if err := document.delete(path); err != nil {
			return err
		}
	}
	return nil
}

func convertMainConnection(raw settingsFile, selection *LegacyConnectionAuth, inheritedEnvironment *string) (*connectionConversion, error) {
	_, providerPresent := raw["provider_override"]
	_, endpointPresent := raw["openai_base_url"]
	_, capabilitiesPresent := raw["provider_capabilities"]
	legacy := providerPresent || endpointPresent || capabilitiesPresent
	_, referencePresent := raw["connection"]
	_, connectionsPresent := raw["connections"]
	if !legacy && (selection == nil || referencePresent || connectionsPresent) {
		return nil, nil
	}
	provider, _, err := lookupFileString(raw, []string{"provider_override"})
	if err != nil {
		return nil, err
	}
	if provider != "" && strings.ToLower(provider) != "openai" {
		return nil, fmt.Errorf("provider_override %q cannot be converted to a supported connection; configure a named connection manually", provider)
	}
	if selection == nil {
		return nil, fmt.Errorf("provider_override/openai_base_url: authentication selection is unknown; configure a named connection explicitly")
	}
	existingConnections, err := readConnectionDefinitions(raw)
	if err != nil {
		return nil, err
	}
	connection := ProviderConnection{Protocol: ConnectionResponses}
	switch *selection {
	case LegacyConnectionAnonymous, LegacyConnectionAPIKey:
		endpoint, present, err := lookupFileString(raw, []string{"openai_base_url"})
		if err != nil {
			return nil, err
		}
		if !present {
			endpoint = DefaultOpenAIResponsesEndpoint
		}
		connection.Endpoint = &endpoint
		if *selection == LegacyConnectionAPIKey {
			reference, explicit, err := lookupFileString(raw, []string{"connection"})
			if err != nil {
				return nil, err
			}
			connection.EnvironmentVariable = inheritedEnvironment
			if explicit {
				connection.EnvironmentVariable = existingConnections[ConnectionID(reference)].EnvironmentVariable
			}
			if connection.EnvironmentVariable == nil {
				return nil, fmt.Errorf("API-backed access needs an explicit connections.<id>.environment_variable and connection reference; replace provider_override/openai_base_url manually, then restart")
			}
		}
	case LegacyConnectionOAuth:
		connection.Protocol = ConnectionChatGPT
		if endpoint, present, err := lookupFileString(raw, []string{"openai_base_url"}); err != nil {
			return nil, err
		} else if present {
			return nil, fmt.Errorf("openai_base_url %q conflicts with subscription authentication; configure a named connection manually", endpoint)
		}
	default:
		return nil, fmt.Errorf("unsupported old authentication selection %q", *selection)
	}
	connection.Capabilities, err = readConnectionCapabilities(raw)
	if err != nil {
		return nil, err
	}
	if err := connection.Validate(); err != nil {
		return nil, err
	}
	id := SuggestConnectionID(connection.Protocol, existingConnections)
	for _, existing := range slices.Sorted(maps.Keys(existingConnections)) {
		if equalConnection(existingConnections[existing], connection) {
			id = existing
			break
		}
	}
	if reference, present, err := lookupFileStringAllowEmpty(raw, []string{"connection"}); err != nil {
		return nil, err
	} else if present {
		existingID, err := ParseConnectionID(reference)
		if err != nil {
			return nil, err
		}
		existing, found := existingConnections[existingID]
		if !found || !equalConnection(existing, connection) {
			return nil, fmt.Errorf("connection %q conflicts with old provider-access settings; select one definition manually", reference)
		}
		id = existingID
	}
	var definition *connectionDefinition
	if _, present := existingConnections[id]; !present {
		settings := connectionSettingsTable(connection)
		if capabilitiesPresent {
			settings["provider_capabilities"] = raw["provider_capabilities"]
		}
		definition = &connectionDefinition{id: id, settings: settings}
	}
	return &connectionConversion{reference: id, definition: definition}, nil
}

func equalConnection(a, b ProviderConnection) bool {
	return a.Protocol == b.Protocol && equalOptionalString(a.Endpoint, b.Endpoint) &&
		equalOptionalString(a.EnvironmentVariable, b.EnvironmentVariable) && a.Capabilities == b.Capabilities
}

func equalOptionalString(a, b *string) bool {
	return a == b || a != nil && b != nil && *a == *b
}

func SuggestConnectionID(protocol ConnectionProtocol, connections map[ConnectionID]ProviderConnection) ConnectionID {
	prefix := "openai"
	if protocol == ConnectionChatGPT {
		prefix = "chatgpt"
	}
	for number := 1; ; number++ {
		id := ConnectionID(fmt.Sprintf("%s-%d", prefix, number))
		if _, exists := connections[id]; !exists {
			return id
		}
	}
}
