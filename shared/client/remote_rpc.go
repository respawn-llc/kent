package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"core/shared/config"
	"core/shared/protocol"
	"core/shared/rpcwire"
)

var errRemoteClosed = errors.New("remote client is closed")

// errRequestCanceledByClient identifies a request-canceled error that has been
// normalized to a clear client-facing message (rather than passing through a
// raw transport message such as context.Canceled's text). requestCanceledError
// reports itself as this sentinel via Is when it carries no distinct message.
var errRequestCanceledByClient = errors.New("request canceled by client")

const preferredLocalSocketProbeTimeout = 100 * time.Millisecond

var errRemoteSessionIDRequired = errors.New("remote session ID is required")

type requestCanceledError struct {
	message string
}

func (e requestCanceledError) normalized() bool {
	message := strings.TrimSpace(e.message)
	return message == "" || message == context.Canceled.Error()
}

func (e requestCanceledError) Error() string {
	if e.normalized() {
		return errRequestCanceledByClient.Error()
	}
	return strings.TrimSpace(e.message)
}

func (e requestCanceledError) Unwrap() error {
	return context.Canceled
}

// Is reports requestCanceledError as errRequestCanceledByClient when it has
// been normalized to the clear client-facing message, letting callers detect
// that state structurally instead of comparing rendered strings.
func (e requestCanceledError) Is(target error) bool {
	return target == errRequestCanceledByClient && e.normalized()
}

type remoteDialPlan struct {
	endpoints []rpcwire.Endpoint
}

type remoteControlConn struct {
	conn      rpcwire.Conn
	pendingMu sync.Mutex
	pending   map[string]chan remoteControlResponse
	requestID atomic.Uint64
	failOnce  sync.Once
	done      chan struct{}
	errMu     sync.Mutex
	err       error
}

type remoteControlResponse struct {
	binary *remoteBinaryResponse
	err    error
}

type remoteSessionControl struct {
	sessionID      string
	remote         *Remote
	initialControl *remoteControlConn
}

func configuredRemoteDialPlan(cfg config.Connection) (remoteDialPlan, error) {
	tcpEndpoint, err := rpcwire.ParseWebSocketEndpoint(cfg.RPCURL())
	if err != nil {
		return remoteDialPlan{}, err
	}
	endpoints := make([]rpcwire.Endpoint, 0, 2)
	if shouldPreferConfiguredLocalSocket(cfg) {
		if socketPath, ok, err := config.ServerLocalRPCSocketPath(cfg.PersistenceRoot); err != nil {
			return remoteDialPlan{}, err
		} else if ok {
			if _, statErr := os.Stat(socketPath); statErr == nil {
				udsEndpoint, err := rpcwire.NewUnixEndpoint(socketPath, protocol.RPCPath)
				if err != nil {
					return remoteDialPlan{}, err
				}
				endpoints = append(endpoints, udsEndpoint)
			}
		}
	}
	endpoints = append(endpoints, tcpEndpoint)
	return remoteDialPlan{endpoints: endpoints}, nil
}

func shouldPreferConfiguredLocalSocket(cfg config.Connection) bool {
	// Explicit TCP target overrides must stay authoritative; the derived unix socket
	// is only a default local optimization for the standard local attach target.
	if hasExplicitTCPServerTarget(cfg) {
		return false
	}
	host := strings.TrimSpace(cfg.ServerHost)
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func hasExplicitTCPServerTarget(cfg config.Connection) bool {
	sources := cfg.Source.Sources
	if len(sources) == 0 {
		return false
	}
	return sources["server_host"].Configured() || sources["server_port"].Configured()
}

func dialRemoteWithTransport(ctx context.Context, plan remoteDialPlan, transport rpcwire.ClientTransport, attachIntent *remoteAttachmentIntent) (*Remote, error) {
	conn, err := plan.dial(ctx, transport)
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = conn.Close() }
	setup := remoteConnectionSetup{
		attachmentIntent: attachIntent,
	}
	state, err := setup.run(ctx, conn)
	if err != nil {
		cleanup()
		return nil, err
	}
	if state.attachment != nil && state.attachment.session != nil {
		attachIntent, err = newRemoteSessionReattachmentIntent(*state.attachment.session)
		if err != nil {
			cleanup()
			return nil, err
		}
	}
	control := newRemoteControlConn(conn)
	return &Remote{
		plan:         plan,
		transport:    transport,
		control:      control,
		identity:     state.identity,
		attachIntent: attachIntent,
		attachment:   state.attachment,
	}, nil
}

