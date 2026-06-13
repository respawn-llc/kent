//go:build !darwin && !linux && !windows

package main

import (
	"context"
	"runtime"
)

type unsupportedServiceBackend struct{}

func currentServiceBackend() serviceBackend {
	return unsupportedServiceBackend{}
}

func (unsupportedServiceBackend) Name() string {
	return runtime.GOOS
}

func (unsupportedServiceBackend) Install(context.Context, serviceSpec, bool, bool) error {
	return errUnsupportedServiceBackend()
}

func (unsupportedServiceBackend) Uninstall(context.Context, serviceSpec, bool) error {
	return errUnsupportedServiceBackend()
}

func (unsupportedServiceBackend) Start(context.Context, serviceSpec) error {
	return errUnsupportedServiceBackend()
}

func (unsupportedServiceBackend) Stop(context.Context, serviceSpec) error {
	return errUnsupportedServiceBackend()
}

func (unsupportedServiceBackend) Restart(context.Context, serviceSpec) error {
	return errUnsupportedServiceBackend()
}

func (unsupportedServiceBackend) Status(context.Context, serviceSpec) (serviceStatus, error) {
	return serviceStatus{Backend: runtime.GOOS}, nil
}

func errUnsupportedServiceBackend() error {
	return serviceCommandError{Name: "builder service", Args: []string{runtime.GOOS}, Result: serviceCommandResult{Code: 1, Stderr: "background service management is not supported on this OS"}}
}
