package serverattach

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// ErrServerIncompatible is returned by Resolve when a server was reachable but
// failed the capability check and no local starter is provided. Callers
// distinguish it from ErrNoServerAvailable so they can tell the operator to
// restart/upgrade the running server rather than start another one (which would
// conflict on the same address). Resolve returns an *IncompatibleServerError
// that matches this sentinel via errors.Is and carries the specific reason.
var ErrServerIncompatible = errors.New("reachable server is not compatible with this client")

// IncompatibleServerError reports that a reachable server failed the capability
// check, naming why (protocol-version skew or missing capabilities) so the
// operator learns the concrete reason instead of a generic message. It matches
// ErrServerIncompatible via errors.Is and exposes Reason for callers that want
// to compose their own message.
type IncompatibleServerError struct {
	Reason string
}

func (e *IncompatibleServerError) Error() string {
	if strings.TrimSpace(e.Reason) == "" {
		return ErrServerIncompatible.Error()
	}
	return ErrServerIncompatible.Error() + ": " + e.Reason
}

func (e *IncompatibleServerError) Is(target error) bool {
	return target == ErrServerIncompatible
}

// ErrReachableServerRootMismatch is returned by Resolve when a server was
// reachable on the configured endpoint but reported a different (or no)
// persistence root than the one the operator explicitly selected, and no local
// starter is provided. Callers distinguish it from ErrNoServerAvailable because
// the actionable fix differs: the wrong-root server is already occupying the
// shared address, so starting another (`kent serve --persistence-root <root>`)
// would only hit a bind conflict; the operator must stop/reconfigure the
// other-root server or point at a matching endpoint instead. Resolve returns a
// *RootMismatchServerError that matches this sentinel via errors.Is.
var ErrReachableServerRootMismatch = errors.New("reachable server serves a different persistence root than the selected one")

// RootMismatchServerError reports that a reachable server's persistence root did
// not match the selected root, naming the other server so the operator learns
// which instance occupies the endpoint. It matches ErrReachableServerRootMismatch
// via errors.Is and exposes Reason for callers that compose their own message.
type RootMismatchServerError struct {
	Reason string
}

func (e *RootMismatchServerError) Error() string {
	if strings.TrimSpace(e.Reason) == "" {
		return ErrReachableServerRootMismatch.Error()
	}
	return ErrReachableServerRootMismatch.Error() + ": " + e.Reason
}

func (e *RootMismatchServerError) Is(target error) bool {
	return target == ErrReachableServerRootMismatch
}

// capabilityVerdict records the outcome of dialing a reachable server: the
// capability-check compatibility (with a human-readable reason when
// incompatible) and, independently, a reason populated when a reachable server
// reported a different persistence root than the required one. The two
// dimensions are independent — a root mismatch is detected before the capability
// check runs — so both are tracked and merged separately.
type capabilityVerdict struct {
	compatibility      CapabilityCompatibility
	reason             string
	rootMismatchReason string
}

// incompatibilityReason explains why a reachable server failed the capability
// check using the identity it reported during the handshake.
func incompatibilityReason(identity protocol.ServerIdentity) string {
	server := describeIncompatibleServer(identity)
	if reported := strings.TrimSpace(identity.ProtocolVersion); reported != "" && reported != protocol.Version {
		return fmt.Sprintf("%s speaks protocol version %s but this client requires %s", server, reported, protocol.Version)
	}
	return fmt.Sprintf("%s does not advertise the capabilities this client requires (it is likely an older build)", server)
}