func (p remoteDialPlan) dial(ctx context.Context, transport rpcwire.ClientTransport) (rpcwire.Conn, error) {
	if len(p.endpoints) == 0 {
		return nil, errors.New("remote rpc endpoint is required")
	}
	var dialErr error
	for i, endpoint := range p.endpoints {
		dialCtx, cancel := endpointDialContext(ctx, endpoint, i < len(p.endpoints)-1)
		conn, err := transport.Dial(dialCtx, endpoint)
		cancel()
		if err == nil {
			return conn, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		dialErr = err
	}
	if dialErr == nil {
		dialErr = errors.New("remote rpc endpoint is required")
	}
	return nil, dialErr
}

func endpointDialContext(ctx context.Context, endpoint rpcwire.Endpoint, hasFallback bool) (context.Context, context.CancelFunc) {
	if !hasFallback || endpoint.Transport != rpcwire.TransportUnix {
		return ctx, func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ctx, func() {}
		}
		probeTimeout := preferredLocalSocketProbeTimeout
		if remaining < preferredLocalSocketProbeTimeout*2 {
			probeTimeout = remaining / 2
		}
		if probeTimeout > 0 && probeTimeout < remaining {
			return context.WithTimeout(ctx, probeTimeout)
		}
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, preferredLocalSocketProbeTimeout)
}

func (c *Remote) openRPCConn(ctx context.Context) (rpcwire.Conn, func(), error) {
	return c.openRPCConnWithAdditionalAttachment(ctx, nil)
}

func (c *Remote) openRPCConnWithAdditionalAttachment(
	ctx context.Context,
	additionalAttachmentIntent *remoteAttachmentIntent,
) (rpcwire.Conn, func(), error) {
	conn, cleanup, _, err := c.openSetupRPCConn(ctx, additionalAttachmentIntent)
	return conn, cleanup, err
}

func (c *Remote) openSetupRPCConn(
	ctx context.Context,
	additionalAttachmentIntent *remoteAttachmentIntent,
) (rpcwire.Conn, func(), remoteConnectionState, error) {
	c.mu.Lock()
	attachmentIntent := c.attachIntent
	attachment := c.attachment
	c.mu.Unlock()
	return c.openSetupRPCConnForAttachment(ctx, additionalAttachmentIntent, attachmentIntent, attachment)
}

func (c *Remote) openSetupRPCConnForAttachment(
	ctx context.Context,
	additionalAttachmentIntent *remoteAttachmentIntent,
	attachmentIntent *remoteAttachmentIntent,
	attachment *remoteAttachment,
) (rpcwire.Conn, func(), remoteConnectionState, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, nil, remoteConnectionState{}, err
	}
	conn, err := c.plan.dial(ctx, c.transport)
	if err != nil {
		return nil, nil, remoteConnectionState{}, err
	}
	cleanup := func() { _ = conn.Close() }
	setup := remoteConnectionSetup{
		attachmentIntent:           attachmentIntent,
		additionalAttachmentIntent: additionalAttachmentIntent,
		expectation: &remoteConnectionExpectation{
			rootID:     c.rootID(),
			attachment: attachment,
		},
	}
	state, err := setup.run(ctx, conn)
	if err != nil {
		cleanup()
		return nil, nil, remoteConnectionState{}, err
	}
	return conn, cleanup, state, nil
}

