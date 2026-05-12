package statuscollect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	appstatus "builder/cli/app/internal/status"
	"builder/shared/auth"
	"builder/shared/config"
	"builder/shared/serverapi"
)

type UsagePayload struct {
	PlanType             string             `json:"plan_type"`
	RateLimit            *UsageRateLimit    `json:"rate_limit"`
	AdditionalRateLimits []UsageExtraBucket `json:"additional_rate_limits"`
}

type UsageExtraBucket struct {
	MeteredFeature string          `json:"metered_feature"`
	LimitName      string          `json:"limit_name"`
	RateLimit      *UsageRateLimit `json:"rate_limit"`
}

type UsageRateLimit struct {
	PrimaryWindow   *UsageWindow `json:"primary_window"`
	SecondaryWindow *UsageWindow `json:"secondary_window"`
}

type UsageWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int     `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

func CollectSubscriptionStatus(ctx context.Context, req appstatus.Request, state auth.State, authStateErr error, fetcher UsagePayloadFetcher, baseURL string) appstatus.SubscriptionInfo {
	if !ShouldFetchSubscriptionUsage(req.Settings, state) {
		return appstatus.SubscriptionInfo{}
	}
	if authStateErr != nil {
		errText := authStateErr.Error()
		return appstatus.SubscriptionInfo{Applicable: true, Summary: "Subscription unavailable: " + errText, Error: errText}
	}
	if fetcher == nil {
		fetcher = FetchUsagePayload
	}
	payload, err := fetcher(ctx, strings.TrimSpace(baseURL), state)
	if err != nil {
		errText := err.Error()
		return appstatus.SubscriptionInfo{Applicable: true, Summary: "Subscription unavailable: " + errText, Error: errText}
	}
	windows := UsageWindowsByLabel(payload)
	summary := SubscriptionPlanSummary(payload.PlanType)
	return appstatus.SubscriptionInfo{Applicable: true, Summary: summary, Windows: windows}
}

func SubscriptionWindowsFromAPI(windows []serverapi.AuthSubscriptionWindow) []appstatus.SubscriptionWindow {
	if len(windows) == 0 {
		return nil
	}
	result := make([]appstatus.SubscriptionWindow, 0, len(windows))
	for _, window := range windows {
		result = append(result, appstatus.SubscriptionWindow{
			Label:       strings.TrimSpace(window.Label),
			Qualifier:   strings.TrimSpace(window.Qualifier),
			UsedPercent: window.UsedPercent,
			ResetAt:     window.ResetAt,
		})
	}
	return result
}

func ShouldFetchSubscriptionUsage(settings config.Settings, state auth.State) bool {
	if state.Method.Type != auth.MethodOAuth || state.Method.OAuth == nil {
		return false
	}
	if strings.TrimSpace(settings.ProviderOverride) != "" {
		return false
	}
	if baseURL := strings.TrimSpace(settings.OpenAIBaseURL); baseURL != "" && !IsOfficialChatGPTBaseURL(baseURL) {
		return false
	}
	return true
}

func SubscriptionPlanSummary(plan string) string {
	trimmed := strings.TrimSpace(plan)
	if trimmed == "" {
		return "Subscription"
	}
	normalized := strings.ToLower(trimmed)
	return strings.ToUpper(normalized[:1]) + normalized[1:] + " subscription"
}

func FetchUsagePayload(ctx context.Context, baseURL string, state auth.State) (UsagePayload, error) {
	authorization, err := state.Method.AuthHeaderValue()
	if err != nil {
		return UsagePayload{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/wham/usage", nil)
	if err != nil {
		return UsagePayload{}, err
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
		return UsagePayload{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return UsagePayload{}, fmt.Errorf("usage request failed: %s", response.Status)
	}
	var payload UsagePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return UsagePayload{}, fmt.Errorf("decode usage response: %w", err)
	}
	return payload, nil
}

func UsageWindowsByLabel(payload UsagePayload) []appstatus.SubscriptionWindow {
	type orderedWindow struct {
		window        appstatus.SubscriptionWindow
		durationSecs  int
		discoveryRank int
	}
	qualifierCounts := map[string]int{}
	ordered := make([]orderedWindow, 0, 2+len(payload.AdditionalRateLimits)*2)
	discoveryRank := 0
	addWindow := func(window *UsageWindow, qualifier string) {
		if window == nil {
			return
		}
		label := LimitDuration(window.LimitWindowSeconds / 60)
		if label == "" {
			return
		}
		snapshot := appstatus.SubscriptionWindow{
			Label:       label,
			Qualifier:   qualifier,
			UsedPercent: window.UsedPercent,
		}
		if window.ResetAt > 0 {
			snapshot.ResetAt = time.Unix(window.ResetAt, 0).UTC()
		}
		ordered = append(ordered, orderedWindow{
			window:        snapshot,
			durationSecs:  window.LimitWindowSeconds,
			discoveryRank: discoveryRank,
		})
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
		qualifier := UsageWindowQualifier(extra, qualifierCounts)
		addWindow(extra.RateLimit.PrimaryWindow, qualifier)
		addWindow(extra.RateLimit.SecondaryWindow, qualifier)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].durationSecs != ordered[j].durationSecs {
			return ordered[i].durationSecs < ordered[j].durationSecs
		}
		return ordered[i].discoveryRank < ordered[j].discoveryRank
	})
	windows := make([]appstatus.SubscriptionWindow, 0, len(ordered))
	for _, window := range ordered {
		windows = append(windows, window.window)
	}
	return windows
}

func UsageWindowQualifier(bucket UsageExtraBucket, counts map[string]int) string {
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

func LimitDuration(windowMinutes int) string {
	const minutesPerHour = 60
	const minutesPerDay = 24 * minutesPerHour
	const minutesPerWeek = 7 * minutesPerDay
	const minutesPerMonth = 30 * minutesPerDay
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
	return "annual"
}
