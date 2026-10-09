package rpcwire

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebSocketTransportRoundTrip(t *testing.T) {
	transport := NewWebSocketTransport()
	serverErr := make(chan error, 1)
	server := httptest.NewServer(transport.Handler(func(ctx context.Context, conn Conn) {
		for _, want := range []Frame{
			{Kind: FrameBinary, Payload: []byte{0, 1, 2, 255}},
		} {
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
				if event.Frame.Kind != want.Kind || !bytes.Equal(event.Frame.Payload, want.Payload) {
					serverErr <- errors.New("received frame differs from sent frame")
					return
				}
				if err := conn.Send(ctx, event.Frame); err != nil {
					serverErr <- err
					return
				}
			case <-ctx.Done():
				serverErr <- ctx.Err()
				return
			}
		}
		serverErr <- nil
	}))
	defer server.Close()

	endpoint, err := ParseWebSocketEndpoint("ws" + server.URL[len("http"):])
	if err != nil {
		t.Fatalf("ParseWebSocketEndpoint: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := transport.Dial(ctx, endpoint)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	for _, want := range []Frame{
		{Kind: FrameBinary, Payload: []byte{0, 1, 2, 255}},
	} {
		if err := conn.Send(ctx, want); err != nil {
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
			if event.Frame.Kind != want.Kind || !bytes.Equal(event.Frame.Payload, want.Payload) {
				t.Fatalf("Received frame = %#v, want %#v", event.Frame, want)
			}
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for response: %v", ctx.Err())
		}
	}

	if err := <-serverErr; err != nil {
		t.Fatalf("Server handler: %v", err)
	}
}

func TestWebSocketTransportSecureRoundTrip(t *testing.T) {
	transport := NewWebSocketTransport()
	serverErr := make(chan error, 1)
	server := httptest.NewTLSServer(transport.Handler(func(ctx context.Context, conn Conn) {
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
	}))
	defer server.Close()

	endpoint, err := ParseWebSocketEndpoint("wss" + server.URL[len("https"):])
	if err != nil {
		t.Fatalf("ParseWebSocketEndpoint: %v", err)
	}
	serverTransport, ok := server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("server client transport type = %T, want *http.Transport", server.Client().Transport)
	}
	endpoint.TLSConfig = serverTransport.TLSClientConfig.Clone()

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
