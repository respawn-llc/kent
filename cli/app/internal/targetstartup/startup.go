package targetstartup

import (
	"context"
	"errors"

	"builder/cli/app/internal/targetresolve"
)

var ErrDaemonWrapperRequired = errors.New("daemon target wrapper is required")

type Source = targetresolve.Source

const (
	SourceRemote   = targetresolve.SourceRemote
	SourceDaemon   = targetresolve.SourceDaemon
	SourceEmbedded = targetresolve.SourceEmbedded
)

type Target[T any] struct {
	Value T
	Close func() error
}

type DaemonTarget[T any] struct {
	Value T
	Close func() error
}

type Request[T any, D any] struct {
	BypassRemote  func(context.Context) (bool, error)
	DialRemote    func(context.Context) (Target[T], bool, error)
	LaunchDaemon  func(context.Context) (DaemonTarget[D], bool, error)
	WrapDaemon    func(context.Context, DaemonTarget[D]) (Target[T], error)
	StartEmbedded func(context.Context) (Target[T], error)
	Validate      func(context.Context, Source, T) error
}

func Resolve[T any, D any](ctx context.Context, req Request[T, D]) (Target[T], error) {
	candidate, err := targetresolve.Resolve[T](ctx, targetresolve.Request[T]{
		BypassRemote: req.BypassRemote,
		DialRemote: func(ctx context.Context) (targetresolve.Candidate[T], bool, error) {
			target, ok, err := callDialRemote(ctx, req.DialRemote)
			return toCandidate(target), ok, err
		},
		LaunchDaemon: func(ctx context.Context) (targetresolve.Candidate[T], bool, error) {
			target, ok, err := callLaunchDaemon(ctx, req.LaunchDaemon)
			if err != nil || !ok {
				return targetresolve.Candidate[T]{}, ok, err
			}
			wrapped, err := wrapDaemon(ctx, req.WrapDaemon, target)
			if err != nil {
				closeDaemon(target)
				return targetresolve.Candidate[T]{}, false, err
			}
			if wrapped.Close == nil {
				wrapped.Close = target.Close
			}
			return toCandidate(wrapped), true, nil
		},
		StartEmbedded: func(ctx context.Context) (targetresolve.Candidate[T], error) {
			target, err := req.StartEmbedded(ctx)
			return toCandidate(target), err
		},
		Validate: func(ctx context.Context, candidate targetresolve.Candidate[T]) error {
			if req.Validate == nil {
				return nil
			}
			return req.Validate(ctx, candidate.Source, candidate.Target)
		},
	})
	if err != nil {
		return Target[T]{}, err
	}
	return Target[T]{Value: candidate.Target, Close: candidate.Close}, nil
}

func callDialRemote[T any](ctx context.Context, fn func(context.Context) (Target[T], bool, error)) (Target[T], bool, error) {
	if fn == nil {
		return Target[T]{}, false, nil
	}
	return fn(ctx)
}

func callLaunchDaemon[T any](ctx context.Context, fn func(context.Context) (DaemonTarget[T], bool, error)) (DaemonTarget[T], bool, error) {
	if fn == nil {
		return DaemonTarget[T]{}, false, nil
	}
	return fn(ctx)
}

func wrapDaemon[T any, D any](ctx context.Context, fn func(context.Context, DaemonTarget[D]) (Target[T], error), target DaemonTarget[D]) (Target[T], error) {
	if fn == nil {
		return Target[T]{}, ErrDaemonWrapperRequired
	}
	return fn(ctx, target)
}

func toCandidate[T any](target Target[T]) targetresolve.Candidate[T] {
	return targetresolve.Candidate[T]{
		Target: target.Value,
		Close:  target.Close,
	}
}

func closeDaemon[T any](target DaemonTarget[T]) {
	if target.Close != nil {
		_ = target.Close()
	}
}