func describeIncompatibleServer(identity protocol.ServerIdentity) string {
	id := strings.TrimSpace(identity.ServerID)
	switch {
	case id != "" && identity.PID > 0:
		return fmt.Sprintf("the running server %s (pid %d)", id, identity.PID)
	case id != "":
		return fmt.Sprintf("the running server %s", id)
	case identity.PID > 0:
		return fmt.Sprintf("the running server (pid %d)", identity.PID)
	default:
		return "the running server"
	}
}

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
		return startEmbedded(ctx, req, nil, capabilityVerdict{compatibility: CapabilityCompatibilityUnchecked})
	}
	verdict := capabilityVerdict{compatibility: CapabilityCompatibilityUnchecked}
	if candidate, ok, err := dialConfiguredRemote(ctx, req, &verdict); err != nil {
		return Resolution[T]{}, err
	} else if ok {
		return validateResolution(ctx, req, candidate)
	}
	launchErr := error(nil)
	if candidate, ok, err := launchDaemon(ctx, req, &verdict); err != nil {
		launchErr = err
	} else if ok {
		return validateResolution(ctx, req, candidate)
	}
	return startEmbedded(ctx, req, launchErr, verdict)
}

// rootMatches reports whether identity satisfies the required persistence-root
// id. An empty required id means no root pin (always matches). A non-empty
// required id never matches a server that reports no/different root, since the
// whole point of the pin is to refuse attaching to an instance whose root cannot
// be confirmed (e.g. an older build that reports no root, or a different-root
// server occupying the same endpoint).
func rootMatches(rootID string, identity protocol.ServerIdentity) bool {
	return rootID == "" || identity.PersistenceRootID == rootID
}

// rootMismatchReason explains, for the verdict, why a reachable server was
// rejected for serving a different persistence root than the selected one. It
// names the other server (and pid) so the operator can identify and stop or
// reconfigure the instance occupying the endpoint.
func rootMismatchReason(identity protocol.ServerIdentity) string {
	server := describeIncompatibleServer(identity)
	if strings.TrimSpace(identity.PersistenceRootID) == "" {
		return fmt.Sprintf("%s reports no persistence root, but this client requires the selected root", server)
	}
	return fmt.Sprintf("%s serves a different persistence root than the selected one", server)
}

func DialRemote(ctx context.Context, mode Mode, policy RemotePolicy, accept remoteattach.Accept) (*client.Remote, bool, error) {
	remote, ok, err, _ := dialRemote(ctx, mode, policy, accept)
	return remote, ok, err
}

func newIncompatibleServerError(verdict capabilityVerdict) error {
	return &IncompatibleServerError{Reason: verdict.reason}
}

func newRootMismatchServerError(verdict capabilityVerdict) error {
	return &RootMismatchServerError{Reason: verdict.rootMismatchReason}
}

func dialRemote(ctx context.Context, mode Mode, policy RemotePolicy, accept remoteattach.Accept) (*client.Remote, bool, error, capabilityVerdict) {
	verdict := capabilityVerdict{compatibility: CapabilityCompatibilityUnchecked}
	// Always wrap accept so the server identity is captured for the
	// incompatibility reason even when the caller supplies no accept predicate
	// and no root pin (in which case the wrapper simply delegates). A reachable
	// server with the wrong root is recorded distinctly so Resolve can surface it
	// as a root mismatch rather than collapsing it into "no server available".
	callerAccept := accept
	var captured protocol.ServerIdentity
	accept = func(identity protocol.ServerIdentity) bool {
		captured = identity
		if !rootMatches(policy.RootID, identity) {
			verdict.rootMismatchReason = rootMismatchReason(identity)
			return false
		}
		return callerAccept == nil || callerAccept(identity)
	}
	supports := policy.Supports
	if supports != nil {
		require := policy.Supports
		supports = func(flags protocol.CapabilityFlags) bool {
			if require(flags) {
				verdict.compatibility = CapabilityCompatibilityCompatible
				return true
			}
			verdict.compatibility = CapabilityCompatibilityIncompatible
			verdict.reason = incompatibilityReason(captured)
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
			RootID:          policy.RootID,
		})
		return remote, ok, nil, verdict
	case ModeHeadless:
		remote, ok, err := remoteattach.DialHeadless(ctx, remoteattach.HeadlessRequest{
			Config:           policy.Config,
			AttachTimeout:    policy.AttachTimeout,
			DiscoveryTimeout: policy.DiscoveryTimeout,
			DialProjectView:  policy.DialProjectView,
			DialWorkspace:    policy.DialWorkspace,
			Accept:           accept,
			Supports:         supports,
			RootID:           policy.RootID,
		})
		return remote, ok, err, verdict
	default:
		return nil, false, errors.New("unsupported server attachment mode"), verdict
	}
}

