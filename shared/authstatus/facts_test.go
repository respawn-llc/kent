package authstatus

import (
	"reflect"
	"testing"

	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

func TestProviderFactsDropsCredentialBearingURLComponents(t *testing.T) {
	facts := ProviderFacts("openai-compatible", false, config.ProviderConnection{
		Endpoint: testString("https://user:secret@example.com:8443/v1/key?token=secret#fragment"),
	})
	want := &authpb.ProviderDisplayOrigin{
		Scheme:   "https",
		Hostname: "example.com",
		Port:     testString("8443"),
	}
	if facts.Kind != authpb.ProviderKind_PROVIDER_KIND_OPENAI_COMPATIBLE ||
		!reflect.DeepEqual(facts.DisplayOrigin, want) {
		t.Fatalf("provider facts = %+v, want origin %+v", facts, want)
	}
	for _, raw := range []string{
		"relative/path",
		"mailto:user@example.com",
		"://invalid",
		"https://example.com:0",
		"https://example.com:65536",
	} {
		if got := ProviderFacts("openai-compatible", false, config.ProviderConnection{Endpoint: &raw}).DisplayOrigin; got != nil {
			t.Fatalf("display origin for %q = %+v, want nil", raw, got)
		}
	}
}

func TestProviderFactsProjectsCanonicalRuntimeCapabilities(t *testing.T) {
	tests := []struct {
		name               string
		providerID         string
		isOpenAIFirstParty bool
		wantKind           authpb.ProviderKind
		wantIdentifier     string
	}{
		{name: "OpenAI", providerID: "openai", isOpenAIFirstParty: true, wantKind: authpb.ProviderKind_PROVIDER_KIND_OPENAI, wantIdentifier: "openai"},
		{name: "ChatGPT", providerID: "chatgpt-codex", isOpenAIFirstParty: true, wantKind: authpb.ProviderKind_PROVIDER_KIND_OPENAI, wantIdentifier: "openai"},
		{name: "configured provider", providerID: "anthropic", wantKind: authpb.ProviderKind_PROVIDER_KIND_CONFIGURED_PROVIDER, wantIdentifier: "anthropic"},
		{name: "compatible endpoint", providerID: "openai-compatible", wantKind: authpb.ProviderKind_PROVIDER_KIND_OPENAI_COMPATIBLE, wantIdentifier: "openai-compatible"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ProviderFacts(test.providerID, test.isOpenAIFirstParty, config.ProviderConnection{})
			if got.Kind != test.wantKind || got.Identifier != test.wantIdentifier {
				t.Fatalf("ProviderFacts = %+v, want kind %q identifier %q", got, test.wantKind, test.wantIdentifier)
			}
		})
	}
}

func testString(value string) *string {
	return &value
}
