package authservice

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"core/internal/testharness/httpclient"
	"core/server/auth"
	"core/server/session"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/textutil"
)

type failingAuthStatusStore struct {
	err error
}

type statusSessionResolver struct {
	record session.PersistedSessionRecord
	err    error
}

func (s *statusSessionResolver) ResolvePersistedSession(context.Context, string) (session.PersistedSessionRecord, error) {
	return s.record, s.err
}

func TestConnectionStatusReadsCurrentSavedSessionBinding(t *testing.T) {
	id := "123e4567-e89b-42d3-a456-426614174000"
	reader := &statusSessionResolver{record: session.PersistedSessionRecord{
		SessionDir: t.TempDir(),
		Meta:       &session.Meta{SessionID: id, ConnectionID: textutil.Value(config.ConnectionID("local"))},
	}}
	resolver := authServiceResolver(t, nil, func(string) (string, bool) { return "key", true })
	service := NewStatusService(resolver, reader)
	request := &authpb.GetStatusRequest{SessionId: &id, SkipSubscriptionUsage: true}
	for _, tc := range []struct {
		binding config.ConnectionID
		method  authpb.AuthMethod
	}{{"local", authpb.AuthMethod_AUTH_METHOD_NONE}, {"api", authpb.AuthMethod_AUTH_METHOD_API_KEY}} {
		reader.record.Meta.ConnectionID = &tc.binding
		result, err := service.GetStatus(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		connection := result.GetResolution().GetKnown().GetSessionConnection()
		if connection == nil || connection.ConnectionId != string(tc.binding) || connection.Method != tc.method {
			t.Fatalf("current saved binding = %+v", result)
		}
	}
	reader.record.Meta.ConnectionID = nil
	result, err := service.GetStatus(t.Context(), request)
	if err != nil || result.GetResolution().GetUnavailable() == nil {
		t.Fatalf("unbound Session must not use global default: %+v, %v", result, err)
	}
	reader.record.Meta.ConnectionID = textutil.Value(config.ConnectionID("deleted"))
	result, err = service.GetStatus(t.Context(), request)
	if err != nil || result.GetResolution().GetUnavailable() == nil {
		t.Fatalf("deleted Connection must not use global default: %+v, %v", result, err)
	}
	reader.err = errors.New("Session read failed")
	result, err = service.GetStatus(t.Context(), request)
	if err != nil || result.GetResolution().GetUnavailable() == nil {
		t.Fatalf("unreadable Session must surface failure: %+v, %v", result, err)
	}
}

func TestConnectionStatusDoesNotLabelGlobalOrProviderTargetsAsSessions(t *testing.T) {
	service := NewStatusService(authServiceResolver(t, nil, func(string) (string, bool) { return "key", true }), nil)
	for _, request := range []*authpb.GetStatusRequest{
		{SkipSubscriptionUsage: true},
		{Provider: &authpb.ProviderSelection{ConnectionId: "api"}, SkipSubscriptionUsage: true},
	} {
		result, err := service.GetStatus(t.Context(), request)
		if err != nil || result.GetResolution().GetKnown() == nil || result.GetResolution().GetKnown().SessionConnection != nil {
			t.Fatalf("non-Session target must not have Session Connection facts: %+v, %v", result, err)
		}
	}
}

func (s failingAuthStatusStore) Load(context.Context) (auth.State, error) {
	return auth.State{}, s.err
}

func (failingAuthStatusStore) Save(context.Context, auth.State) error {
	return nil
}

func TestConnectionStatusReadsStoredOAuthWithoutRefreshing(t *testing.T) {
	now := time.Now()
	manager := auth.NewManager(auth.NewMemoryStore(auth.State{Connections: map[config.ConnectionID]auth.OAuthMethod{
		"work": {AccessToken: "stale", AccountID: "account", Expiry: textutil.Value(now.Add(-time.Hour))},
	}}), map[config.ConnectionProtocol]*auth.OAuthRefresher{config.ConnectionChatGPT: auth.NewOAuthRefresher(func() time.Time { return now }, time.Minute, func(context.Context, auth.OAuthMethod) (auth.OAuthMethod, error) {
		t.Error("status attempted OAuth refresh")
		return auth.OAuthMethod{}, errors.New("unexpected refresh")
	})})
	status, err := NewStatusService(authServiceResolver(t, manager, nil), nil).GetStatus(t.Context(), &authpb.GetStatusRequest{SkipSubscriptionUsage: true})
	if err != nil {
		t.Fatal(err)
	}
	facts := status.Resolution.GetKnown()
	if facts == nil || facts.ConnectionId != "work" || facts.GetOauth().GetAccountId() != "account" {
		t.Fatalf("saved connection facts = %+v", facts)
	}
}

func TestConnectionStatusSeparatesFailuresAndSelectedConnection(t *testing.T) {
	manager := auth.NewManager(failingAuthStatusStore{err: errors.New("unreadable OAuth store")}, nil)
	service := NewStatusService(authServiceResolver(t, manager, func(name string) (string, bool) { return "secret", name == "SERVER_KEY" }), nil)
	status, err := service.GetStatus(t.Context(), &authpb.GetStatusRequest{SkipSubscriptionUsage: true})
	if err != nil || status.Resolution.GetUnavailable() == nil {
		t.Fatalf("OAuth failure was not reported: %+v, %v", status, err)
	}
	for _, id := range []string{"local", "api"} {
		status, err := service.GetStatus(t.Context(), &authpb.GetStatusRequest{
			Provider: &authpb.ProviderSelection{ConnectionId: id}, SkipSubscriptionUsage: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		facts := status.Resolution.GetKnown()
		if facts == nil || facts.ConnectionId != id || status.Subscription.Applicable {
			t.Fatalf("broken default affected %s: %+v", id, status)
		}
		if id == "api" && facts.GetApiKey().EnvironmentVariable != "SERVER_KEY" {
			t.Fatal("API-key facts must identify the environment reference")
		}
	}
}

func TestFetchUsagePayloadUsesTypedOAuthHeaders(t *testing.T) {
	var authorization, accountHeader, acceptEncoding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		accountHeader = r.Header.Get("ChatGPT-Account-Id")
		acceptEncoding = r.Header.Get("Accept-Encoding")
		payload, _ := json.Marshal(usagePayload{PlanType: "pro"})
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		_, _ = writer.Write(payload)
		_ = writer.Close()
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()

	_, err := fetchUsagePayload(context.Background(), server.URL, auth.OAuthMethod{
		AccessToken: "access-token",
		AccountID:   "acct-1",
	})
	if err != nil {
		t.Fatalf("fetchUsagePayload: %v", err)
	}
	if authorization != "Bearer access-token" || accountHeader != "acct-1" {
		t.Fatalf("headers = authorization %q account %q", authorization, accountHeader)
	}
	if acceptEncoding != "zstd,gzip" {
		t.Fatalf("Accept-Encoding = %q, want zstd,gzip", acceptEncoding)
	}
}

func TestUsageWindowFactsKeepStableDuplicateDurations(t *testing.T) {
	resetAt := time.Date(2026, time.August, 9, 5, 0, 0, 0, time.UTC).Unix()
	windows, err := usageWindowFacts(usagePayload{
		RateLimit: &usageRateLimit{
			PrimaryWindow: &usageWindow{UsedPercent: 10, LimitWindowSeconds: 5 * 3600, ResetAt: resetAt},
		},
		AdditionalRateLimits: []usageExtraBucket{{
			LimitName:      "vision",
			MeteredFeature: "images",
			RateLimit: &usageRateLimit{
				PrimaryWindow: &usageWindow{UsedPercent: 20, LimitWindowSeconds: 5 * 3600, ResetAt: resetAt},
			},
		}},
	})
	if err != nil {
		t.Fatalf("usageWindowFacts: %v", err)
	}
	if len(windows) != 2 ||
		windows[0].Bucket != authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_DEFAULT ||
		windows[1].Bucket != authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_ADDITIONAL ||
		windows[1].LimitName == nil || *windows[1].LimitName != "vision" ||
		windows[1].MeteredFeature == nil || *windows[1].MeteredFeature != "images" {
		t.Fatalf("windows = %+v", windows)
	}
}

func TestUsageWindowFactsRejectsNonPositiveDurations(t *testing.T) {
	_, err := usageWindowFacts(usagePayload{
		RateLimit: &usageRateLimit{
			PrimaryWindow: &usageWindow{UsedPercent: 10},
		},
	})
	if err == nil {
		t.Fatal("non-positive usage duration was accepted")
	}
}

func TestGrokStatusProjectsCurrentWeeklyPeriodBoundary(t *testing.T) {
	service, id := grokStatusTestService(t, config.ConnectionGrokCLIProxy)
	withStatusTransport(t, func(request *http.Request) (*http.Response, error) {
		return statusJSONResponse(http.StatusOK, `{
			"subscriptionTier": "SuperGrok",
			"config": {
				"creditUsagePercent": 86,
				"currentPeriod": {
					"type": "USAGE_PERIOD_TYPE_WEEKLY",
					"start": "2026-10-05T00:00:00Z",
					"end": "2026-10-12T00:00:00Z"
				}
			}
		}`, request), nil
	})
	status, err := service.GetStatus(t.Context(), &authpb.GetStatusRequest{
		Provider: &authpb.ProviderSelection{ConnectionId: id},
	})
	if err != nil {
		t.Fatal(err)
	}
	subscription := status.GetSubscription()
	if subscription.GetPlan() != "SuperGrok" || len(subscription.GetWindows()) != 1 {
		t.Fatalf("Grok subscription = %+v", subscription)
	}
	window := subscription.GetWindows()[0]
	if window.GetDurationSeconds() != 7*24*60*60 || window.GetUsedPercent() != 86 ||
		window.GetResetAt() == nil || !window.GetResetAt().AsTime().Equal(time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Grok quota window = %+v", window)
	}
}

func TestGrokStatusDistinguishesMissingAndBlankSubscriptionTier(t *testing.T) {
	for _, tc := range []struct {
		name        string
		payload     string
		wantFailure bool
	}{
		{
			name:    "missing",
			payload: `{"config":{"creditUsagePercent":0}}`,
		},
		{
			name:    "null",
			payload: `{"subscriptionTier":null,"config":{"creditUsagePercent":0}}`,
		},
		{
			name:        "empty",
			payload:     `{"subscriptionTier":"","config":{"creditUsagePercent":0}}`,
			wantFailure: true,
		},
		{
			name:        "whitespace",
			payload:     `{"subscriptionTier":" \t ","config":{"creditUsagePercent":0}}`,
			wantFailure: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, id := grokStatusTestService(t, config.ConnectionGrokCLIProxy)
			withStatusTransport(t, func(request *http.Request) (*http.Response, error) {
				return statusJSONResponse(http.StatusOK, tc.payload, request), nil
			})
			status, err := service.GetStatus(t.Context(), &authpb.GetStatusRequest{
				Provider: &authpb.ProviderSelection{ConnectionId: id},
			})
			if err != nil {
				t.Fatal(err)
			}

			subscription := status.GetSubscription()
			if tc.wantFailure {
				if subscription.GetFailure() == nil || len(subscription.GetWindows()) != 0 {
					t.Fatalf("invalid tier facts = %+v", subscription)
				}
				return
			}
			if subscription.GetFailure() != nil || subscription.Plan != nil || len(subscription.GetWindows()) != 1 {
				t.Fatalf("absent tier facts = %+v", subscription)
			}
		})
	}
}

func TestGrokStatusOmitsQuotaWindowWhenTotalUsageIsMissing(t *testing.T) {
	service, id := grokStatusTestService(t, config.ConnectionGrokCLIProxy)
	withStatusTransport(t, func(request *http.Request) (*http.Response, error) {
		return statusJSONResponse(http.StatusOK, `{
			"config": {
				"currentPeriod": {
					"type": "USAGE_PERIOD_TYPE_WEEKLY",
					"start": "2026-10-05T00:00:00Z",
					"end": "2026-10-12T00:00:00Z"
				}
			}
		}`, request), nil
	})
	status, err := service.GetStatus(t.Context(), &authpb.GetStatusRequest{
		Provider: &authpb.ProviderSelection{ConnectionId: id},
	})
	if err != nil {
		t.Fatal(err)
	}
	subscription := status.GetSubscription()
	if !subscription.GetApplicable() ||
		subscription.GetQuotaSource() != authpb.SubscriptionQuotaSource_SUBSCRIPTION_QUOTA_SOURCE_GROK ||
		subscription.GetFailure() != nil || len(subscription.GetWindows()) != 0 {
		t.Fatalf("missing Grok total = %+v", subscription)
	}
}

func TestGrokStatusPreservesTotalAndProductContributions(t *testing.T) {
	service, id := grokStatusTestService(t, config.ConnectionGrokCLIProxy)
	withStatusTransport(t, func(request *http.Request) (*http.Response, error) {
		return statusJSONResponse(http.StatusOK, `{
			"config": {
				"creditUsagePercent": 86,
				"productUsage": [
					{"product": "PRODUCT_GROK_BUILD", "usagePercent": 67},
					{"product": "PRODUCT_GROK_CHAT", "usagePercent": 19}
				]
			}
		}`, request), nil
	})
	status, err := service.GetStatus(t.Context(), &authpb.GetStatusRequest{
		Provider: &authpb.ProviderSelection{ConnectionId: id},
	})
	if err != nil {
		t.Fatal(err)
	}
	subscription := status.GetSubscription()
	if len(subscription.GetWindows()) != 1 || subscription.GetWindows()[0].GetUsedPercent() != 86 {
		t.Fatalf("Grok total usage = %+v", subscription)
	}
	contributions := subscription.GetWindows()[0].GetGrokContributions()
	if contributions == nil || contributions.GetBuildUsedPercent() != 67 || contributions.GetChatUsedPercent() != 19 {
		t.Fatalf("Grok product contributions = %+v", subscription.GetWindows()[0])
	}
}

func TestGrokStatusDistinguishesZeroAndMissingProductUsage(t *testing.T) {
	service, id := grokStatusTestService(t, config.ConnectionGrokCLIProxy)
	withStatusTransport(t, func(request *http.Request) (*http.Response, error) {
		return statusJSONResponse(http.StatusOK, `{
			"config": {
				"creditUsagePercent": 0,
				"productUsage": [
					{"product": "PRODUCT_GROK_BUILD", "usagePercent": 0},
					{"product": "PRODUCT_GROK_CHAT"}
				]
			}
		}`, request), nil
	})
	status, err := service.GetStatus(t.Context(), &authpb.GetStatusRequest{
		Provider: &authpb.ProviderSelection{ConnectionId: id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(status.GetSubscription().GetWindows()) != 1 {
		t.Fatalf("Grok quota windows = %+v", status.GetSubscription())
	}
	contributions := status.GetSubscription().GetWindows()[0].GetGrokContributions()
	if contributions == nil || contributions.BuildUsedPercent == nil ||
		*contributions.BuildUsedPercent != 0 || contributions.ChatUsedPercent != nil {
		t.Fatalf("Grok contribution presence = %+v", contributions)
	}
}

func TestGrokBillingFailuresClassifyUnavailableSignInStatuses(t *testing.T) {
	const providerDiagnostic = "Subscription required"
	for _, statusCode := range []int{
		http.StatusNotFound,
		http.StatusForbidden,
		http.StatusUnauthorized,
		http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			service, id := grokStatusTestService(t, config.ConnectionGrokOAuthAPI)
			withStatusTransport(t, func(request *http.Request) (*http.Response, error) {
				return statusJSONResponse(statusCode, `{"error":"Subscription required"}`, request), nil
			})

			status, err := service.GetStatus(t.Context(), &authpb.GetStatusRequest{
				Provider: &authpb.ProviderSelection{ConnectionId: id},
			})
			if err != nil {
				t.Fatal(err)
			}
			subscription := status.GetSubscription()
			if status.GetResolution().GetKnown() == nil || !subscription.GetApplicable() ||
				subscription.GetFailure() == nil ||
				!strings.Contains(subscription.GetFailure().GetCause(), providerDiagnostic) {
				t.Fatalf("Grok HTTP failure facts = %+v", status)
			}
			if statusCode == http.StatusInternalServerError {
				if subscription.FailureClassification != nil {
					t.Fatalf("ordinary provider failure classification = %v", subscription.GetFailureClassification())
				}
			} else if subscription.GetFailureClassification() != authpb.SubscriptionFailureClassification_SUBSCRIPTION_FAILURE_CLASSIFICATION_UNAVAILABLE_FOR_SIGN_IN_METHOD {
				t.Fatalf("HTTP %d classification = %v", statusCode, subscription.GetFailureClassification())
			}
		})
	}
}

func TestGrokBillingOrdinaryFailuresRetainDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name      string
		roundTrip httpclient.RoundTripFunc
	}{
		{
			name: "malformed response",
			roundTrip: func(request *http.Request) (*http.Response, error) {
				return statusJSONResponse(http.StatusOK, `{`, request), nil
			},
		},
		{
			name: "invalid usage percentage",
			roundTrip: func(request *http.Request) (*http.Response, error) {
				return statusJSONResponse(http.StatusOK, `{"config":{"creditUsagePercent":101}}`, request), nil
			},
		},
		{
			name: "transport failure",
			roundTrip: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("controlled transport failure")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, id := grokStatusTestService(t, config.ConnectionGrokCLIProxy)
			requests := 0
			withStatusTransport(t, func(request *http.Request) (*http.Response, error) {
				requests++
				return tc.roundTrip(request)
			})

			status, err := service.GetStatus(t.Context(), &authpb.GetStatusRequest{
				Provider: &authpb.ProviderSelection{ConnectionId: id},
			})
			if err != nil {
				t.Fatal(err)
			}
			subscription := status.GetSubscription()
			if status.GetResolution().GetKnown() == nil || !subscription.GetApplicable() ||
				subscription.GetFailure() == nil || subscription.GetFailure().GetCause() == "" ||
				subscription.FailureClassification != nil || requests != 1 {
				t.Fatalf("ordinary billing failure facts = %+v, requests=%d", status, requests)
			}
		})
	}
}

func grokStatusTestService(t *testing.T, protocol config.ConnectionProtocol) (*StatusService, string) {
	t.Helper()
	id := config.ConnectionID(protocol)
	manager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)
	if err := manager.SaveOAuth(t.Context(), id, auth.OAuthMethod{
		AccessToken: "selected-grok-token",
		AccountID:   "must-not-send-chatgpt-account",
	}); err != nil {
		t.Fatal(err)
	}
	resolver := authServiceResolver(t, manager, nil)
	if err := config.AddProviderConnection(filepath.Join(resolver.root, "config.toml"), id, config.ProviderConnection{
		Protocol: protocol,
	}); err != nil {
		t.Fatal(err)
	}
	return NewStatusService(resolver, nil), string(id)
}

func withStatusTransport(t *testing.T, transport httpclient.RoundTripFunc) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func statusJSONResponse(statusCode int, body string, request *http.Request) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func authStatusTestString(value string) *string {
	return &value
}
