package rpcwire

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Frame struct {
	Kind    FrameKind
	Payload []byte
}

type FrameKind uint8

const (
	FrameText FrameKind = iota + 1
	FrameBinary
)

type Event struct {
	Frame Frame
	Err   error
}

type Conn interface {
	Send(context.Context, Frame) error
	Events() <-chan Event
	Closed() <-chan struct{}
	Close() error
}

type ClientTransport interface {
	Dial(context.Context, Endpoint) (Conn, error)
}

type ServerTransport interface {
	Handler(func(context.Context, Conn)) http.Handler
}

const (
	defaultWebSocketHandshakeTimeout = 5 * time.Second
	webSocketMessageBytes            = 16 << 20
)

type WebSocketTransport struct{}

func NewWebSocketTransport() WebSocketTransport { return WebSocketTransport{} }

func (WebSocketTransport) Dial(ctx context.Context, endpoint Endpoint) (Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	base, err := webSocketHTTPTransport(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: base, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	handshake, cancel := context.WithTimeout(ctx, webSocketHandshakeTimeout(ctx))
	defer cancel()
	socket, response, err := websocket.Dial(handshake, endpoint.ServerURL, &websocket.DialOptions{
		HTTPClient: client, HTTPHeader: http.Header{"Origin": []string{endpoint.OriginURL}},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, err
	}
	return newWebSocketConn(socket), nil
}

func (WebSocketTransport) Handler(handler func(context.Context, Conn)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controller := http.NewResponseController(w)
		if err := controller.SetWriteDeadline(time.Now().Add(webSocketHandshakeTimeout(r.Context()))); err != nil {
			slog.ErrorContext(r.Context(), "set websocket handshake deadline", "error", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// Preserve the existing server's unrestricted Origin policy.
			InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled,
		})
		// The deadline bounds the upgrade, never the connection's lifetime.
		deadlineErr := controller.SetWriteDeadline(time.Time{})
		if err != nil {
			return
		}
		if deadlineErr != nil {
			_ = socket.CloseNow()
			slog.ErrorContext(r.Context(), "clear websocket handshake deadline", "error", deadlineErr)
			return
		}
		adapter := newWebSocketConn(socket)
		defer adapter.Close()
		handler(r.Context(), adapter)
	})
}

type webSocketConn struct {
	socket    *websocket.Conn
	events    chan Event
	closed    chan struct{}
	closeOnce sync.Once
}

func newWebSocketConn(socket *websocket.Conn) *webSocketConn {
	socket.SetReadLimit(webSocketMessageBytes)
	conn := &webSocketConn{socket: socket, events: make(chan Event, 16), closed: make(chan struct{})}
	go conn.read()
	return conn
}

func (c *webSocketConn) Send(ctx context.Context, frame Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if frame.Kind != FrameBinary {
		return fmt.Errorf("outbound websocket frame must be binary, got kind %d", frame.Kind)
	}
	if len(frame.Payload) > webSocketMessageBytes {
		return fmt.Errorf("outbound websocket message exceeds %d bytes", webSocketMessageBytes)
	}
	return c.socket.Write(ctx, websocket.MessageBinary, frame.Payload)
}

func (c *webSocketConn) Events() <-chan Event    { return c.events }
func (c *webSocketConn) Closed() <-chan struct{} { return c.closed }

func (c *webSocketConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.socket.CloseNow()
	})
	return err
}

func (c *webSocketConn) read() {
	defer close(c.events)
	defer c.Close()
	for {
		// The handshake context can expire after a successful upgrade. Only
		// closing this connection owns the lifetime of its reader.
		kind, payload, err := c.socket.Read(context.Background())
		if err != nil {
			select {
			case <-c.closed:
				return
			default:
			}
			switch websocket.CloseStatus(err) {
			case websocket.StatusNormalClosure, websocket.StatusGoingAway:
				err = errors.Join(io.EOF, err)
			}
			c.publish(Event{Err: err})
			return
		}
		frameKind := FrameBinary
		if kind == websocket.MessageText {
			frameKind = FrameText
		}
		if !c.publish(Event{Frame: Frame{Kind: frameKind, Payload: payload}}) {
			return
		}
	}
}

func (c *webSocketConn) publish(event Event) bool {
	select {
	case c.events <- event:
		return true
	case <-c.closed:
		return false
	}
}

func webSocketHTTPTransport(ctx context.Context, endpoint Endpoint) (http.RoundTripper, error) {
	if endpoint.Transport == TransportUnix {
		return &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", endpoint.Address)
		}}, nil
	}
	if !endpoint.UseTLS || endpoint.TLSConfig == nil {
		return http.DefaultTransport, nil
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("destination TLS override requires a standard HTTP transport")
	}
	local := base.Clone()
	local.TLSClientConfig = endpoint.TLSConfig.Clone()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.ServerURL, nil)
	if err != nil {
		return nil, err
	}
	if endpoint.UseTLS && base.Proxy != nil {
		request.URL.Scheme = "https"
		proxy, err := base.Proxy(request)
		if err != nil {
			return nil, err
		}
		if proxy != nil && proxy.Scheme == "https" {
			local.DialTLSContext = proxyTLSDialer(base, proxy)
		}
	}
	return local, nil
}

func proxyTLSDialer(base *http.Transport, proxy *url.URL) func(context.Context, string, string) (net.Conn, error) {
	policy := &tls.Config{}
	if base.TLSClientConfig != nil {
		policy = base.TLSClientConfig.Clone()
	}
	policy.ServerName = proxy.Hostname()
	// HTTP/1.1 CONNECT is handled by net/http, not an independently negotiated
	// HTTP/2 proxy connection.
	policy.NextProtos = []string{"http/1.1"}
	dial := base.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		raw, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		conn := tls.Client(raw, policy)
		if base.TLSHandshakeTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, base.TLSHandshakeTimeout)
			defer cancel()
		}
		if err := conn.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return conn, nil
	}
}

func webSocketHandshakeTimeout(ctx context.Context) time.Duration {
	if ctx == nil {
		return defaultWebSocketHandshakeTimeout
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 {
			return remaining
		}
		return time.Millisecond
	}
	return defaultWebSocketHandshakeTimeout
}
