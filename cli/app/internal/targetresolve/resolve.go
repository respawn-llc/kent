package targetresolve

import (
	"context"
	"errors"
)

type Source string

const (
	SourceRemote   Source = "remote"
	SourceDaemon   Source = "daemon"
	SourceEmbedded Source = "embedded"
)

type Candidate[T any] struct {
	Target T
	Close  func() error
	Source Source
}

type Request[T any] struct {
	BypassRemote  func(context.Context) (bool, error)
	DialRemote    func(context.Context) (Candidate[T], bool, error)
	LaunchDaemon  func(context.Context) (Candidate[T], bool, error)
	StartEmbedded func(context.Context) (Candidate[T], error)
	Validate      func(context.Context, Candidate[T]) error
}

func Resolve[T any](ctx context.Context, req Request[T]) (Candidate[T], error) {
	if req.StartEmbedded == nil {
		return Candidate[T]{}, errors.New("embedded target starter is required")
	}
	bypassRemote, err := callBypassRemote(ctx, req.BypassRemote)
	if err != nil {
		return Candidate[T]{}, err
	}
	if bypassRemote {
		return startEmbedded(ctx, req, nil)
	}
	if candidate, ok, err := callRemote(ctx, req.DialRemote); err != nil {
		return Candidate[T]{}, err
	} else if ok {
		candidate.Source = SourceRemote
		if err := validateCandidate(ctx, req.Validate, candidate); err != nil {
			closeCandidate(candidate)
			return Candidate[T]{}, err
		}
		return candidate, nil
	}
	launchErr := error(nil)
	if candidate, ok, err := callRemote(ctx, req.LaunchDaemon); err != nil {
		launchErr = err
	} else if ok {
		candidate.Source = SourceDaemon
		if err := validateCandidate(ctx, req.Validate, candidate); err != nil {
			closeCandidate(candidate)
			return Candidate[T]{}, err
		}
		return candidate, nil
	}
	return startEmbedded(ctx, req, launchErr)
}

func startEmbedded[T any](ctx context.Context, req Request[T], launchErr error) (Candidate[T], error) {
	candidate, err := req.StartEmbedded(ctx)
	if err != nil {
		if launchErr != nil {
			return Candidate[T]{}, errors.Join(launchErr, err)
		}
		return Candidate[T]{}, err
	}
	candidate.Source = SourceEmbedded
	if err := validateCandidate(ctx, req.Validate, candidate); err != nil {
		closeCandidate(candidate)
		return Candidate[T]{}, err
	}
	return candidate, nil
}

func callBypassRemote(ctx context.Context, fn func(context.Context) (bool, error)) (bool, error) {
	if fn == nil {
		return false, nil
	}
	return fn(ctx)
}

func callRemote[T any](ctx context.Context, fn func(context.Context) (Candidate[T], bool, error)) (Candidate[T], bool, error) {
	if fn == nil {
		return Candidate[T]{}, false, nil
	}
	return fn(ctx)
}

func validateCandidate[T any](ctx context.Context, fn func(context.Context, Candidate[T]) error, candidate Candidate[T]) error {
	if fn == nil {
		return nil
	}
	return fn(ctx, candidate)
}

func closeCandidate[T any](candidate Candidate[T]) {
	if candidate.Close != nil {
		_ = candidate.Close()
	}
}
