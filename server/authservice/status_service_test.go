package authservice

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func authStatusTestString(value string) *string {
	return &value
}
