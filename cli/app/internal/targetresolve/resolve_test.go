package targetresolve

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolveUsesRemoteBeforeDaemonAndEmbedded(t *testing.T) {
	calls := make([]string, 0)
	result, err := Resolve(context.Background(), Request[string]{
		DialRemote: func(context.Context) (Candidate[string], bool, error) {
			calls = append(calls, "remote")
			return Candidate[string]{Target: "remote"}, true, nil
		},
		LaunchDaemon: func(context.Context) (Candidate[string], bool, error) {
			calls = append(calls, "daemon")
			return Candidate[string]{Target: "daemon"}, true, nil
		},
		StartEmbedded: func(context.Context) (Candidate[string], error) {
			calls = append(calls, "embedded")
			return Candidate[string]{Target: "embedded"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if result.Target != "remote" {
		t.Fatalf("target = %q, want remote", result.Target)
	}
	if got := strings.Join(calls, ","); got != "remote" {
		t.Fatalf("calls = %s, want remote", got)
	}
}

func TestResolveBypassSkipsRemoteAndDaemon(t *testing.T) {
	calls := make([]string, 0)
	result, err := Resolve(context.Background(), Request[string]{
		BypassRemote: func(context.Context) (bool, error) {
			calls = append(calls, "bypass")
			return true, nil
		},
		DialRemote: func(context.Context) (Candidate[string], bool, error) {
			calls = append(calls, "remote")
			return Candidate[string]{Target: "remote"}, true, nil
		},
		LaunchDaemon: func(context.Context) (Candidate[string], bool, error) {
			calls = append(calls, "daemon")
			return Candidate[string]{Target: "daemon"}, true, nil
		},
		StartEmbedded: func(context.Context) (Candidate[string], error) {
			calls = append(calls, "embedded")
			return Candidate[string]{Target: "embedded"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if result.Target != "embedded" {
		t.Fatalf("target = %q, want embedded", result.Target)
	}
	if got := strings.Join(calls, ","); got != "bypass,embedded" {
		t.Fatalf("calls = %s, want bypass,embedded", got)
	}
}

func TestResolveFallsBackToEmbeddedWhenRemoteAndDaemonUnavailable(t *testing.T) {
	result, err := Resolve(context.Background(), Request[string]{
		DialRemote: func(context.Context) (Candidate[string], bool, error) {
			return Candidate[string]{}, false, nil
		},
		LaunchDaemon: func(context.Context) (Candidate[string], bool, error) {
			return Candidate[string]{}, false, nil
		},
		StartEmbedded: func(context.Context) (Candidate[string], error) {
			return Candidate[string]{Target: "embedded"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if result.Target != "embedded" {
		t.Fatalf("target = %q, want embedded", result.Target)
	}
}

func TestResolveClosesCandidateWhenValidationFails(t *testing.T) {
	validationErr := errors.New("auth failed")
	closed := false
	_, err := Resolve(context.Background(), Request[string]{
		DialRemote: func(context.Context) (Candidate[string], bool, error) {
			return Candidate[string]{
				Target: "remote",
				Close: func() error {
					closed = true
					return nil
				},
			}, true, nil
		},
		StartEmbedded: func(context.Context) (Candidate[string], error) {
			return Candidate[string]{Target: "embedded"}, nil
		},
		Validate: func(context.Context, Candidate[string]) error {
			return validationErr
		},
	})
	if !errors.Is(err, validationErr) {
		t.Fatalf("error = %v, want validation error", err)
	}
	if !closed {
		t.Fatal("expected failed candidate to close")
	}
}

func TestResolveClosesDaemonCandidateWhenValidationFails(t *testing.T) {
	validationErr := errors.New("auth failed")
	closed := false
	_, err := Resolve(context.Background(), Request[string]{
		DialRemote: func(context.Context) (Candidate[string], bool, error) {
			return Candidate[string]{}, false, nil
		},
		LaunchDaemon: func(context.Context) (Candidate[string], bool, error) {
			return Candidate[string]{
				Target: "daemon",
				Close: func() error {
					closed = true
					return nil
				},
			}, true, nil
		},
		StartEmbedded: func(context.Context) (Candidate[string], error) {
			return Candidate[string]{Target: "embedded"}, nil
		},
		Validate: func(_ context.Context, candidate Candidate[string]) error {
			if candidate.Source != SourceDaemon {
				t.Fatalf("source = %q, want daemon", candidate.Source)
			}
			return validationErr
		},
	})
	if !errors.Is(err, validationErr) {
		t.Fatalf("error = %v, want validation error", err)
	}
	if !closed {
		t.Fatal("expected failed daemon candidate to close")
	}
}

func TestResolveClosesEmbeddedCandidateWhenValidationFails(t *testing.T) {
	validationErr := errors.New("project missing")
	closed := false
	_, err := Resolve(context.Background(), Request[string]{
		DialRemote: func(context.Context) (Candidate[string], bool, error) {
			return Candidate[string]{}, false, nil
		},
		LaunchDaemon: func(context.Context) (Candidate[string], bool, error) {
			return Candidate[string]{}, false, nil
		},
		StartEmbedded: func(context.Context) (Candidate[string], error) {
			return Candidate[string]{
				Target: "embedded",
				Close: func() error {
					closed = true
					return nil
				},
			}, nil
		},
		Validate: func(_ context.Context, candidate Candidate[string]) error {
			if candidate.Source != SourceEmbedded {
				t.Fatalf("source = %q, want embedded", candidate.Source)
			}
			return validationErr
		},
	})
	if !errors.Is(err, validationErr) {
		t.Fatalf("error = %v, want validation error", err)
	}
	if !closed {
		t.Fatal("expected failed embedded candidate to close")
	}
}

func TestResolveJoinsDaemonLaunchAndEmbeddedErrors(t *testing.T) {
	launchErr := errors.New("launch failed")
	embeddedErr := errors.New("embedded failed")
	_, err := Resolve(context.Background(), Request[string]{
		LaunchDaemon: func(context.Context) (Candidate[string], bool, error) {
			return Candidate[string]{}, false, launchErr
		},
		StartEmbedded: func(context.Context) (Candidate[string], error) {
			return Candidate[string]{}, embeddedErr
		},
	})
	if !errors.Is(err, launchErr) || !errors.Is(err, embeddedErr) {
		t.Fatalf("error = %v, want joined launch and embedded errors", err)
	}
}
