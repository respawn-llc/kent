package llm

import (
	"encoding/json"
)

// MarshalResponsesWirePayload builds the responses.ResponseNewParams that the
// selected Responses transport would POST, using the production
// buildPayload path, and marshals a request-shape-equivalent diagnostic JSON.
// JSON escaping may differ from the openai-go SDK HTTP body. No HTTP is
// performed. This does not reproduce provider token accounting or
// compaction/context decisions.
//
// This is an operator-only diagnostic seam used by offline inspection tooling to
// capture a request payload for a session without executing a model turn.
//
//   - registration: the actual selected connection registration, independent of
//     effective or locked capabilities; selects provider policy.
//   - request: the provider-DTO request (project from a provider-agnostic Request
//     via RequestAsResponses, the same projection the live ResponsesClient uses).
//   - store: mirrors the transport's Store flag (Responses API persistence),
//     sourced from the session's provider settings.
//   - modelVerbosity: mirrors the transport's ModelVerbosity setting.
//   - mode: the auth mode consumed by the selected provider policy.
//   - capabilities: the effective session capabilities, not transport identity.
func MarshalResponsesWirePayload(registration ProviderVariantRegistration, request ResponsesRequest, store bool, modelVerbosity string, mode OpenAIAuthMode, capabilities ProviderCapabilities) (json.RawMessage, error) {
	transport, err := NewHTTPTransport(nil, registration)
	if err != nil {
		return nil, err
	}
	transport.Store, transport.ModelVerbosity = store, modelVerbosity
	params, err := transport.buildPayload(request, mode, capabilities)
	if err != nil {
		return nil, err
	}
	return json.Marshal(params)
}
