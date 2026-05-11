package authstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"builder/server/auth"
	"builder/shared/config"
	"builder/shared/serverapi"
)

const usageBaseURL = "https://chatgpt.com/backend-api"

type UsagePayloadFetcher func(ctx context.Context, baseURL string, state auth.State) (usagePayload, error)

var DefaultUsagePayloadFetcher UsagePayloadFetcher = fetchUsagePayload

type Service struct {
	manager  *auth.Manager
	settings config.Settings
	fetcher  UsagePayloadFetcher
}

func NewService(manager *auth.Manager, settings config.Settings) *Service {
	return &Service{manager: manager, settings: settings, fetcher: DefaultUsagePayloadFetcher}
}

func (s *Service) WithUsagePayloadFetcher(fetcher UsagePayloadFetcher) *Service {
	if s != nil && fetcher != nil {
		s.fetcher = fetcher
	}
	return s
}

func (s *Service) GetAuthStatus(ctx context.Context, req serverapi.AuthStatusRequest) (serverapi.AuthStatusResponse, error) {
	_ = req
	state := auth.EmptyState()
	authStateErr := error(nil)
	settings := config.Settings{}
	if s != nil && s.manager != nil {
		loaded, loadErr := s.manager.Load(ctx)
		if loadErr != nil {
			authStateErr = loadErr
		} else {
			state = loaded
			resolved, resolveErr := s.manager.CurrentState(ctx)
			if resolveErr == nil {
				state = resolved
			} else {
				authStateErr = resolveErr
			}
		}
	}
	if s != nil {
		settings = s.settings
	}
	resp := serverapi.AuthStatusResponse{
		Auth:         authInfo(state, settings, authStateErr),
		Subscription: s.subscriptionStatus(ctx, settings, state, authStateErr),
	}
	if authStateErr != nil {
		resp.Warning = "auth: " + authStateErr.Error()
	}
	return resp, nil
}

func authInfo(state auth.State, settings config.Settings, statusErr error) serverapi.AuthStatusInfo {
	if statusErr != nil && !state.IsConfigured() {
		return serverapi.AuthStatusInfo{Summary: "Auth unavailable", Details: []string{statusErr.Error()}, Visible: true}
	}
	details := make([]string, 0, 2)
	baseURL := strings.TrimSpace(settings.OpenAIBaseURL)
	if baseURL != "" && !isOfficialChatGPTBaseURL(baseURL) {
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
		return serverapi.AuthStatusInfo{Summary: summary, Details: details, Visible: true}
	case auth.MethodAPIKey:
		summary := auth.MaskedAPIKeySummary(state.Method.APIKey)
		if provider := providerLabel(state, settings); provider != "" {
			details = append(details, provider)
		}
		if pref := envPreferenceLabel(state.EnvAPIKeyPreference); pref != "" {
			details = append(details, pref)
		}
		if statusErr != nil {
			details = append(details, statusErr.Error())
		}
		return serverapi.AuthStatusInfo{Summary: summary, Details: details, Visible: true}
	default:
		if statusErr != nil {
			return serverapi.AuthStatusInfo{Summary: "Auth unavailable", Details: []string{statusErr.Error()}, Visible: true}
		}
		return serverapi.AuthStatusInfo{Summary: "No Auth", Visible: true}
	}
}

func (s *Service) subscriptionStatus(ctx context.Context, settings config.Settings, state auth.State, authStateErr error) serverapi.AuthSubscriptionInfo {
	if !shouldFetchSubscriptionUsage(settings, state) {
		return serverapi.AuthSubscriptionInfo{}
	}
	if authStateErr != nil {
		errText := authStateErr.Error()
		return serverapi.AuthSubscriptionInfo{Applicable: true, Summary: "Subscription unavailable: " + errText, Error: errText}
	}
	fetcher := fetchUsagePayload
	if s != nil && s.fetcher != nil {
		fetcher = s.fetcher
	}
	payload, err := fetcher(ctx, usageBaseURL, state)
	if err != nil {
		errText := err.Error()
		return serverapi.AuthSubscriptionInfo{Applicable: true, Summary: "Subscription unavailable: " + errText, Error: errText}
	}
	return serverapi.AuthSubscriptionInfo{
		Applicable: true,
		Summary:    subscriptionPlanSummary(payload.PlanType),
		Windows:    usageWindowsByLabel(payload),
	}
}

func shouldFetchSubscriptionUsage(settings config.Settings, state auth.State) bool {
	if state.Method.Type != auth.MethodOAuth || state.Method.OAuth == nil {
		return false
	}
	if strings.TrimSpace(settings.ProviderOverride) != "" {
		return false
	}
	if baseURL := strings.TrimSpace(settings.OpenAIBaseURL); baseURL != "" && !isOfficialChatGPTBaseURL(baseURL) {
		return false
	}
	return true
}

func isOfficialChatGPTBaseURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host == "chatgpt.com" || host == "chat.openai.com"
}

func providerLabel(state auth.State, settings config.Settings) string {
	if provider := strings.TrimSpace(settings.ProviderOverride); provider != "" {
		return provider
	}
	if baseURL := strings.TrimSpace(settings.OpenAIBaseURL); baseURL != "" {
		return filepath.ToSlash(baseURL)
	}
	if state.Method.Type == auth.MethodAPIKey {
		return "OpenAI"
	}
	return ""
}

func envPreferenceLabel(pref auth.EnvAPIKeyPreference) string {
	switch pref {
	case auth.EnvAPIKeyPreferencePreferSaved:
		return "saved auth preferred"
	case auth.EnvAPIKeyPreferencePreferEnv:
		return "OPENAI_API_KEY preferred"
	default:
		return ""
	}
}

