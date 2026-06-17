package serverattach

import (
	"errors"

	"core/shared/config"
)

var (
	ErrSessionConfigLoaderRequired  = errors.New("session target config loader is required")
	ErrSessionRemoteFactoryRequired = errors.New("session target remote factory is required")
)

type SessionServer interface {
	Close() error
}

type SessionWrapDaemonRequest[S SessionServer, D any] struct {
	LoadConfig func() (config.App, error)
	NewRemote  func(D, config.App, func() error) S
}

func SessionRemote[S SessionServer](server S) Target[S] {
	return Target[S]{
		Value: server,
		Close: server.Close,
	}
}

func WrapSessionDaemon[S SessionServer, D any](daemon DaemonTarget[D], req SessionWrapDaemonRequest[S, D]) (Target[S], error) {
	if req.LoadConfig == nil {
		return Target[S]{}, ErrSessionConfigLoaderRequired
	}
	if req.NewRemote == nil {
		return Target[S]{}, ErrSessionRemoteFactoryRequired
	}
	cfg, err := req.LoadConfig()
	if err != nil {
		return Target[S]{}, err
	}
	server := req.NewRemote(daemon.Value, cfg, daemon.Close)
	return SessionRemote(server), nil
}
