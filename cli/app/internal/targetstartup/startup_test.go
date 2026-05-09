package targetstartup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolveBypassStartsEmbedded(t *testing.T) {
	calls := make([]string, 0)
	target, err := Resolve[string, string](context.Background(), Request[string, string]{
		BypassRemote: func(context.Context) (bool, error) {
			calls = append(calls, "bypass")
			return true, nil
		},
		DialRemote: func(context.Context) (Target[string], bool, error) {
			calls = append(calls, "remote")
			return Target[string]{Value: "remote"}, true, nil
		},
		LaunchDaemon: func(context.Context) (DaemonTarget[string], bool, error) {
			calls = append(calls, "daemon")
			return DaemonTarget[string]{Value: "daemon"}, true, nil
		},
		StartEmbedded: func(context.Context) (Target[string], error) {
			calls = append(calls, "embedded")
			return Target[string]{Value: "embedded"}, nil
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target.Value != "embedded" {
		t.Fatalf("target = %q, want embedded", target.Value)
	}
	if got := strings.Join(calls, ","); got != "bypass,embedded" {
		t.Fatalf("calls = %s, want bypass,embedded", got)
	}
}

func TestResolveWrapsDaemonAndUsesWrappedClose(t *testing.T) {
	closed := false
	target, err := Resolve[string, string](context.Background(), Request[string, string]{
		LaunchDaemon: func(context.Context) (DaemonTarget[string], bool, error) {
			return DaemonTarget[string]{
				Value: "daemon",
				Close: func() error {
					t.Fatal("raw daemon close should not be used when wrapper provides close")
					return nil
				},
			}, true, nil
		},
		WrapDaemon: func(_ context.Context, daemon DaemonTarget[string]) (Target[string], error) {
			return Target[string]{
				Value: "wrapped:" + daemon.Value,
				Close: func() error {
					closed = true
					return nil
				},
			}, nil
		},
		StartEmbedded: func(context.Context) (Target[string], error) {
			return Target[string]{Value: "embedded"}, nil
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target.Value != "wrapped:daemon" {
		t.Fatalf("target = %q, want wrapped daemon", target.Value)
	}
	if err := target.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !closed {
		t.Fatal("expected wrapped close")
	}
}

func TestResolveClosesDaemonWhenWrapFailsThenFallsBackToEmbedded(t *testing.T) {
	wrapErr := errors.New("wrap failed")
	closed := false
	target, err := Resolve[string, string](context.Background(), Request[string, string]{
		LaunchDaemon: func(context.Context) (DaemonTarget[string], bool, error) {
			return DaemonTarget[string]{
				Value: "daemon",
				Close: func() error {
					closed = true
					return nil
				},
			}, true, nil
		},
		WrapDaemon: func(context.Context, DaemonTarget[string]) (Target[string], error) {
			return Target[string]{}, wrapErr
		},
		StartEmbedded: func(context.Context) (Target[string], error) {
			return Target[string]{Value: "embedded"}, nil
		},
	})
	if err != nil {
		t.Fatalf("resolve should fall back to embedded: %v", err)
	}
	if target.Value != "embedded" {
		t.Fatalf("target = %q, want embedded", target.Value)
	}
	if !closed {
		t.Fatal("expected daemon close on wrap failure")
	}
}

func TestResolveRequiresDaemonWrapper(t *testing.T) {
	closed := false
	target, err := Resolve[string, string](context.Background(), Request[string, string]{
		LaunchDaemon: func(context.Context) (DaemonTarget[string], bool, error) {
			return DaemonTarget[string]{
				Value: "daemon",
				Close: func() error {
					closed = true
					return nil
				},
			}, true, nil
		},
		StartEmbedded: func(context.Context) (Target[string], error) {
			return Target[string]{Value: "embedded"}, nil
		},
	})
	if err != nil {
		t.Fatalf("resolve should fall back to embedded: %v", err)
	}
	if target.Value != "embedded" {
		t.Fatalf("target = %q, want embedded", target.Value)
	}
	if !closed {
		t.Fatal("expected daemon close when wrapper is missing")
	}
}

func TestResolvePassesSourceToValidation(t *testing.T) {
	var source Source
	_, err := Resolve[string, string](context.Background(), Request[string, string]{
		DialRemote: func(context.Context) (Target[string], bool, error) {
			return Target[string]{Value: "remote"}, true, nil
		},
		StartEmbedded: func(context.Context) (Target[string], error) {
			return Target[string]{Value: "embedded"}, nil
		},
		Validate: func(_ context.Context, candidateSource Source, target string) error {
			source = candidateSource
			if target != "remote" {
				t.Fatalf("target = %q, want remote", target)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if source != SourceRemote {
		t.Fatalf("source = %q, want remote", source)
	}
}