func subscriptionPlanSummary(plan string) string {
	trimmed := strings.TrimSpace(plan)
	if trimmed == "" {
		return "Subscription"
	}
	normalized := strings.ToLower(trimmed)
	return strings.ToUpper(normalized[:1]) + normalized[1:] + " subscription"
}

type usagePayload struct {
	PlanType             string             `json:"plan_type"`
	RateLimit            *usageRateLimit    `json:"rate_limit"`
	AdditionalRateLimits []usageExtraBucket `json:"additional_rate_limits"`
}

type UsagePayload = usagePayload

type usageExtraBucket struct {
	MeteredFeature string          `json:"metered_feature"`
	LimitName      string          `json:"limit_name"`
	RateLimit      *usageRateLimit `json:"rate_limit"`
}

type usageRateLimit struct {
	PrimaryWindow   *usageWindow `json:"primary_window"`
	SecondaryWindow *usageWindow `json:"secondary_window"`
}

type usageWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int     `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

func fetchUsagePayload(ctx context.Context, baseURL string, state auth.State) (usagePayload, error) {
	authorization, err := state.Method.AuthHeaderValue()
	if err != nil {
		return usagePayload{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/wham/usage", nil)
	if err != nil {
		return usagePayload{}, err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("User-Agent", "builder/dev")
	if state.Method.OAuth != nil {
		if accountID := strings.TrimSpace(state.Method.OAuth.AccountID); accountID != "" {
			request.Header.Set("ChatGPT-Account-Id", accountID)
		}
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return usagePayload{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return usagePayload{}, fmt.Errorf("usage request failed: %s", response.Status)
	}
	var payload usagePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return usagePayload{}, fmt.Errorf("decode usage response: %w", err)
	}
	return payload, nil
}

func usageWindowsByLabel(payload usagePayload) []serverapi.AuthSubscriptionWindow {
	type orderedWindow struct {
		window        serverapi.AuthSubscriptionWindow
		durationSecs  int
		discoveryRank int
	}
	qualifierCounts := map[string]int{}
	ordered := make([]orderedWindow, 0, 2+len(payload.AdditionalRateLimits)*2)
	discoveryRank := 0
	addWindow := func(window *usageWindow, qualifier string) {
		if window == nil {
			return
		}
		label := limitDuration(window.LimitWindowSeconds / 60)
		if label == "" {
			return
		}
		snapshot := serverapi.AuthSubscriptionWindow{
			Label:       label,
			Qualifier:   qualifier,
			UsedPercent: window.UsedPercent,
		}
		if window.ResetAt > 0 {
			snapshot.ResetAt = time.Unix(window.ResetAt, 0).UTC()
		}
		ordered = append(ordered, orderedWindow{window: snapshot, durationSecs: window.LimitWindowSeconds, discoveryRank: discoveryRank})
		discoveryRank++
	}
	if payload.RateLimit != nil {
		addWindow(payload.RateLimit.PrimaryWindow, "")
		addWindow(payload.RateLimit.SecondaryWindow, "")
	}
	for _, extra := range payload.AdditionalRateLimits {
		if extra.RateLimit == nil {
			continue
		}
		qualifier := usageWindowQualifier(extra, qualifierCounts)
		addWindow(extra.RateLimit.PrimaryWindow, qualifier)
		addWindow(extra.RateLimit.SecondaryWindow, qualifier)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].durationSecs != ordered[j].durationSecs {
			return ordered[i].durationSecs < ordered[j].durationSecs
		}
		return ordered[i].discoveryRank < ordered[j].discoveryRank
	})
	windows := make([]serverapi.AuthSubscriptionWindow, 0, len(ordered))
	for _, window := range ordered {
		windows = append(windows, window.window)
	}
	return windows
}

func usageWindowQualifier(bucket usageExtraBucket, counts map[string]int) string {
	limitName := strings.TrimSpace(bucket.LimitName)
	feature := strings.TrimSpace(bucket.MeteredFeature)
	base := ""
	switch {
	case limitName == "" && feature == "":
		base = "extra"
	case limitName == "":
		base = feature
	case feature == "" || strings.EqualFold(limitName, feature):
		base = limitName
	default:
		base = limitName + " / " + feature
	}
	counts[base]++
	if counts[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s #%d", base, counts[base])
}

func limitDuration(windowMinutes int) string {
	const minutesPerHour = 60
	const minutesPerDay = 24 * minutesPerHour
	const minutesPerWeek = 7 * minutesPerDay
	const minutesPerMonth = 30 * minutesPerDay
	const minutesPerYear = 365 * minutesPerDay
	const roundingBiasMinutes = 3

	if windowMinutes < 0 {
		windowMinutes = 0
	}
	if windowMinutes <= minutesPerDay+roundingBiasMinutes {
		hours := (windowMinutes + roundingBiasMinutes) / minutesPerHour
		if hours < 1 {
			hours = 1
		}
		return fmt.Sprintf("%dh", hours)
	}
	if windowMinutes <= minutesPerWeek+roundingBiasMinutes {
		return "weekly"
	}
	if windowMinutes <= minutesPerMonth+roundingBiasMinutes {
		return "monthly"
	}
	if windowMinutes < minutesPerYear-roundingBiasMinutes {
		days := (windowMinutes + minutesPerDay/2) / minutesPerDay
		if days < 31 {
			days = 31
		}
		return fmt.Sprintf("%dd", days)
	}
	return "annual"
}

var _ serverapi.AuthStatusService = (*Service)(nil)
