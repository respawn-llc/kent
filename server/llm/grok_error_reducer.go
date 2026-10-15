package llm

import (
	"encoding/json"
	"strings"

	"core/shared/config"
	"core/shared/llmerrors"
)

func newGrokErrorReducer(providerID string) ProviderErrorReducer {
	return responsesErrorReducer{
		providerID: providerID, decode: decodeGrokErrorPayload,
		classify: func(status int, code, typ string) UnifiedErrorCode {
			category := classifyGrokError(status, code, typ)
			if category == UnifiedErrorCodeUnknown && status == 402 {
				if providerID == string(config.ConnectionGrokCLIProxy) {
					return UnifiedErrorCodeUsageExhausted
				}
				return llmerrors.UnifiedErrorCodeEntitlement
			}
			return category
		},
	}
}

func decodeGrokErrorPayload(data []byte) (responsesErrorPayload, bool) {
	if payload, ok := decodeResponsesErrorPayload(data); ok {
		return grokWKEPayload(payload), true
	}
	var envelope struct {
		Type     string          `json:"type"`
		Code     json.RawMessage `json:"code"`
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
	var code string
	if len(envelope.Code) != 0 {
		var numeric json.Number
		if err := json.Unmarshal(envelope.Code, &numeric); err != nil {
			if err := json.Unmarshal(envelope.Code, &code); err != nil {
				return responsesErrorPayload{}, false
			}
		}
	}
	if len(envelope.Error) != 0 {
		if payload, ok := decodeGrokErrorPayload(envelope.Error); ok {
			return payload, true
		}
		var message string
		if err := json.Unmarshal(envelope.Error, &message); err == nil {
			return grokWKEPayload(responsesErrorPayload{Type: envelope.Type, Code: code, Message: message}), true
		}
	}
	if code != "" || envelope.Message != "" {
		return grokWKEPayload(responsesErrorPayload{Type: envelope.Type, Code: code, Message: envelope.Message}), true
	}
	return responsesErrorPayload{}, false
}

func grokWKEPayload(payload responsesErrorPayload) responsesErrorPayload {
	var numeric json.Number
	if json.Unmarshal([]byte(payload.Code), &numeric) == nil {
		payload.Code = ""
	}
	if payload.Code != "" {
		return payload
	}
	_, rest, present := strings.Cut(payload.Message, "[WKE=")
	if !present {
		return payload
	}
	tag, _, closed := strings.Cut(rest, "]")
	if closed && tag == "personal-team-blocked:spending-limit" {
		payload.Code = tag
	}
	return payload
}

func classifyGrokError(status int, code, providerType string) UnifiedErrorCode {
	switch {
	case status == 426:
		return llmerrors.UnifiedErrorCodeProtocolVersion
	case status == 401 || code == "invalid_token" || code == "token_expired":
		return UnifiedErrorCodeAuthentication
	case code == "personal-team-blocked:spending-limit":
		return UnifiedErrorCodeUsageExhausted
	default:
		return classifyResponsesErrorCode(status, code)
	}
}
