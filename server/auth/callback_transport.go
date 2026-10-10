package auth

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	sharedauth "core/shared/auth"
	"core/shared/config"
)

func CallbackTransport(protocol config.ConnectionProtocol) (sharedauth.CallbackTransport, error) {
	switch protocol {
	case config.ConnectionChatGPT:
		return sharedauth.CallbackTransport{BindAddress: oauthBindAddress, RedirectHost: "localhost", CallbackPath: oauthCallbackPath}, nil
	default:
		return sharedauth.CallbackTransport{}, fmt.Errorf("connection protocol %s has no browser callback transport", protocol)
	}
}

func ValidateCallbackRedirect(protocol config.ConnectionProtocol, redirect string) error {
	transport, err := CallbackTransport(protocol)
	if err != nil {
		return err
	}
	u, err := url.Parse(redirect)
	if err != nil {
		return fmt.Errorf("parse OAuth callback redirect: %w", err)
	}
	_, bindPort, err := net.SplitHostPort(transport.BindAddress)
	if err != nil {
		return err
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || port == 0 || (bindPort != "0" && u.Port() != bindPort) ||
		u.Scheme != "http" || u.Hostname() != transport.RedirectHost || u.Path != transport.CallbackPath ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("OAuth callback redirect does not match the selected connection transport")
	}
	return nil
}
