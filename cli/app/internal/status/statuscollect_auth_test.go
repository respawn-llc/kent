package status

import (
	"errors"
	"net/url"
	"testing"
	"time"

	authpb "core/shared/protoapi/gen/kent/api/auth"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAuthStageFromResponseProjectsTypedMethods(t *testing.T) {
	for _, method := range []authpb.AuthMethod{
		authpb.AuthMethod_AUTH_METHOD_API_KEY,
		authpb.AuthMethod_AUTH_METHOD_OAUTH,
		authpb.AuthMethod_AUTH_METHOD_NONE,
	} {
		result := AuthStageFromResponse(&authpb.Status{
			Resolution: &authpb.StatusResolution{
				Resolution: &authpb.StatusResolution_Known{Known: &authpb.StatusFacts{
					ConnectionId: "work",
					Method:       method,
					Provider:     &authpb.ProviderFacts{Kind: authpb.ProviderKind_PROVIDER_KIND_OPENAI},
				}},
			},
		})
		if result.Auth.Method != method || !result.Auth.Visible || result.Auth.Unavailable {
			t.Fatalf("method %v projection = %+v", method, result)
		}
	}
}

func TestAuthStageShowsGrokQuotaAlongsideAuthDetails(t *testing.T) {
	connection := &authpb.ConnectionFacts{ConnectionId: "grok", Method: authpb.AuthMethod_AUTH_METHOD_OAUTH}
	source := authpb.SubscriptionQuotaSource_SUBSCRIPTION_QUOTA_SOURCE_GROK
	buildUsedPercent := float64(67)
	chatUsedPercent := float64(19)
	result := AuthStageFromResponse(&authpb.Status{
		Resolution: &authpb.StatusResolution{Resolution: &authpb.StatusResolution_Known{
			Known: &authpb.StatusFacts{
				ConnectionId: "grok", Method: connection.Method, SessionConnection: connection,
				Provider: &authpb.ProviderFacts{Kind: authpb.ProviderKind_PROVIDER_KIND_CONFIGURED_PROVIDER, Identifier: "grok"},
			},
		}},
		Subscription: &authpb.SubscriptionFacts{
			Applicable:  true,
			QuotaSource: &source,
			Windows: []*authpb.SubscriptionWindowFacts{{
				DurationSeconds: 7 * 24 * 60 * 60,
				UsedPercent:     86,
				GrokContributions: &authpb.GrokUsageContributions{
					BuildUsedPercent: &buildUsedPercent,
					ChatUsedPercent:  &chatUsedPercent,
				},
			}},
		},
	})
	if !result.Auth.Visible || result.Auth.Connection != connection ||
		!result.Subscription.Applicable || result.Subscription.QuotaSource == nil ||
		*result.Subscription.QuotaSource != source || len(result.Subscription.Windows) != 1 {
		t.Fatalf("Grok Auth projection = %+v", result)
	}
	window := result.Subscription.Windows[0]
	if window.Label != "weekly" || window.UsedPercent != 86 || window.GrokContributions == nil ||
		window.GrokContributions.BuildUsedPercent == nil || *window.GrokContributions.BuildUsedPercent != buildUsedPercent ||
		window.GrokContributions.ChatUsedPercent == nil || *window.GrokContributions.ChatUsedPercent != chatUsedPercent {
		t.Fatalf("Grok quota projection = %+v", window)
	}
}

func TestAuthStageFromResponseProjectsUnavailableAndRetainedOAuth(t *testing.T) {
	unavailable := AuthStageFromResponse(&authpb.Status{
		Resolution: &authpb.StatusResolution{
			Resolution: &authpb.StatusResolution_Unavailable{
				Unavailable: &authpb.StatusFailure{Cause: "permission denied"},
			},
		},
	})
	if !unavailable.Auth.Unavailable || !unavailable.Auth.Visible {
		t.Fatalf("unavailable projection = %+v", unavailable)
	}
	email := "user@example.com"
	refreshFailure := &authpb.StatusFailure{Cause: "refresh failed"}
	retained := AuthStageFromResponse(&authpb.Status{
		Resolution: &authpb.StatusResolution{
			Resolution: &authpb.StatusResolution_Known{Known: &authpb.StatusFacts{
				ConnectionId: "work",
				Method:       authpb.AuthMethod_AUTH_METHOD_OAUTH,
				MethodFacts:  &authpb.StatusFacts_Oauth{Oauth: &authpb.OAuthFacts{Email: &email}},
			}},
			PartialFailure: refreshFailure,
		},
		Subscription: &authpb.SubscriptionFacts{Applicable: true, Failure: refreshFailure},
	})
	if retained.Auth.Method != authpb.AuthMethod_AUTH_METHOD_OAUTH ||
		retained.Auth.Unavailable || retained.Auth.Summary != email ||
		!retained.Subscription.Applicable || retained.Subscription.Error != refreshFailure.Cause {
		t.Fatalf("retained OAuth projection = %+v", retained)
	}
}

func TestAuthProviderDisplayOriginFormatsScopedIPv6(t *testing.T) {
	hostname := "fe80::1%en0"
	rendered := authProviderDisplayOrigin(&authpb.ProviderDisplayOrigin{
		Scheme: "https", Hostname: hostname,
	})
	parsed, err := url.Parse(rendered)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != hostname || parsed.Port() != "" {
		t.Fatalf("scoped IPv6 origin = %q, parsed=%+v, error=%v", rendered, parsed, err)
	}
}

func TestSubscriptionProjectionPreservesDurationAndDuplicateBuckets(t *testing.T) {
	reset := time.Date(2026, time.August, 9, 5, 0, 0, 0, time.UTC)
	vision, images := "vision", "images"
	windows := []*authpb.SubscriptionWindowFacts{
		{Bucket: authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_DEFAULT, DurationSeconds: 5 * 3600, UsedPercent: 10, ResetAt: timestamppb.New(reset)},
		{Bucket: authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_ADDITIONAL, DurationSeconds: 5 * 3600, UsedPercent: 20, LimitName: &vision, MeteredFeature: &images},
		{Bucket: authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_ADDITIONAL, DurationSeconds: 5 * 3600, UsedPercent: 30, LimitName: &vision, MeteredFeature: &images},
		{Bucket: authpb.SubscriptionWindowBucket_SUBSCRIPTION_WINDOW_BUCKET_DEFAULT, DurationSeconds: 90 * 24 * 3600, UsedPercent: 40},
	}
	projected := AuthStageFromResponse(&authpb.Status{
		Subscription: &authpb.SubscriptionFacts{Applicable: true, Windows: windows},
	}).Subscription
	if !projected.Applicable || len(projected.Windows) != len(windows) {
		t.Fatalf("subscription = %+v", projected)
	}
	for i, window := range projected.Windows {
		if window.UsedPercent != windows[i].UsedPercent {
			t.Fatalf("usage %d = %+v", i, window)
		}
	}
	if !projected.Windows[0].ResetAt.Equal(reset) ||
		projected.Windows[0].Label != projected.Windows[1].Label ||
		projected.Windows[0].Label == projected.Windows[3].Label ||
		projected.Windows[1].Qualifier == projected.Windows[2].Qualifier {
		t.Fatalf("window projections = %+v", projected.Windows)
	}
}

func TestSubscriptionProjectionPreservesGrokSourceAndContributionPresence(t *testing.T) {
	source := authpb.SubscriptionQuotaSource_SUBSCRIPTION_QUOTA_SOURCE_GROK
	buildUsedPercent := float64(0)
	projected := AuthStageFromResponse(&authpb.Status{
		Subscription: &authpb.SubscriptionFacts{
			Applicable:  true,
			QuotaSource: &source,
			Windows: []*authpb.SubscriptionWindowFacts{{
				DurationSeconds: 7 * 24 * 60 * 60,
				GrokContributions: &authpb.GrokUsageContributions{
					BuildUsedPercent: &buildUsedPercent,
				},
			}},
		},
	}).Subscription
	if projected.QuotaSource == nil || *projected.QuotaSource != source || len(projected.Windows) != 1 {
		t.Fatalf("subscription source = %+v", projected)
	}
	contributions := projected.Windows[0].GrokContributions
	if contributions == nil || contributions.BuildUsedPercent == nil || *contributions.BuildUsedPercent != 0 ||
		contributions.ChatUsedPercent != nil {
		t.Fatalf("Grok contributions = %+v", projected.Windows[0])
	}
}

func TestSubscriptionProjectionPreservesUnavailableSignInClassificationAndDiagnostic(t *testing.T) {
	classification := authpb.SubscriptionFailureClassification_SUBSCRIPTION_FAILURE_CLASSIFICATION_UNAVAILABLE_FOR_SIGN_IN_METHOD
	source := authpb.SubscriptionQuotaSource_SUBSCRIPTION_QUOTA_SOURCE_GROK
	const diagnostic = "billing access denied"
	projected := AuthStageFromResponse(&authpb.Status{
		Subscription: &authpb.SubscriptionFacts{
			Applicable:            true,
			QuotaSource:           &source,
			FailureClassification: &classification,
			Failure:               &authpb.StatusFailure{Cause: diagnostic},
		},
	}).Subscription
	if projected.FailureClassification == nil || *projected.FailureClassification != classification ||
		projected.Error != diagnostic {
		t.Fatalf("subscription failure projection = %+v", projected)
	}
}

func TestUnavailableAuthStageKeepsSubscriptionApplicabilityUnknown(t *testing.T) {
	result := UnavailableAuthStage(errors.New("connection lost"))
	if result.Subscription.Applicable || result.Subscription.Summary != "" {
		t.Fatalf("RPC failure invented subscription applicability: %+v", result)
	}
}
