package protoapi_test

import (
	"testing"

	"core/shared/config"
	"core/shared/protoapi"
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
