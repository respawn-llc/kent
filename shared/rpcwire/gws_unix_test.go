//go:build unix

package rpcwire

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"core/shared/protocol"
)

func TestWebSocketTransportUnixRoundTrip(t *testing.T) {
	transport := NewWebSocketTransport()
	serverErr := make(chan error, 1)
	socketPath := shortUnixSocketPath("gws-roundtrip")
	defer func() { _ = os.Remove(socketPath) }()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen unix: %v", err)
	}
	httpServer := &http.Server{Handler: transport.Handler(func(ctx context.Context, conn Conn) {
		select {
		case event, ok := <-conn.Events():
			if !ok {
				serverErr <- context.Canceled
				return
			}
			if event.Err != nil {
				serverErr <- event.Err
				return
			}
			serverErr <- conn.Send(ctx, event.Frame)
		case <-ctx.Done():
			serverErr <- ctx.Err()
		}
	})}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()
	defer func() {
		if err := httpServer.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Close server: %v", err)
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve server: %v", err)
		}
	}()

	endpoint, err := NewUnixEndpoint(socketPath, protocol.RPCPath)
	if err != nil {
		t.Fatalf("NewUnixEndpoint: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := transport.Dial(ctx, endpoint)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	request := Frame{Kind: FrameBinary, Payload: []byte{0, 1, 2, 255}}
	if err := conn.Send(ctx, request); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case event, ok := <-conn.Events():
		if !ok {
			t.Fatal("Events closed before response")
		}
		if event.Err != nil {
			t.Fatalf("Events error: %v", event.Err)
		}
		if event.Frame.Kind != FrameBinary || !bytes.Equal(event.Frame.Payload, request.Payload) {
			t.Fatalf("response = %#v, want %#v", event.Frame, request)
		}
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for response: %v", ctx.Err())
	}

	if err := <-serverErr; err != nil {
		t.Fatalf("Server handler: %v", err)
	}
}

func shortUnixSocketPath(prefix string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d.sock", prefix, time.Now().UnixNano()))
}
