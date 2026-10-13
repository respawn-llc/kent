package client

import "fmt"

// ServerUnavailableError identifies a failed transport connection, before the
// server's identity, credentials, or readiness can be checked.
type ServerUnavailableError struct {
	Endpoint string
	Cause    error
}

func (e *ServerUnavailableError) Error() string {
	return fmt.Sprintf(
		"Cannot reach the Kent server at %s.\n"+
			"On the server machine, run `kent serve` in a separate terminal and leave it running, "+
			"or run `kent service install` to install and start the background service.\n"+
			"If the server is already running, check the configured server address and port.\n"+
			"Setup guide: https://kent.sh/server/#background-service\n"+
			"Connection error: %v",
		e.Endpoint, e.Cause,
	)
}

func (e *ServerUnavailableError) Unwrap() error { return e.Cause }
