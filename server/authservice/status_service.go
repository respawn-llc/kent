package authservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"core/server/authmanager"
	"core/server/httpcompression"
	"core/server/llm"
	"core/server/sessionstore"
	"core/shared/authstatus"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/textutil"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const usageBaseURL = "https://chatgpt.com/backend-api"

type StatusService struct {
	connections *ConnectionResolver
	sessions    session.PersistedSessionResolver
}

func NewStatusService(connections *ConnectionResolver, sessions session.PersistedSessionResolver) *StatusService {
	return &StatusService{connections: connections, sessions: sessions}
}

func (s *StatusService) GetStatus(ctx context.Context, req *authpb.GetStatusRequest) (*authpb.Status, error) {
	var target *string
	if req.Provider != nil {
		target = &req.Provider.ConnectionId
	}
	if req.SessionId != nil {
		record, err := session.ResolvePersistedSessionRecord(ctx, s.sessions, *req.SessionId)
		if err != nil {
			return unavailableStatus(err), nil
		}
		if record.Meta.ConnectionID == nil {
			return unavailableStatus(errors.New("Session has no saved Provider Connection")), nil
		}
		target = textutil.Value(string(*record.Meta.ConnectionID))
	}
	snapshot, err := s.connections.snapshot(ctx, target)
	if err != nil {
		return unavailableStatus(err), nil
	}
	definition := snapshot.connection.Definition
	registration, err := llm.ResolveConnectionVariant(definition)
	if err != nil {
		return nil, err
	}
	capabilities := registration.Variant.Capabilities
	facts := &authpb.StatusFacts{
		ConnectionId: string(snapshot.connection.ID), Method: authstatus.ConnectionAuthMethod(definition),
		Provider: authstatus.ProviderFacts(capabilities.ProviderID, capabilities.IsOpenAIFirstParty, definition),
	}
	if req.SessionId != nil {
		facts.SessionConnection, err = authstatus.SessionConnection(snapshot.settings, &snapshot.connection.ID)
		if err != nil {
			return nil, err
		}
	}
	switch facts.Method {
	case authpb.AuthMethod_AUTH_METHOD_OAUTH:
		oauth := &authpb.OAuthFacts{}
		if snapshot.oauth != nil {
			oauth.AccountId = textutil.OptionalTrimmedString(snapshot.oauth.AccountID)
			oauth.Email = textutil.OptionalTrimmedString(snapshot.oauth.Email)
		}
		facts.MethodFacts = &authpb.StatusFacts_Oauth{Oauth: oauth}
	case authpb.AuthMethod_AUTH_METHOD_API_KEY:
		facts.MethodFacts = &authpb.StatusFacts_ApiKey{ApiKey: &authpb.APIKeyFacts{EnvironmentVariable: *definition.EnvironmentVariable}}
	case authpb.AuthMethod_AUTH_METHOD_NONE:
		facts.MethodFacts = &authpb.StatusFacts_NoAuth{NoAuth: &emptypb.Empty{}}
	}
	resolution := &authpb.StatusResolution{Resolution: &authpb.StatusResolution_Known{Known: facts}}
	if snapshot.failure != nil {
		resolution.PartialFailure = authStatusFailure(snapshot.failure)
	}
	return &authpb.Status{
		Resolution: resolution,
		Subscription: subscriptionStatus(
			ctx, snapshot.oauth, definition.Protocol, req.SkipSubscriptionUsage,
		),
	}, nil
}

func unavailableStatus(err error) *authpb.Status {
	return &authpb.Status{
		Resolution:   &authpb.StatusResolution{Resolution: &authpb.StatusResolution_Unavailable{Unavailable: authStatusFailure(err)}},
		Subscription: &authpb.SubscriptionFacts{},
	}
}

func subscriptionStatus(
	ctx context.Context,
	credential *auth.OAuthMethod,
	protocol config.ConnectionProtocol,
	skipUsage bool,
) *authpb.SubscriptionFacts {
	if credential == nil || skipUsage {
		return &authpb.SubscriptionFacts{}
	}
	switch protocol {
	case config.ConnectionChatGPT:
		return chatGPTSubscriptionStatus(ctx, *credential)
	case config.ConnectionGrokCLIProxy, config.ConnectionGrokOAuthAPI:
		return grokSubscriptionStatus(ctx, *credential)
	default:
		return &authpb.SubscriptionFacts{}
	}
}

