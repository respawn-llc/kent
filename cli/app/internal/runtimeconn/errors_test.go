package runtimeconn

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"testing"

	"builder/server/llm"
	"builder/shared/llmerrors"
	"builder/shared/serverapi"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "embedded api status", err: &llm.APIStatusError{StatusCode: 429, Body: "rate limit"}, want: false},
		{name: "remote api status dto", err: &llmerrors.APIStatusError{StatusCode: 429, Body: "rate limit"}, want: false},
		{name: "stream gap", err: serverapi.ErrStreamGap, want: false},
		{name: "stream unavailable", err: serverapi.ErrStreamUnavailable, want: false},
		{name: "stream failed", err: serverapi.ErrStreamFailed, want: false},
		{name: "context deadline", err: context.DeadlineExceeded, want: false},
		{name: "net timeout", err: timeoutError{}, want: false},
		{name: "eof", err: io.EOF, want: true},
		{name: "url", err: &url.Error{Op: "Get", URL: "http://127.0.0.1", Err: io.EOF}, want: true},
		{name: "op", err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("reset")}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsConnectionError(tc.err); got != tc.want {
				t.Fatalf("IsConnectionError(%T)=%v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestConfirmsReachability(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: true},
		{name: "embedded api status", err: &llm.APIStatusError{StatusCode: 429, Body: "rate limit"}, want: true},
		{name: "remote api status dto", err: &llmerrors.APIStatusError{StatusCode: 429, Body: "rate limit"}, want: true},
		{name: "stream gap", err: serverapi.ErrStreamGap, want: false},
		{name: "stream unavailable", err: serverapi.ErrStreamUnavailable, want: false},
		{name: "stream failed", err: serverapi.ErrStreamFailed, want: false},
		{name: "context canceled", err: context.Canceled, want: false},
		{name: "net timeout", err: timeoutError{}, want: false},
		{name: "eof", err: io.EOF, want: false},
		{name: "generic", err: errors.New("validation failed"), want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConfirmsReachability(tc.err); got != tc.want {
				t.Fatalf("ConfirmsReachability(%T)=%v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "deadline", err: context.DeadlineExceeded, want: true},
		{name: "canceled", err: context.Canceled, want: true},
		{name: "net timeout", err: timeoutError{}, want: true},
		{name: "eof", err: io.EOF, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTimeoutError(tc.err); got != tc.want {
				t.Fatalf("IsTimeoutError(%T)=%v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
