package authstatus

import (
	"net/url"
	"strconv"
	"strings"

	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

func ProviderFacts(providerID string, isOpenAIFirstParty bool, connection config.ProviderConnection) *authpb.ProviderFacts {
	providerID = strings.TrimSpace(providerID)
	if isOpenAIFirstParty {
		return &authpb.ProviderFacts{
			Kind:       authpb.ProviderKind_PROVIDER_KIND_OPENAI,
			Identifier: "openai",
		}
	}
	if providerID != "openai-compatible" {
		return &authpb.ProviderFacts{
			Kind:       authpb.ProviderKind_PROVIDER_KIND_CONFIGURED_PROVIDER,
			Identifier: providerID,
		}
	}
	var origin *authpb.ProviderDisplayOrigin
	if connection.Endpoint != nil {
		origin = providerDisplayOrigin(*connection.Endpoint)
	}
	return &authpb.ProviderFacts{
		Kind:          authpb.ProviderKind_PROVIDER_KIND_OPENAI_COMPATIBLE,
		Identifier:    "openai-compatible",
		DisplayOrigin: origin,
	}
}

func ProviderSelection(settings config.Settings) *authpb.ProviderSelection {
	if settings.Connection == nil {
		return nil
	}
	return &authpb.ProviderSelection{ConnectionId: string(*settings.Connection)}
}

func providerDisplayOrigin(raw string) *authpb.ProviderDisplayOrigin {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" {
		return nil
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return nil
	}
	hostname := strings.TrimSpace(parsed.Hostname())
	if hostname == "" {
		return nil
	}
	origin := &authpb.ProviderDisplayOrigin{Scheme: scheme, Hostname: hostname}
	if port := strings.TrimSpace(parsed.Port()); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil
		}
		origin.Port = &port
	}
	return origin
}