func chatGPTSubscriptionStatus(ctx context.Context, credential auth.OAuthMethod) *authpb.SubscriptionFacts {
	payload, err := fetchUsagePayload(ctx, usageBaseURL, credential)
	if err != nil {
		return &authpb.SubscriptionFacts{Applicable: true, Failure: authStatusFailure(err)}
	}
	windows, err := usageWindowFacts(payload)
	if err != nil {
		return &authpb.SubscriptionFacts{Applicable: true, Failure: authStatusFailure(err)}
	}
	return &authpb.SubscriptionFacts{
		Applicable:  true,
		Plan:        textutil.OptionalTrimmedString(payload.PlanType),
		Windows:     windows,
		QuotaSource: textutil.Value(authpb.SubscriptionQuotaSource_SUBSCRIPTION_QUOTA_SOURCE_CHATGPT),
	}
}

func grokSubscriptionStatus(ctx context.Context, credential auth.OAuthMethod) *authpb.SubscriptionFacts {
	payload, err := fetchGrokBillingPayload(ctx, credential)
	if err != nil {
		return grokSubscriptionFailure(err)
	}
	plan, err := grokSubscriptionPlan(payload.SubscriptionTier)
	if err != nil {
		return grokSubscriptionFailure(err)
	}
	windows, err := grokUsageWindowFacts(payload)
	if err != nil {
		return grokSubscriptionFailure(err)
	}
	return &authpb.SubscriptionFacts{
		Applicable:  true,
		Plan:        plan,
		Windows:     windows,
		QuotaSource: textutil.Value(authpb.SubscriptionQuotaSource_SUBSCRIPTION_QUOTA_SOURCE_GROK),
	}
}

func grokSubscriptionFailure(err error) *authpb.SubscriptionFacts {
	facts := &authpb.SubscriptionFacts{
		Applicable:  true,
		Failure:     authStatusFailure(err),
		QuotaSource: textutil.Value(authpb.SubscriptionQuotaSource_SUBSCRIPTION_QUOTA_SOURCE_GROK),
	}
	var responseError *statusHTTPError
	if errors.As(err, &responseError) {
		switch responseError.statusCode {
		case http.StatusNotFound, http.StatusForbidden, http.StatusUnauthorized:
			classification := authpb.SubscriptionFailureClassification_SUBSCRIPTION_FAILURE_CLASSIFICATION_UNAVAILABLE_FOR_SIGN_IN_METHOD
			facts.FailureClassification = &classification
		}
	}
	return facts
}

type usagePayload struct {
	PlanType             string             `json:"plan_type"`
	RateLimit            *usageRateLimit    `json:"rate_limit"`
	AdditionalRateLimits []usageExtraBucket `json:"additional_rate_limits"`
}

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

