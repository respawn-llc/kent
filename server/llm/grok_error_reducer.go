package llm

import (
	"encoding/json"

	"core/shared/llmerrors"
)

func newGrokErrorReducer(providerID string) ProviderErrorReducer {
	return responsesErrorReducer{
		providerID: providerID, decode: decodeGrokErrorPayload, classify: classifyGrokError,
	}
}

func decodeGrokErrorPayload(data []byte) (responsesErrorPayload, bool) {
	if payload, ok := decodeResponsesErrorPayload(data); ok {
		return payload, true
	}
	var envelope struct {
		Type     string          `json:"type"`
		Code     string          `json:"code"`
		Message  string          `json:"message"`
		Error    json.RawMessage `json:"error"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return responsesErrorPayload{}, false
	}
	if len(envelope.Response) != 0 {
		return decodeGrokErrorPayload(envelope.Response)
	}
	if len(envelope.Error) != 0 {
		var message string
		if err := json.Unmarshal(envelope.Error, &message); err == nil {
			return responsesErrorPayload{Type: envelope.Type, Code: envelope.Code, Message: message}, true
		}
	}
	return responsesErrorPayload{}, false
}

func classifyGrokError(status int, code string) UnifiedErrorCode {
	switch {
	case code == "personal-team-blocked:spending-limit" || status == 402:
		return llmerrors.UnifiedErrorCodeEntitlement
	case status == 426:
		return llmerrors.UnifiedErrorCodeProtocolVersion
	case status == 401 || code == "invalid_token" || code == "token_expired":
		return UnifiedErrorCodeAuthentication
	default:
		return classifyOpenAIUnifiedErrorCode(status, code)
	}
}