func (c *Remote) prepareDraftHandoff(ctx context.Context, sessionID string) (*remoteSessionControl, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	c.mu.Lock()
	attachmentIntent := c.attachIntent
	c.mu.Unlock()
	attachedSessionID, attachedToSession := attachmentIntent.sessionID()
	if attachedToSession {
		if attachedSessionID != sessionID {
			return nil, false, fmt.Errorf(
				"remote is attached to session %q, cannot prepare draft handoff for session %q",
				attachedSessionID,
				sessionID,
			)
		}
		return nil, false, nil
	}
	if _, attachedToProject := attachmentIntent.projectRequest(); !attachedToProject {
		return nil, false, nil
	}
	// A Project-attached control connection loses access as soon as the Session
	// moves. Establish the exact-Session control while the source Project still
	// owns it so the composer draft can be persisted before TUI reattachment.
	intent, err := newRemoteSessionAttachmentIntent(sessionID)
	if err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	if c.closed.Load() {
		c.mu.Unlock()
		return nil, false, errors.New("remote client is closed")
	}
	current := c.draftHandoff
	if current != nil && current.sessionID == sessionID {
		c.mu.Unlock()
		return current, false, nil
	}
	c.mu.Unlock()
	conn, cleanup, state, err := c.openSetupRPCConn(ctx, intent)
	if err != nil {
		return nil, false, err
	}
	if state.attachment == nil || state.attachment.session == nil {
		cleanup()
		return nil, false, errors.New("Session handoff attachment is required")
	}
	handoffIntent, err := newRemoteSessionReattachmentIntent(*state.attachment.session)
	if err != nil {
		cleanup()
		return nil, false, err
	}
	handoffControl := newRemoteControlConn(conn)
	handoffRemote := &Remote{
		plan:         c.plan,
		transport:    c.transport,
		control:      handoffControl,
		identity:     state.identity,
		attachIntent: handoffIntent,
		attachment:   state.attachment,
	}
	handoffRemote.expectedRootID.Store(c.rootID())
	return &remoteSessionControl{
		sessionID:      sessionID,
		remote:         handoffRemote,
		initialControl: handoffControl,
	}, true, nil
}

func (c *Remote) installDraftHandoff(candidate *remoteSessionControl) error {
	if candidate == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed.Load() {
		c.mu.Unlock()
		return errors.Join(errors.New("remote client is closed"), candidate.remote.Close())
	}
	current := c.draftHandoff
	if current != nil && current.sessionID == candidate.sessionID {
		c.mu.Unlock()
		return candidate.remote.Close()
	}
	c.draftHandoff = candidate
	c.mu.Unlock()
	if current != nil {
		_ = current.remote.Close()
	}
	return nil
}

func (c *Remote) draftControl(ctx context.Context, sessionID string) (*remoteControlConn, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	c.mu.Lock()
	attachmentIntent := c.attachIntent
	handoff := c.draftHandoff
	c.mu.Unlock()
	attachedSessionID, attachedToSession := attachmentIntent.sessionID()
	if attachedToSession && attachedSessionID == sessionID {
		return c.ensureControl(ctx)
	}
	if handoff != nil && handoff.sessionID == sessionID {
		return handoff.remote.ensureControl(ctx)
	}
	return c.ensureControl(ctx)
}

// TakeSessionHandoff promotes the exact-Session connection prepared by the
// transcript subscription. The prepared socket predates any Session move, so
// promotion refreshes it once unless an earlier draft operation already did.
func (c *Remote) TakeSessionHandoff(ctx context.Context, sessionID string) (*Remote, bool, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, false, err
	}
	sessionID = strings.TrimSpace(sessionID)
	c.mu.Lock()
	handoff := c.draftHandoff
	c.mu.Unlock()
	if handoff == nil || handoff.sessionID != sessionID {
		return nil, false, nil
	}
	handoff.remote.mu.Lock()
	initialControl := handoff.remote.control
	if initialControl == handoff.initialControl {
		handoff.remote.control = nil
	}
	handoff.remote.mu.Unlock()
	if initialControl == handoff.initialControl {
		_ = initialControl.Close()
	}
	if _, err := handoff.remote.ensureControl(ctx); err != nil {
		return nil, true, err
	}
	c.mu.Lock()
	if c.draftHandoff != handoff {
		c.mu.Unlock()
		return nil, true, errors.New("Session handoff changed while reattaching")
	}
	c.draftHandoff = nil
	c.mu.Unlock()
	return handoff.remote, true, nil
}

