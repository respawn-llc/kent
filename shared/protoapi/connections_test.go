package protoapi_test

import (
	"testing"

	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

func TestConnectionCapabilityOverridesSurviveProtoTransport(t *testing.T) {
	endpoint := "http://localhost:1234/v1"
	connection := config.ProviderConnection{
		Protocol: config.ConnectionResponses,
		Endpoint: &endpoint,
		Capabilities: config.ProviderCapabilitiesOverride{
			ProviderID:           "openai-compatible",
			SupportsResponsesAPI: true,
			SupportsFastMode:     true,
		},
	}

	wire := protoapi.ConnectionToProto("local", connection)
	if wire.Capabilities == nil || !wire.Capabilities.SupportsFastMode {
		t.Fatalf("wire capabilities = %+v, want fast-mode support", wire.Capabilities)
	}
	id, received, err := protoapi.ConnectionFromProto(wire)
	if err != nil {
		t.Fatalf("ConnectionFromProto: %v", err)
	}
	if id != "local" || received.Capabilities != connection.Capabilities {
		t.Fatalf("round-trip connection = (%q, %+v), want (%q, %+v)", id, received, "local", connection)
	}
}

func TestConnectionFromProtoPersistsDeepSeekBase(t *testing.T) {
	env := "DEEPSEEK_API_KEY"
	official := "  https://api.deepseek.com/ "
	custom := " https://models.example.test/v1/ "
	for _, test := range []struct {
		name     string
		endpoint *string
		want     *string
	}{
		{name: "omitted"},
		{name: "official", endpoint: &official},
		{name: "custom", endpoint: &custom, want: ptr("https://models.example.test/v1")},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, received, err := protoapi.ConnectionFromProto(&authpb.ConnectionDefinition{
				Id: "deepseek-1", Protocol: authpb.ConnectionProtocol_CONNECTION_PROTOCOL_DEEPSEEK_API_KEY,
				EnvironmentVariable: &env, Endpoint: test.endpoint,
			})
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case test.want == nil && received.Endpoint != nil:
				t.Fatalf("endpoint = %q, want none", *received.Endpoint)
			case test.want != nil && (received.Endpoint == nil || *received.Endpoint != *test.want):
				t.Fatalf("endpoint = %v, want %q", received.Endpoint, *test.want)
			}
		})
	}
	_, _, err := protoapi.ConnectionFromProto(&authpb.ConnectionDefinition{
		Id: "deepseek-1", Protocol: authpb.ConnectionProtocol_CONNECTION_PROTOCOL_DEEPSEEK_API_KEY,
		EnvironmentVariable: &env, Endpoint: ptr("not a url"),
	})
	if err == nil {
		t.Fatal("accepted a non-absolute DeepSeek base")
	}
}

func ptr(value string) *string { return &value }
