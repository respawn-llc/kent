package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sdkwebsocket "github.com/openai/openai-go/v3/packages/websocket"
)

type responsesOpeningOrigin struct {
	scheme string
	host   string
	port   string
}

func responsesOrigin(value *url.URL) (responsesOpeningOrigin, error) {
	scheme := strings.ToLower(value.Scheme)
	switch scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	case "http", "https":
	default:
		return responsesOpeningOrigin{}, fmt.Errorf("unsupported Responses socket URL scheme")
	}
	if value.Hostname() == "" || value.User != nil || value.Fragment != "" || value.Opaque != "" {
		return responsesOpeningOrigin{}, fmt.Errorf("invalid Responses socket URL")
	}
	port := value.Port()
	if port == "" {
		port = "80"
		if scheme == "https" {
			port = "443"
		}
	}
	return responsesOpeningOrigin{scheme: scheme, host: strings.ToLower(value.Hostname()), port: port}, nil
}

func (origin responsesOpeningOrigin) validate(request *http.Request) error {
	if request.URL == nil || request.RequestURI != "" {
		return fmt.Errorf("invalid Responses socket request target")
	}
	target, err := responsesOrigin(request.URL)
	if err != nil {
		return err
	}
	if target != origin {
		return fmt.Errorf("Responses socket request changed configured origin")
	}
	if request.Host != "" {
		authority, err := url.Parse(target.scheme + "://" + request.Host)
		if err != nil {
			return err
		}
		actual, err := responsesOrigin(authority)
		if err != nil || actual != origin || authority.Path != "" {
			return fmt.Errorf("Responses socket request changed configured HTTP authority")
		}
	}
	return nil
}

// The SDK owns the upgrade and socket. Observe rejected HTTP responses before
// Dial closes their bodies so the existing provider reducer retains diagnostics.
type responsesOpeningTransport struct {
	next     http.RoundTripper
	origin   responsesOpeningOrigin
	response *http.Response
	failure  error
	provider string
}

func (opening *responsesOpeningTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := opening.origin.validate(request); err != nil {
		return nil, err
	}
	response, err := opening.next.RoundTrip(request)
	opening.response = response
	if response != nil && response.StatusCode >= http.StatusBadRequest {
		opening.failure = newResponsesRequestErrorMapper(opening.provider).Map(err, response, "open responses websocket")
	}
	return response, err
}

func (t *HTTPTransport) openResponsesSocket(ctx context.Context, request ResponsesRequest, preparation responsesDispatchPreparation) (*sdkwebsocket.Connection[responsesSocketEvent], *http.Response, error) {
	base, err := url.Parse(t.serviceBaseURL())
	if err != nil {
		return nil, nil, err
	}
	origin, err := responsesOrigin(base)
	if err != nil {
		return nil, nil, err
	}
	target := base.JoinPath("responses")
	target.Scheme = origin.scheme
	upgrade, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	upgrade.Header = t.buildRequestHeaders(request, preparation)
	upgrade.Header.Del(codexTurnStateHeader)
	client := *t.Client
	transport := client.Transport
	if transport == nil {
		transport = sharedHTTPTransport
	}
	opening := &responsesOpeningTransport{next: transport, origin: origin, provider: preparation.variant.ProviderID}
	client.Transport = opening
	priorRedirect := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if err := origin.validate(next); err != nil {
			return err
		}
		next.URL.Scheme = origin.scheme
		if priorRedirect != nil {
			return priorRedirect(next, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("Responses socket opening exceeded 10 redirects")
		}
		return nil
	}
	connection, err := sdkwebsocket.Dial(ctx, upgrade, &client, sdkwebsocket.Options{
		MaxMessageBytes: responsesSocketMessageBytes, MaxBufferedBytes: responsesSocketBufferedBytes, MaxBufferedEvents: responsesSocketBufferedEvents,
	}, decodeResponsesSocketEvent, func(responsesSocketEvent) string { return "" })
	if err != nil {
		if opening.failure != nil {
			return nil, opening.response, opening.failure
		}
		return nil, opening.response, newResponsesRequestErrorMapper(preparation.variant.ProviderID).Map(err, opening.response, "open responses websocket")
	}
	return connection, opening.response, nil
}