func fetchUsagePayload(ctx context.Context, baseURL string, credential auth.OAuthMethod) (usagePayload, error) {
	authorization, err := credential.AuthHeaderValue()
	if err != nil {
		return usagePayload{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/wham/usage", nil)
	if err != nil {
		return usagePayload{}, err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("User-Agent", "kent/dev")
	if accountID := strings.TrimSpace(credential.AccountID); accountID != "" {
		request.Header.Set("ChatGPT-Account-Id", accountID)
	}
	return fetchStatusPayload[usagePayload](request, "usage")
}

type grokBillingPayload struct {
	Config           *grokBillingConfig `json:"config"`
	SubscriptionTier *string            `json:"subscriptionTier"`
}

func grokSubscriptionPlan(tier *string) (*string, error) {
	if tier == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*tier)
	if normalized == "" {
		return nil, errors.New("billing response contains a blank subscription tier")
	}
	return &normalized, nil
}

type grokBillingConfig struct {
	CreditUsagePercent *float64           `json:"creditUsagePercent"`
	CurrentPeriod      *grokUsagePeriod   `json:"currentPeriod"`
	ProductUsage       []grokProductUsage `json:"productUsage"`
}

type grokUsagePeriod struct {
	Type  grokUsagePeriodType `json:"type"`
	Start string              `json:"start"`
	End   string              `json:"end"`
}

type grokUsagePeriodType string

const grokUsagePeriodWeekly grokUsagePeriodType = "USAGE_PERIOD_TYPE_WEEKLY"

type grokProductUsage struct {
	Product      grokProduct `json:"product"`
	UsagePercent *float64    `json:"usagePercent"`
}

type grokProduct string

const (
	grokProductBuild grokProduct = "GrokBuild"
	grokProductChat  grokProduct = "GrokChat"
)

func fetchGrokBillingPayload(ctx context.Context, credential auth.OAuthMethod) (grokBillingPayload, error) {
	endpoint, err := grokBillingEndpoint()
	if err != nil {
		return grokBillingPayload{}, err
	}
	authorization, err := credential.AuthHeaderValue()
	if err != nil {
		return grokBillingPayload{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return grokBillingPayload{}, err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("User-Agent", "kent/dev")
	return fetchStatusPayload[grokBillingPayload](request, "billing")
}

func grokBillingEndpoint() (string, error) {
	registration, err := llm.ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionGrokCLIProxy})
	if err != nil {
		return "", fmt.Errorf("resolve Grok billing endpoint: %w", err)
	}
	if registration.Variant.BaseURL == nil {
		return "", errors.New("Grok CLI proxy base URL is not configured")
	}
	return strings.TrimRight(*registration.Variant.BaseURL, "/") + "/billing?format=credits", nil
}

func fetchStatusPayload[T any](request *http.Request, operation string) (T, error) {
	var zero T
	response, err := httpcompression.NewClient(&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return zero, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var errorPayload statusErrorPayload
		decodeErr := json.NewDecoder(response.Body).Decode(&errorPayload)
		closeErr := response.Body.Close()
		diagnostic := errorPayload.Error
		if diagnostic != nil {
			normalized := strings.TrimSpace(*diagnostic)
			diagnostic = &normalized
		}
		statusErr := &statusHTTPError{
			operation:  operation,
			status:     response.Status,
			statusCode: response.StatusCode,
			diagnostic: diagnostic,
		}
		if errors.Is(decodeErr, io.EOF) {
			decodeErr = nil
		} else if decodeErr != nil {
			decodeErr = fmt.Errorf("decode %s error response: %w", operation, decodeErr)
		}
		return zero, errors.Join(statusErr, decodeErr, closeErr)
	}
	var payload T
	decodeErr := json.NewDecoder(response.Body).Decode(&payload)
	closeErr := response.Body.Close()
	if decodeErr != nil {
		return zero, errors.Join(fmt.Errorf("decode %s response: %w", operation, decodeErr), closeErr)
	}
	if closeErr != nil {
		return zero, fmt.Errorf("close %s response: %w", operation, closeErr)
	}
	return payload, nil
}

type statusErrorPayload struct {
	Error *string `json:"error"`
}

type statusHTTPError struct {
	operation  string
	status     string
	statusCode int
	diagnostic *string
}

func (e *statusHTTPError) Error() string {
	message := fmt.Sprintf("%s request failed: %s", e.operation, e.status)
	if e.diagnostic == nil {
		return message
	}
	if *e.diagnostic == "" {
		return message + ": provider returned a blank error diagnostic"
	}
	return message + ": " + *e.diagnostic
}

func grokUsageWindowFacts(payload grokBillingPayload) ([]*authpb.SubscriptionWindowFacts, error) {
	if payload.Config == nil || payload.Config.CreditUsagePercent == nil {
		return nil, nil
	}
	usedPercent := *payload.Config.CreditUsagePercent
	if math.IsNaN(usedPercent) || math.IsInf(usedPercent, 0) || usedPercent < 0 || usedPercent > 100 {
		return nil, fmt.Errorf("billing usage percentage is outside 0–100: %v", usedPercent)
	}
	contributions, err := grokUsageContributions(payload.Config.ProductUsage)
	if err != nil {
		return nil, err
	}
	durationSeconds := uint32(7 * 24 * 60 * 60)
	var resetAt *timestamppb.Timestamp
	if period := payload.Config.CurrentPeriod; period != nil {
		start, startErr := time.Parse(time.RFC3339, strings.TrimSpace(period.Start))
		end, endErr := time.Parse(time.RFC3339, strings.TrimSpace(period.End))
		if startErr == nil && endErr == nil && end.After(start) {
			periodDuration := end.Sub(start) / time.Second
			if periodDuration > 0 && int64(periodDuration) <= int64(^uint32(0)) {
				durationSeconds = uint32(periodDuration)
				if period.Type == grokUsagePeriodWeekly {
					candidate := timestamppb.New(end.UTC())
					if candidate.CheckValid() == nil {
						resetAt = candidate
					}
				}
			}
		}
	}
	window := &authpb.SubscriptionWindowFacts{
		Bucket:            authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_DEFAULT,
		DurationSeconds:   durationSeconds,
		UsedPercent:       usedPercent,
		GrokContributions: contributions,
	}
	if resetAt != nil {
		window.ResetAt = resetAt
	}
	return []*authpb.SubscriptionWindowFacts{window}, nil
}

func grokUsageContributions(products []grokProductUsage) (*authpb.GrokUsageContributions, error) {
	contributions := &authpb.GrokUsageContributions{}
	foundProduct := false
	for _, product := range products {
		var target **float64
		switch product.Product {
		case grokProductBuild:
			target = &contributions.BuildUsedPercent
		case grokProductChat:
			target = &contributions.ChatUsedPercent
		default:
			continue
		}
		foundProduct = true
		if product.UsagePercent == nil {
			continue
		}
		usedPercent := *product.UsagePercent
		if math.IsNaN(usedPercent) || math.IsInf(usedPercent, 0) || usedPercent < 0 || usedPercent > 100 {
			return nil, fmt.Errorf("billing product usage percentage is outside 0–100: %v", usedPercent)
		}
		*target = textutil.Value(usedPercent)
	}
	if !foundProduct {
		return nil, nil
	}
	return contributions, nil
}

func usageWindowFacts(payload usagePayload) ([]*authpb.SubscriptionWindowFacts, error) {
	type orderedWindow struct {
		facts         *authpb.SubscriptionWindowFacts
		discoveryRank int
	}
	ordered := make([]orderedWindow, 0, 2+len(payload.AdditionalRateLimits)*2)
	discoveryRank := 0
	addWindow := func(window *usageWindow, bucket authpb.SubscriptionWindowBucket, limitName, feature string) error {
		if window == nil {
			return nil
		}
		if window.LimitWindowSeconds <= 0 {
			return fmt.Errorf("usage window duration must be positive: %d", window.LimitWindowSeconds)
		}
		facts := &authpb.SubscriptionWindowFacts{
			Bucket:          bucket,
			DurationSeconds: uint32(window.LimitWindowSeconds),
			UsedPercent:     window.UsedPercent,
		}
		if window.ResetAt > 0 {
			facts.ResetAt = timestamppb.New(time.Unix(window.ResetAt, 0).UTC())
		}
		if bucket == authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_ADDITIONAL {
			facts.LimitName = textutil.OptionalTrimmedString(limitName)
			facts.MeteredFeature = textutil.OptionalTrimmedString(feature)
		}
		ordered = append(ordered, orderedWindow{facts: facts, discoveryRank: discoveryRank})
		discoveryRank++
		return nil
	}
	if payload.RateLimit != nil {
		if err := addWindow(payload.RateLimit.PrimaryWindow, authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_DEFAULT, "", ""); err != nil {
			return nil, err
		}
		if err := addWindow(payload.RateLimit.SecondaryWindow, authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_DEFAULT, "", ""); err != nil {
			return nil, err
		}
	}
	for _, extra := range payload.AdditionalRateLimits {
		if extra.RateLimit == nil {
			continue
		}
		if err := addWindow(extra.RateLimit.PrimaryWindow, authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_ADDITIONAL, extra.LimitName, extra.MeteredFeature); err != nil {
			return nil, err
		}
		if err := addWindow(extra.RateLimit.SecondaryWindow, authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_ADDITIONAL, extra.LimitName, extra.MeteredFeature); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].facts.DurationSeconds != ordered[j].facts.DurationSeconds {
			return ordered[i].facts.DurationSeconds < ordered[j].facts.DurationSeconds
		}
		return ordered[i].discoveryRank < ordered[j].discoveryRank
	})
	windows := make([]*authpb.SubscriptionWindowFacts, 0, len(ordered))
	for _, window := range ordered {
		windows = append(windows, window.facts)
	}
	return windows, nil
}

func authStatusFailure(err error) *authpb.StatusFailure {
	return &authpb.StatusFailure{Cause: strings.TrimSpace(err.Error())}
}
