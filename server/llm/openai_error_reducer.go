package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

type responsesErrorReducer struct {
	providerID string
	decode     func([]byte) (responsesErrorPayload, bool)
	classify   func(int, string) UnifiedErrorCode
}

type opaqueProviderErrorReducer struct {
	providerID string
}

func newOpenAICompatibleErrorReducer(providerID string) ProviderErrorReducer {
	return responsesErrorReducer{
		providerID: strings.TrimSpace(providerID),
		decode:     decodeResponsesErrorPayload, classify: classifyOpenAIUnifiedErrorCode,
	}
}

func newOpaqueProviderErrorReducer(providerID string) ProviderErrorReducer {
	return opaqueProviderErrorReducer{providerID: strings.TrimSpace(providerID)}
}

func (r responsesErrorReducer) Reduce(err error, rawResp *http.Response) (*ProviderAPIError, bool) {
	if reduced, ok := r.reduceFromStreamError(err, newResponsesStatus(rawResp)); ok {
		return reduced, true
	}
	if reduced, ok := r.reduceFromSDK(err); ok {
		return reduced, true
	}
	if reduced, ok := r.reduceFromResponse(rawResp); ok {
		return reduced, true
	}
	return nil, false
}

func (r opaqueProviderErrorReducer) Reduce(err error, rawResp *http.Response) (*ProviderAPIError, bool) {
	if reduced, ok := r.reduceFromResponse(rawResp); ok {
		return reduced, true
	}
	if err != nil {
		message := strings.TrimSpace(err.Error())
		return &ProviderAPIError{
			ProviderID: r.providerID,
			StatusCode: 0,
			Code:       UnifiedErrorCodeUnknown,
			Message:    message,
			Raw:        message,
			Err:        err,
		}, true
	}
	return nil, false
}

func (r responsesErrorReducer) reduceFromStreamError(err error, responseStatus *responsesStatus) (*ProviderAPIError, bool) {
	if err == nil {
		return nil, false
	}
	if responseStatus == nil {
		return nil, false
	}
	var streamErr *ssestream.StreamError
	if !errors.As(err, &streamErr) {
		return nil, false
	}
	reduced, ok := r.reducePayload(streamErr.Event.Data, err, responseStatus.Code)
	if !ok {
		raw := truncateError(streamErr.Event.Data)
		return r.providerError(responseStatus.Code, responsesErrorPayload{Message: raw}, raw, err), true
	}
	return reduced, true
}

func (r responsesErrorReducer) reducePayload(data []byte, cause error, status int) (*ProviderAPIError, bool) {
	payload, ok := r.decode(data)
	if !ok {
		return nil, false
	}
	return r.providerError(status, payload, string(data), cause), true
}

func (r responsesErrorReducer) providerError(status int, payload responsesErrorPayload, raw string, cause error) *ProviderAPIError {
	return &ProviderAPIError{
		ProviderID: r.providerID, StatusCode: status, Code: r.classify(status, payload.Code),
		ProviderCode: payload.Code, ProviderType: payload.Type, ProviderParam: payload.Param,
		Message: payload.Message, Raw: raw, Err: cause,
	}
}

func (r responsesErrorReducer) reduceFromSDK(err error) (*ProviderAPIError, bool) {
	if err == nil {
		return nil, false
	}
	var sdkErr *openai.Error
	if !errors.As(err, &sdkErr) {
		return nil, false
	}
	return r.providerError(sdkErr.StatusCode, responsesErrorPayload{
		Code: sdkErr.Code, Type: sdkErr.Type, Param: sdkErr.Param, Message: sdkErr.Message,
	}, sdkErr.RawJSON(), err), true
}

func (r responsesErrorReducer) reduceFromResponse(rawResp *http.Response) (*ProviderAPIError, bool) {
	if rawResp == nil || rawResp.StatusCode < 300 {
		return nil, false
	}
	if rawResp.Body == nil {
		return &ProviderAPIError{
			ProviderID: r.providerID,
			StatusCode: rawResp.StatusCode,
			Code:       r.classify(rawResp.StatusCode, ""),
			Message:    http.StatusText(rawResp.StatusCode),
			Raw:        "<empty error body>",
		}, true
	}
	body, readErr := io.ReadAll(rawResp.Body)
	rawResp.Body.Close()
	rawResp.Body = io.NopCloser(bytes.NewReader(body))
	raw := truncateError(body)
	if readErr != nil {
		return r.providerError(rawResp.StatusCode, responsesErrorPayload{Message: raw}, raw, readErr), true
	}
	if reduced, ok := r.reducePayload(body, nil, rawResp.StatusCode); ok {
		return reduced, true
	}

	return &ProviderAPIError{
		ProviderID: r.providerID,
		StatusCode: rawResp.StatusCode,
		Code:       r.classify(rawResp.StatusCode, ""),
		Message:    raw,
		Raw:        raw,
	}, true
}

