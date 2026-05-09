package statuscollect

import (
	"context"
	"strings"
	"testing"
	"time"

	appstatus "builder/cli/app/internal/status"
	"builder/server/auth"
	"builder/shared/config"
)

func TestCollectorUsesRefreshedOAuthStateForUsageFetch(t *testing.T) {
	now := time.Date(2026, time.January, 1, 10, 0, 0, 0, time.UTC)
	store := auth.NewMemoryStore(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type: auth.MethodOAuth,
			OAuth: &auth.OAuthMethod{
				AccessToken:  "stale-token",
				RefreshToken: "refresh-token",
				TokenType:    "Bearer",
				Expiry:       now.Add(-time.Minute),
				AccountID:    "acct-456",
			},
		},
	})
	refresher := auth.NewOAuthRefresher(nil, func() time.Time { return now }, 30*time.Second)
	refresher.Refresh = func(context.Context, auth.Method) (auth.Method, error) {
		return auth.Method{
			Type: auth.MethodOAuth,
			OAuth: &auth.OAuthMethod{
				AccessToken:  "fresh-token",
				RefreshToken: "refresh-token",
				TokenType:    "Bearer",
				Expiry:       now.Add(time.Hour),
				AccountID:    "acct-456",
			},
		}, nil
	}
	manager := auth.NewManager(store, refresher, func() time.Time { return now.Add(time.Minute) })
	collector := Collector{
		AuthManager:  manager,
		UsageBaseURL: DefaultUsageBaseURL,
		UsagePayloadFetcher: func(_ context.Context, baseURL string, state auth.State) (UsagePayload, error) {
			if baseURL != DefaultUsageBaseURL {
				t.Fatalf("base URL = %q", baseURL)
			}
			authorization, err := state.Method.AuthHeaderValue()
			if err != nil {
				t.Fatalf("auth header value: %v", err)
			}
			if got := authorization; got != "Bearer fresh-token" {
				t.Fatalf("authorization header value = %q", got)
			}
			if got := strings.TrimSpace(state.Method.OAuth.AccountID); got != "acct-456" {
				t.Fatalf("ChatGPT-Account-Id value = %q", got)
			}
			return UsagePayload{PlanType: "pro", RateLimit: &UsageRateLimit{PrimaryWindow: &UsageWindow{UsedPercent: 12.5, LimitWindowSeconds: 18000, ResetAt: 1704069000}}}, nil
		},
	}

	snapshot, err := collector.Collect(context.Background(), appstatus.Request{
		WorkspaceRoot: t.TempDir(),
		Settings:      config.Settings{},
	})
	if err != nil {
		t.Fatalf("collect status: %v", err)
	}
	if !strings.Contains(snapshot.Auth.Summary, "Subscription") {
		t.Fatalf("auth summary = %q", snapshot.Auth.Summary)
	}
	if snapshot.Subscription.Summary != "Pro subscription" {
		t.Fatalf("subscription summary = %q", snapshot.Subscription.Summary)
	}
	if len(snapshot.Subscription.Windows) != 1 || snapshot.Subscription.Windows[0].Label != "5h" {
		t.Fatalf("windows = %#v", snapshot.Subscription.Windows)
	}
}

func TestCollectorPreservesStoredAuthStateWhenRefreshFails(t *testing.T) {
	now := time.Date(2026, time.January, 1, 10, 0, 0, 0, time.UTC)
	store := auth.NewMemoryStore(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type: auth.MethodOAuth,
			OAuth: &auth.OAuthMethod{
				AccessToken:  "stale-token",
				RefreshToken: "refresh-token",
				TokenType:    "Bearer",
				Expiry:       now.Add(-time.Minute),
				AccountID:    "acct-789",
				Email:        "user@example.com",
			},
		},
		EnvAPIKeyPreference: auth.EnvAPIKeyPreferencePreferSaved,
	})
	refresher := auth.NewOAuthRefresher(nil, func() time.Time { return now }, 30*time.Second)
	refresher.Refresh = func(context.Context, auth.Method) (auth.Method, error) {
		return auth.Method{}, auth.ErrOAuthRefreshFailed
	}
	manager := auth.NewManager(store, refresher, func() time.Time { return now.Add(time.Minute) })

	collector := Collector{AuthManager: manager}
	snapshot, err := collector.Collect(context.Background(), appstatus.Request{
		WorkspaceRoot: t.TempDir(),
		Settings:      config.Settings{},
	})
	if err != nil {
		t.Fatalf("collect status: %v", err)
	}
	if !strings.Contains(snapshot.Auth.Summary, "user@example.com") {
		t.Fatalf("auth summary = %q", snapshot.Auth.Summary)
	}
	if !snapshot.Subscription.Applicable {
		t.Fatal("expected subscription section to stay applicable")
	}
	if !strings.Contains(snapshot.Subscription.Summary, auth.ErrOAuthRefreshFailed.Error()) {
		t.Fatalf("subscription summary = %q", snapshot.Subscription.Summary)
	}
	if !strings.Contains(snapshot.CollectorWarning, auth.ErrOAuthRefreshFailed.Error()) {
		t.Fatalf("collector warning = %q", snapshot.CollectorWarning)
	}
}