func newRemoteControlConn(conn rpcwire.Conn) *remoteControlConn {
	control := &remoteControlConn{
		conn:    conn,
		pending: map[string]chan remoteControlResponse{},
		done:    make(chan struct{}),
	}
	go control.readLoop()
	return control
}

func (c *remoteControlConn) Close() error {
	c.fail(errRemoteClosed)
	return c.conn.Close()
}

func (c *remoteControlConn) IsDone() bool {
	if c == nil {
		return true
	}
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *remoteControlConn) readLoop() {
	for event := range c.conn.Events() {
		if event.Err != nil {
			c.fail(event.Err)
			return
		}
		var (
			correlation string
			response    remoteControlResponse
		)
		switch event.Frame.Kind {
		case rpcwire.FrameBinary:
			binary, id, err := decodeBinaryEnvelope(event.Frame.Payload)
			correlation = id
			if err != nil {
				response.err = err
			} else {
				response.binary = binary
			}
		default:
			continue
		}
		if strings.TrimSpace(correlation) == "" {
			continue
		}
		c.pendingMu.Lock()
		responseCh := c.pending[correlation]
		delete(c.pending, correlation)
		c.pendingMu.Unlock()
		if responseCh == nil {
			continue
		}
		responseCh <- response
	}
	c.fail(io.EOF)
}

func (c *remoteControlConn) registerPending(id string, responseCh chan remoteControlResponse) error {
	select {
	case <-c.done:
		return c.currentErr()
	default:
	}
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	select {
	case <-c.done:
		return c.currentErr()
	default:
	}
	c.pending[id] = responseCh
	return nil
}

func (c *remoteControlConn) removePending(id string) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *remoteControlConn) fail(err error) {
	c.failOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}
		c.errMu.Lock()
		c.err = err
		c.errMu.Unlock()
		close(c.done)
		c.pendingMu.Lock()
		c.pending = map[string]chan remoteControlResponse{}
		c.pendingMu.Unlock()
	})
}

func (c *remoteControlConn) currentErr() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.err == nil {
		return io.EOF
	}
	return c.err
}

// ErrServerRootMismatch reports that an attached (or reconnected) server reports
// a different persistence root than the client pinned via Remote.RequireRoot.
// Callers match it with errors.Is rather than comparing message text.
var ErrServerRootMismatch = errors.New("attached server reports a different persistence root than required")

// validateIdentityRoot enforces the pinned persistence-root id against a server
// identity. An empty expectedRootID disables validation. A server that does not
// report its root (empty id, e.g. an older build) is rejected when an id is
// required, since the whole point is to refuse instances whose root cannot be
// confirmed.
func validateIdentityRoot(expectedRootID string, identity protocol.ServerIdentity) error {
	if strings.TrimSpace(expectedRootID) == "" {
		return nil
	}
	if identity.PersistenceRootID != expectedRootID {
		return ErrServerRootMismatch
	}
	return nil
}

func receiveFrame(ctx context.Context, conn rpcwire.Conn) (rpcwire.Frame, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return rpcwire.Frame{}, ctx.Err()
	case event, ok := <-conn.Events():
		if !ok {
			return rpcwire.Frame{}, io.EOF
		}
		if event.Err != nil {
			return rpcwire.Frame{}, event.Err
		}
		return event.Frame, nil
	}
}
