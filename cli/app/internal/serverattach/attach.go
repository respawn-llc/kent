package serverattach

import (
	"context"
	"errors"
	"time"

	"core/cli/app/internal/remoteattach"
	"core/shared/client"
	"core/shared/config"
	"core/shared/protocol"
)

type Mode string

const (
	ModeInteractive Mode = "interactive"
	ModeHeadless    Mode = "headless"
)

type ProjectViewRemote = remoteattach.ProjectViewRemote

type Source string

const (
	SourceConfiguredRemote Source = "configured_remote"
	SourceLaunchedDaemon   Source = "launched_daemon"
	SourceEmbeddedFallback Source = "embedded_fallback"
)

type WorkspaceBindingState string

const (
	WorkspaceBindingUnknown             WorkspaceBindingState = "unknown"
	WorkspaceBindingInteractiveOptional WorkspaceBindingState = "interactive_optional"
	WorkspaceBindingInteractiveRequired WorkspaceBindingState = "interactive_required"
	WorkspaceBindingHeadlessRequired    WorkspaceBindingState = "headless_required"
)

type OwnershipState string

const (
	OwnershipExternalDaemon OwnershipState = "external_daemon"
	OwnershipLaunchedDaemon OwnershipState = "launched_daemon"
	OwnershipEmbedded       OwnershipState = "embedded"
)

type CapabilityCompatibility string

const (
	CapabilityCompatibilityUnchecked    CapabilityCompatibility = "unchecked"
	CapabilityCompatibilityCompatible   CapabilityCompatibility = "compatible"
	CapabilityCompatibilityIncompatible CapabilityCompatibility = "incompatible"
)

type AuthReadiness string

const (
	AuthReadinessUnchecked AuthReadiness = "unchecked"
	AuthReadinessValidated AuthReadiness = "validated"
)

type RemotePolicy struct {
	Config           config.App
	AttachTimeout    time.Duration
	DiscoveryTimeout time.Duration
	DialProjectView  remoteattach.DialProjectView
	DialWorkspace    remoteattach.DialWorkspace
	Supports         remoteattach.Supports
	RequireBound     bool
	// RootID, when non-empty, requires an attached server to report a matching
	// protocol.ServerIdentity.PersistenceRootID. It is set when the operator
	// explicitly selects a non-default persistence root, so a run cannot attach
	// to a different instance reachable on the same TCP endpoint. Empty means no
	// root validation (default-root behavior is unchanged).
	RootID string
}

type Target[T any] struct {
	Value T
	Close func() error
}

type DaemonTarget[T any] struct {
	Value T
	Close func() error
}

type Resolution[T any] struct {
	Value                 T
	Close                 func() error
	Mode                  Mode
	Source                Source
	Config                config.App
	WorkspaceBindingState WorkspaceBindingState
	Ownership             OwnershipState
	Capability            CapabilityCompatibility
	Auth                  AuthReadiness
}

// ErrNoServerAvailable is returned by Resolve when no remote server could be
// attached and the request provides no local server starter (no LaunchDaemon
// and no StartEmbedded). Callers translate it into a frontend-appropriate
// message; the headless run path uses it to require a pre-existing server.
var ErrNoServerAvailable = errors.New("no server available to attach to")

type LaunchedRemoteDialer func(context.Context, remoteattach.Accept) (*client.Remote, bool, error)

type Request[T any] struct {
	Mode          Mode
	Remote        RemotePolicy
	BypassRemote  func(context.Context) (bool, error)
	LaunchDaemon  func(context.Context, LaunchedRemoteDialer) (DaemonTarget[*client.Remote], bool, error)
	WrapRemote    func(*client.Remote, config.App, func() error, OwnershipState) (Target[T], error)
	StartEmbedded func(context.Context) (Target[T], error)
	Validate      func(context.Context, Resolution[T]) (AuthReadiness, error)
}

