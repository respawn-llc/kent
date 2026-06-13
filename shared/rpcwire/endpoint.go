package rpcwire

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

type Transport string

const (
	TransportTCP  Transport = "tcp"
	TransportUnix Transport = "unix"
)

type Endpoint struct {
	Transport Transport
	Address   string
	ServerURL string
	OriginURL string
	UseTLS    bool
	TLSConfig *tls.Config
}

func ParseWebSocketEndpoint(raw string) (Endpoint, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Endpoint{}, errors.New("rpc endpoint is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return Endpoint{}, fmt.Errorf("parse rpc endpoint: %w", err)
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return Endpoint{}, fmt.Errorf("unsupported websocket scheme %q", parsed.Scheme)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return Endpoint{}, errors.New("websocket host is required")
	}
	port := strings.TrimSpace(parsed.Port())
	if port == "" {
		if parsed.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return Endpoint{
		Transport: TransportTCP,
		Address:   net.JoinHostPort(host, port),
		ServerURL: trimmed,
		OriginURL: websocketOrigin(parsed),
		UseTLS:    parsed.Scheme == "wss",
	}, nil
}

func NewUnixEndpoint(socketPath string, rpcPath string) (Endpoint, error) {
	trimmedSocketPath := strings.TrimSpace(socketPath)
	if trimmedSocketPath == "" {
		return Endpoint{}, errors.New("unix socket path is required")
	}
	trimmedPath := strings.TrimSpace(rpcPath)
	if trimmedPath == "" {
		trimmedPath = "/"
	}
	if !strings.HasPrefix(trimmedPath, "/") {
		trimmedPath = "/" + trimmedPath
	}
	serverURL := (&url.URL{Scheme: "ws", Host: "kent.local", Path: trimmedPath}).String()
	return Endpoint{
		Transport: TransportUnix,
		Address:   trimmedSocketPath,
		ServerURL: serverURL,
		OriginURL: "http://kent.local",
	}, nil
}

func websocketOrigin(parsed *url.URL) string {
	if parsed == nil {
		return "http://127.0.0.1"
	}
	scheme := "http"
	if parsed.Scheme == "wss" {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: parsed.Host}).String()
}