func (r opaqueProviderErrorReducer) reduceFromResponse(rawResp *http.Response) (*ProviderAPIError, bool) {
	if rawResp == nil || rawResp.StatusCode < 300 {
		return nil, false
	}
	if rawResp.Body == nil {
		code := UnifiedErrorCodeUnknown
		if rawResp.StatusCode == 401 || rawResp.StatusCode == 403 {
			code = UnifiedErrorCodeAuthentication
		}
		return &ProviderAPIError{
			ProviderID: r.providerID,
			StatusCode: rawResp.StatusCode,
			Code:       code,
			Message:    http.StatusText(rawResp.StatusCode),
			Raw:        "<empty error body>",
		}, true
	}
	body, _ := io.ReadAll(rawResp.Body)
	rawResp.Body.Close()
	rawResp.Body = io.NopCloser(bytes.NewReader(body))
	raw := truncateError(body)
	code := UnifiedErrorCodeUnknown
	if rawResp.StatusCode == 401 || rawResp.StatusCode == 403 {
		code = UnifiedErrorCodeAuthentication
	}
	return &ProviderAPIError{
		ProviderID: r.providerID,
		StatusCode: rawResp.StatusCode,
		Code:       code,
		Message:    raw,
		Raw:        raw,
	}, true
}

type responsesErrorPayload struct {
	Type    string
	Code    string
	Param   string
	Message string
}

func decodeResponsesErrorPayload(data []byte) (responsesErrorPayload, bool) {
	if len(bytes.TrimSpace(data)) == 0 || !json.Valid(data) {
		return responsesErrorPayload{}, false
	}
	var envelope struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Param   string `json:"param"`
		Message string `json:"message"`
		Error   struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Param   string `json:"param"`
			Message string `json:"message"`
		} `json:"error"`
		Response struct {
			IncompleteDetails struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
			Error struct {
				Type    string `json:"type"`
				Code    string `json:"code"`
				Param   string `json:"param"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return responsesErrorPayload{}, false
	}
	eventType := strings.TrimSpace(envelope.Type)
	payload := responsesErrorPayload{
		Type:    eventType,
		Code:    strings.TrimSpace(envelope.Code),
		Param:   strings.TrimSpace(envelope.Param),
		Message: strings.TrimSpace(envelope.Message),
	}
	if strings.TrimSpace(envelope.Error.Code) != "" || strings.TrimSpace(envelope.Error.Message) != "" {
		payload = responsesErrorPayload{
			Type:    strings.TrimSpace(envelope.Error.Type),
			Code:    strings.TrimSpace(envelope.Error.Code),
			Param:   strings.TrimSpace(envelope.Error.Param),
			Message: strings.TrimSpace(envelope.Error.Message),
		}
	}
	if strings.TrimSpace(envelope.Response.Error.Code) != "" || strings.TrimSpace(envelope.Response.Error.Message) != "" {
		payload = responsesErrorPayload{
			Type:    strings.TrimSpace(envelope.Response.Error.Type),
			Code:    strings.TrimSpace(envelope.Response.Error.Code),
			Param:   strings.TrimSpace(envelope.Response.Error.Param),
			Message: strings.TrimSpace(envelope.Response.Error.Message),
		}
	}
	if eventType == "response.incomplete" {
		reason := strings.TrimSpace(envelope.Response.IncompleteDetails.Reason)
		if reason != "" {
			payload = responsesErrorPayload{
				Type:    eventType,
				Code:    reason,
				Param:   "response.incomplete_details.reason",
				Message: "response incomplete: " + reason,
			}
		}
	}
	if payload.Code == "" && payload.Message == "" {
		return responsesErrorPayload{}, false
	}
	return payload, true
}

func classifyOpenAIUnifiedErrorCode(statusCode int, providerCode string) UnifiedErrorCode {
	if statusCode == 401 || statusCode == 403 {
		return UnifiedErrorCodeAuthentication
	}
	switch strings.ToLower(strings.TrimSpace(providerCode)) {
	case "context_length_exceeded",
		"context_window_exceeded",
		"max_context_length_exceeded",
		"token_limit_exceeded",
		"prompt_too_long",
		"input_too_long":
		return UnifiedErrorCodeContextLengthOverflow
	case "server_is_overloaded":
		if providerCode == "server_is_overloaded" {
			return UnifiedErrorCodeProviderOverload
		}
		return UnifiedErrorCodeUnknown
	default:
		return UnifiedErrorCodeUnknown
	}
}