func Resolve[T any](ctx context.Context, req Request[T]) (Resolution[T], error) {
	bypass, err := callBypassRemote(ctx, req.BypassRemote)
	if err != nil {
		return Resolution[T]{}, err
	}
	if bypass {
		return startEmbedded(ctx, req, nil, CapabilityCompatibilityUnchecked)
	}
	capability := CapabilityCompatibilityUnchecked
	if candidate, ok, err := dialConfiguredRemote(ctx, req, &capability); err != nil {
		return Resolution[T]{}, err
	} else if ok {
		return validateResolution(ctx, req, candidate)
	}
	launchErr := error(nil)
	if candidate, ok, err := launchDaemon(ctx, req, &capability); err != nil {
		launchErr = err
	} else if ok {
		return validateResolution(ctx, req, candidate)
	}
	return startEmbedded(ctx, req, launchErr, capability)
}

// composeRootAccept wraps an accept predicate so that, when a persistence-root
// id is required, only a server reporting that exact id is accepted. A server
// that does not report its root (empty id, e.g. an older build) is rejected
// rather than trusted, since the whole point is to avoid attaching to an
// instance whose root cannot be confirmed.
func composeRootAccept(rootID string, accept remoteattach.Accept) remoteattach.Accept {
	if rootID == "" {
		return accept
	}
	return func(identity protocol.ServerIdentity) bool {
		if identity.PersistenceRootID != rootID {
			return false
		}
		return accept == nil || accept(identity)
	}
}

func DialRemote(ctx context.Context, mode Mode, policy RemotePolicy, accept remoteattach.Accept) (*client.Remote, bool, error) {
	remote, ok, err, _ := dialRemote(ctx, mode, policy, accept)
	return remote, ok, err
}

func dialRemote(ctx context.Context, mode Mode, policy RemotePolicy, accept remoteattach.Accept) (*client.Remote, bool, error, CapabilityCompatibility) {
	capability := CapabilityCompatibilityUnchecked
	accept = composeRootAccept(policy.RootID, accept)
	supports := policy.Supports
	if supports != nil {
		supports = func(flags protocol.CapabilityFlags) bool {
			if policy.Supports(flags) {
				capability = CapabilityCompatibilityCompatible
				return true
			}
			capability = CapabilityCompatibilityIncompatible
			return false
		}
	}
	switch mode {
	case ModeInteractive:
		remote, ok := remoteattach.DialInteractive(ctx, remoteattach.InteractiveRequest{
			Config:          policy.Config,
			AttachTimeout:   policy.AttachTimeout,
			DialProjectView: policy.DialProjectView,
			DialWorkspace:   policy.DialWorkspace,
			Accept:          accept,
			Supports:        supports,
			RequireBound:    policy.RequireBound,
		})
		return remote, ok, nil, capability
	case ModeHeadless:
		remote, ok, err := remoteattach.DialHeadless(ctx, remoteattach.HeadlessRequest{
			Config:           policy.Config,
			AttachTimeout:    policy.AttachTimeout,
			DiscoveryTimeout: policy.DiscoveryTimeout,
			DialProjectView:  policy.DialProjectView,
			DialWorkspace:    policy.DialWorkspace,
			Accept:           accept,
			Supports:         supports,
		})
		return remote, ok, err, capability
	default:
		return nil, false, errors.New("unsupported server attachment mode"), capability
	}
}

func callBypassRemote(ctx context.Context, fn func(context.Context) (bool, error)) (bool, error) {
	if fn == nil {
		return false, nil
	}
	return fn(ctx)
}

func dialConfiguredRemote[T any](ctx context.Context, req Request[T], capability *CapabilityCompatibility) (Resolution[T], bool, error) {
	remote, ok, err, checkedCapability := dialRemote(ctx, req.Mode, req.Remote, nil)
	recordCapability(capability, checkedCapability)
	if err != nil || !ok {
		return Resolution[T]{}, ok, err
	}
	target, err := wrapRemote(req, remote, remote.Close, OwnershipExternalDaemon)
	if err != nil {
		_ = remote.Close()
		return Resolution[T]{}, false, err
	}
	return newResolution(req, SourceConfiguredRemote, OwnershipExternalDaemon, target, valueCapability(capability)), true, nil
}

