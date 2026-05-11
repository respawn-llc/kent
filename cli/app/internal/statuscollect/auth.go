package statuscollect

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"

	appstatus "builder/cli/app/internal/status"
	"builder/server/auth"
	"builder/shared/config"
)

type AuthStateLoader interface {
	Load(context.Context) (auth.State, error)
}

type AuthStateResolver interface {
	AuthStateLoader
	CurrentState(context.Context) (auth.State, error)
}

func NormalizeAuthStateResolver(resolver AuthStateResolver) AuthStateResolver {
	if isNilAuthDependency(resolver) {
		return nil
	}
	return resolver
}

func NormalizeAuthStateLoader(loader AuthStateLoader) AuthStateLoader {
	if isNilAuthDependency(loader) {
		return nil
	}
	return loader
}

func isNilAuthDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func AuthInfo(state auth.State, settings config.Settings, statusErr error) appstatus.AuthInfo {
	if statusErr != nil && !state.IsConfigured() {
		return appstatus.AuthInfo{Summary: "Auth unavailable", Details: []string{statusErr.Error()}, Visible: true}
	}
	details := make([]string, 0, 2)
	baseURL := strings.TrimSpace(settings.OpenAIBaseURL)
	if baseURL != "" && !IsOfficialChatGPTBaseURL(baseURL) {
		details = append(details, filepath.ToSlash(baseURL))
	}
	switch state.Method.Type {
	case auth.MethodOAuth:
		summary := "Subscription"
		if state.Method.OAuth != nil && strings.TrimSpace(state.Method.OAuth.Email) != "" {
			summary = strings.TrimSpace(state.Method.OAuth.Email)
		}
		if statusErr != nil {
			details = append(details, statusErr.Error())
		}
		return appstatus.AuthInfo{Summary: summary, Details: details, Visible: true}
	case auth.MethodAPIKey:
		summary := auth.MaskedAPIKeySummary(state.Method.APIKey)
		if provider := ProviderLabel(state, settings); provider != "" {
			details = append(details, provider)
		}
		if pref := EnvPreferenceLabel(state.EnvAPIKeyPreference); pref != "" {
			details = append(details, pref)
		}
		if statusErr != nil {
			details = append(details, statusErr.Error())
		}
		return appstatus.AuthInfo{Summary: summary, Details: details, Visible: true}
	default:
		if statusErr != nil {
			return appstatus.AuthInfo{Summary: "Auth unavailable", Details: []string{statusErr.Error()}, Visible: true}
		}
		return appstatus.AuthInfo{Summary: "No Auth", Visible: true}
	}
}

func AuthCacheIdentity(manager AuthStateLoader) string {
	manager = NormalizeAuthStateLoader(manager)
	if manager == nil {
		return "auth:none"
	}
	state, err := manager.Load(context.Background())
	if err != nil {
		return "auth:error"
	}
	return AuthIdentity(state)
}

func AuthIdentity(state auth.State) string {
	switch state.Method.Type {
	case auth.MethodOAuth:
		oauth := state.Method.OAuth
		if oauth == nil {
			return "oauth"
		}
		parts := []string{
			"oauth",
			strings.TrimSpace(oauth.AccountID),
			strings.TrimSpace(oauth.Email),
		}
		if parts[1] == "" && parts[2] == "" {
			parts = append(parts, OpaqueOAuthIdentity(*oauth))
		}
		return strings.Join(parts, "|")
	case auth.MethodAPIKey:
		return strings.Join([]string{
			"apikey",
			string(state.EnvAPIKeyPreference),
		}, "|")
	default:
		return "auth:none"
	}
}

func OpaqueOAuthIdentity(oauth auth.OAuthMethod) string {
	token := strings.TrimSpace(oauth.RefreshToken)
	if token == "" {
		token = strings.TrimSpace(oauth.AccessToken)
	}
	if token == "" {
		return "opaque"
	}
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("opaque:%x", sum[:8])
}

func ProviderLabel(state auth.State, settings config.Settings) string {
	providerOverride := strings.ToLower(strings.TrimSpace(settings.ProviderOverride))
	if providerOverride != "" {
		return providerOverride
	}
	if state.Method.Type == auth.MethodOAuth {
		return "chatgpt-codex"
	}
	if strings.TrimSpace(settings.OpenAIBaseURL) != "" {
		return "openai-compatible"
	}
	return "openai"
}

func EnvPreferenceLabel(preference auth.EnvAPIKeyPreference) string {
	switch preference {
	case auth.EnvAPIKeyPreferencePreferEnv:
		return "prefer env"
	case auth.EnvAPIKeyPreferencePreferSaved:
		return "prefer saved"
	default:
		return ""
	}
}

func IsOfficialChatGPTBaseURL(baseURL string) bool {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host != "chatgpt.com" && host != "chat.openai.com" {
		return false
	}
	pathValue := strings.TrimRight(strings.TrimSpace(parsed.EscapedPath()), "/")
	return pathValue == "" || pathValue == "/backend-api"
}
