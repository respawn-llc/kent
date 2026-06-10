package sessiontarget

import (
	"errors"

	"builder/cli/app/internal/targetstartup"
	"builder/shared/config"
)

var (
	ErrConfigLoaderRequired  = errors.New("session target config loader is required")
	ErrRemoteFactoryRequired = errors.New("session target remote factory is required")
)

type Server interface {
	Close() error
}

type WrapDaemonRequest[S Server, D any] struct {
	LoadConfig func() (config.App, error)
	NewRemote  func(D, config.App, func() error) S
}

func Remote[S Server](server S) targetstartup.Target[S] {
	return targetstartup.Target[S]{
		Value: server,
		Close: server.Close,
	}
}

func WrapDaemon[S Server, D any](daemon targetstartup.DaemonTarget[D], req WrapDaemonRequest[S, D]) (targetstartup.Target[S], error) {
	if req.LoadConfig == nil {
		return targetstartup.Target[S]{}, ErrConfigLoaderRequired
	}
	if req.NewRemote == nil {
		return targetstartup.Target[S]{}, ErrRemoteFactoryRequired
	}
	cfg, err := req.LoadConfig()
	if err != nil {
		return targetstartup.Target[S]{}, err
	}
	server := req.NewRemote(daemon.Value, cfg, daemon.Close)
	return Remote(server), nil
}