func launchDaemon[T any](ctx context.Context, req Request[T], capability *CapabilityCompatibility) (Resolution[T], bool, error) {
	if req.LaunchDaemon == nil {
		return Resolution[T]{}, false, nil
	}
	daemon, ok, err := req.LaunchDaemon(ctx, func(ctx context.Context, accept remoteattach.Accept) (*client.Remote, bool, error) {
		remote, ok, err, checkedCapability := dialRemote(ctx, req.Mode, req.Remote, accept)
		recordCapability(capability, checkedCapability)
		return remote, ok, err
	})
	if err != nil || !ok {
		return Resolution[T]{}, ok, err
	}
	target, err := wrapRemote(req, daemon.Value, daemon.Close, OwnershipLaunchedDaemon)
	if err != nil {
		closeTarget(DaemonTarget[*client.Remote]{Close: daemon.Close})
		return Resolution[T]{}, false, err
	}
	return newResolution(req, SourceLaunchedDaemon, OwnershipLaunchedDaemon, target, valueCapability(capability)), true, nil
}

func wrapRemote[T any](req Request[T], remote *client.Remote, closeFn func() error, ownership OwnershipState) (Target[T], error) {
	if req.WrapRemote == nil {
		return Target[T]{}, errors.New("remote target wrapper is required")
	}
	target, err := req.WrapRemote(remote, req.Remote.Config, closeFn, ownership)
	if err != nil {
		return Target[T]{}, err
	}
	if target.Close == nil {
		target.Close = closeFn
	}
	return target, nil
}

func startEmbedded[T any](ctx context.Context, req Request[T], launchErr error, capability CapabilityCompatibility) (Resolution[T], error) {
	if req.StartEmbedded == nil {
		if launchErr != nil {
			return Resolution[T]{}, errors.Join(ErrNoServerAvailable, launchErr)
		}
		return Resolution[T]{}, ErrNoServerAvailable
	}
	target, err := req.StartEmbedded(ctx)
	if err != nil {
		if launchErr != nil {
			return Resolution[T]{}, errors.Join(launchErr, err)
		}
		return Resolution[T]{}, err
	}
	return validateResolution(ctx, req, newResolution(req, SourceEmbeddedFallback, OwnershipEmbedded, target, capability))
}

func validateResolution[T any](ctx context.Context, req Request[T], resolution Resolution[T]) (Resolution[T], error) {
	if req.Validate == nil {
		return resolution, nil
	}
	auth, err := req.Validate(ctx, resolution)
	if err != nil {
		closeResolution(resolution)
		return Resolution[T]{}, err
	}
	resolution.Auth = auth
	return resolution, nil
}

func newResolution[T any](req Request[T], source Source, ownership OwnershipState, target Target[T], capability CapabilityCompatibility) Resolution[T] {
	return Resolution[T]{
		Value:                 target.Value,
		Close:                 target.Close,
		Mode:                  req.Mode,
		Source:                source,
		Config:                req.Remote.Config,
		WorkspaceBindingState: workspaceBindingState(req.Mode, req.Remote.RequireBound),
		Ownership:             ownership,
		Capability:            capability,
		Auth:                  AuthReadinessUnchecked,
	}
}

func recordCapability(target *CapabilityCompatibility, checked CapabilityCompatibility) {
	if target == nil || checked == CapabilityCompatibilityUnchecked {
		return
	}
	*target = checked
}

func valueCapability(capability *CapabilityCompatibility) CapabilityCompatibility {
	if capability == nil {
		return CapabilityCompatibilityUnchecked
	}
	return *capability
}

func workspaceBindingState(mode Mode, requireBound bool) WorkspaceBindingState {
	switch mode {
	case ModeHeadless:
		return WorkspaceBindingHeadlessRequired
	case ModeInteractive:
		if requireBound {
			return WorkspaceBindingInteractiveRequired
		}
		return WorkspaceBindingInteractiveOptional
	default:
		return WorkspaceBindingUnknown
	}
}

func closeResolution[T any](resolution Resolution[T]) {
	if resolution.Close != nil {
		_ = resolution.Close()
	}
}

func closeTarget[T any](target DaemonTarget[T]) {
	if target.Close != nil {
		_ = target.Close()
	}
}
