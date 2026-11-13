package llm

import (
	"net/http"
	"strings"

	"core/server/httpcompression"
	"core/shared/textutil"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

func requestCompressionOption(variant ProviderVariantContract) option.RequestOption {
	return option.WithMiddleware(httpcompression.Middleware(variant.RequestCompression))
}

func (t *HTTPTransport) serviceBaseURL() string {
	return *t.registration.Variant.BaseURL
}

func (t *HTTPTransport) buildRequestOptions(request ResponsesRequest, preparation responsesDispatchPreparation) []option.RequestOption {
	headers := t.buildRequestHeaders(request, preparation)
	options := make([]option.RequestOption, 0, len(headers))
	for name, values := range headers {
		for index, value := range values {
			if index == 0 {
				options = append(options, option.WithHeader(name, value))
			} else {
				options = append(options, option.WithHeaderAdd(name, value))
			}
		}
	}
	return options
}

func (t *HTTPTransport) buildRequestHeaders(request ResponsesRequest, preparation responsesDispatchPreparation) http.Header {
	headers := preparation.variant.ResponsesPolicy.requestHeaders(t.ProviderIdentifier, request, preparation)
	headers.Set("User-Agent", t.providerUserAgent())
	if strings.TrimSpace(preparation.authHeader) != "" {
		headers.Set("Authorization", preparation.authHeader)
	}
	return headers
}

func servedModelMetadata(rawResp *http.Response, standardModel string) *string {
	if model := strings.TrimSpace(standardModel); model != "" {
		return textutil.Value(model)
	}
	if rawResp == nil {
		return nil
	}
	for _, headerName := range []string{"openai-model", "x-openai-model"} {
		for _, value := range rawResp.Header.Values(headerName) {
			if model := strings.TrimSpace(value); model != "" {
				return textutil.Value(model)
			}
		}
	}
	return nil
}

func observeStandardServedModel(target **string, model string) {
	model = strings.TrimSpace(model)
	if target != nil && *target == nil && model != "" {
		*target = textutil.Value(model)
	}
}

func reasoningIncludedMetadata(rawResp *http.Response) bool {
	if rawResp == nil {
		return false
	}
	return strings.TrimSpace(rawResp.Header.Get("x-reasoning-included")) == "true"
}

func (t *HTTPTransport) providerCapabilitiesForVariant(variant ProviderVariantContract) ProviderCapabilities {
	return variant.capabilitiesWithOverride(t.ProviderCapabilitiesOverride)
}

func usageFromSDK(usage responses.ResponseUsage, window int) Usage {
	out := Usage{WindowTokens: window}
	if usage.JSON.InputTokens.Valid() {
		out.InputTokens = textutil.Value(int(usage.InputTokens))
	}
	if usage.JSON.OutputTokens.Valid() {
		out.OutputTokens = textutil.Value(int(usage.OutputTokens))
	}
	if usage.JSON.InputTokensDetails.Valid() && usage.InputTokensDetails.JSON.CachedTokens.Valid() {
		out.CachedInputTokens = textutil.Value(int(usage.InputTokensDetails.CachedTokens))
	}
	return out
}