func callBypassRemote(ctx context.Context, fn func(context.Context) (bool, error)) (bool, error) {
	if fn == nil {
		return false, nil
	}
	return fn(ctx)
}

func dialConfiguredRemote[T any](ctx context.Context, req Request[T], verdict *capabilityVerdict) (Resolution[T], bool, error) {
	remote, ok, err, checked := dialRemote(ctx, req.Mode, req.Remote, nil)
	recordCapability(verdict, checked)
	if err != nil || !ok {
		return Resolution[T]{}, ok, err
	}
	target, err := wrapRemote(req, remote, remote.Close, OwnershipExternalDaemon)
	if err != nil {
		_ = remote.Close()
		return Resolution[T]{}, false, err
	}
	return newResolution(req, SourceConfiguredRemote, OwnershipExternalDaemon, target, valueCapability(verdict)), true, nil
}

func launchDaemon[T any](ctx context.Context, req Request[T], verdict *capabilityVerdict) (Resolution[T], bool, error) {
	if req.LaunchDaemon == nil {
		return Resolution[T]{}, false, nil
	}
	daemon, ok, err := req.LaunchDaemon(ctx, func(ctx context.Context, accept remoteattach.Accept) (*client.Remote, bool, error) {
		remote, ok, err, checked := dialRemote(ctx, req.Mode, req.Remote, accept)
		recordCapability(verdict, checked)
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
	return newResolution(req, SourceLaunchedDaemon, OwnershipLaunchedDaemon, target, valueCapability(verdict)), true, nil
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

func startEmbedded[T any](ctx context.Context, req Request[T], launchErr error, verdict capabilityVerdict) (Resolution[T], error) {
	if req.StartEmbedded == nil {
		noServerErr := error(ErrNoServerAvailable)
		switch {
		case verdict.rootMismatchReason != "":
			// A reachable wrong-root server is the more specific, more actionable
			// diagnosis (the root check runs before the capability check, so a
			// mismatched server is rejected here before compatibility is evaluated).
			noServerErr = newRootMismatchServerError(verdict)
		case verdict.compatibility == CapabilityCompatibilityIncompatible:
			noServerErr = newIncompatibleServerError(verdict)
		}
		if launchErr != nil {
			return Resolution[T]{}, errors.Join(noServerErr, launchErr)
		}
		return Resolution[T]{}, noServerErr
	}
	target, err := req.StartEmbedded(ctx)
	if err != nil {
		if launchErr != nil {
			return Resolution[T]{}, errors.Join(launchErr, err)
		}
		return Resolution[T]{}, err
	}
	return validateResolution(ctx, req, newResolution(req, SourceEmbeddedFallback, OwnershipEmbedded, target, verdict.compatibility))
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

func recordCapability(target *capabilityVerdict, checked capabilityVerdict) {
	if target == nil {
		return
	}
	// Merge the two independent dimensions: a dial may set only the root-mismatch
	// reason (the root check runs before, and short-circuits, the capability
	// check), so propagating solely on a non-Unchecked compatibility would drop a
	// recorded root mismatch.
	if checked.compatibility != CapabilityCompatibilityUnchecked {
		target.compatibility = checked.compatibility
		target.reason = checked.reason
	}
	if checked.rootMismatchReason != "" {
		target.rootMismatchReason = checked.rootMismatchReason
	}
}

func valueCapability(verdict *capabilityVerdict) CapabilityCompatibility {
	if verdict == nil {
		return CapabilityCompatibilityUnchecked
	}
	return verdict.compatibility
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
