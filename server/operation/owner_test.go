package operation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"core/internal/testharness/testsetup"
)

func TestShutdownPreservesIndependentFailureJoinedWithCancellation(t *testing.T) {
	for _, test := range []struct {
		name         string
		cancellation func(context.Context) error
	}{
		{"cause", context.Cause},
		{"context error", func(ctx context.Context) error { return ctx.Err() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			logs := testsetup.CaptureSlogRecords(t)
			owner := NewOperationOwner()
			started := make(chan struct{})
			failure := errors.New("cleanup failed")
			work, err := owner.Start(t.Context(), func(scope OperationScope) error {
				close(started)
				<-scope.Context().Done()
				return fmt.Errorf("operation cleanup: %w", errors.Join(test.cancellation(scope.Context()), failure))
			})
			if err != nil {
				t.Fatal(err)
			}
			<-started
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			if err := work.Await(t.Context()); !errors.Is(err, failure) {
				t.Fatalf("operation result lost independent failure: %v", err)
			}
			records := logs.Records()
			if len(records) != 1 || records[0].Level != slog.LevelError {
				t.Fatalf("combined failure diagnostics = %+v", records)
			}
			if diagnostic, ok := records[0].Fields["error"].(error); !ok || !errors.Is(diagnostic, failure) {
				t.Fatalf("diagnostic lost independent failure: %+v", records[0])
			}
		})
	}
}

func TestRunFailureRemainsObservableAfterCallerCancellation(t *testing.T) {
	logs := testsetup.CaptureSlogRecords(t)
	owner := NewOperationOwner()
	defer func() { _ = owner.Close() }()
	caller, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	failure := errors.New("operation failed")
	go func() {
		_, err := Run(caller, owner, func(OperationScope) (int, error) {
			close(started)
			<-release
			return 0, failure
		})
		result <- err
	}()
	<-started
	cancel()
	err := <-result
	close(release)
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("caller wait = %v", err)
	}
	records := logs.Records()
	if len(records) != 1 || records[0].Level != slog.LevelError {
		t.Fatalf("operation failure diagnostics = %+v", records)
	}
	if diagnostic, ok := records[0].Fields["error"].(error); !ok || !errors.Is(diagnostic, failure) {
		t.Fatalf("operation failure diagnostic = %+v", records[0])
	}
}

func TestOperationOwnerShutdownRejectsCancelsAndJoins(t *testing.T) {
	logs := testsetup.CaptureSlogRecords(t)
	owner := NewOperationOwner()
	started := make(chan struct{})
	canceled := make(chan error, 1)
	allowExit := make(chan struct{})
	operation, err := owner.Start(t.Context(), func(scope OperationScope) error {
		close(started)
		<-scope.Context().Done()
		canceled <- context.Cause(scope.Context())
		<-allowExit
		return context.Cause(scope.Context())
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started

	closed := make(chan error, 1)
	go func() { closed <- owner.Close() }()
	if err := <-canceled; !errors.Is(err, ErrOperationOwnerClosed) {
		t.Fatalf("operation cancellation = %v, want owner shutdown", err)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned before operation joined: %v", err)
	default:
	}
	if _, err := owner.Start(t.Context(), func(OperationScope) error { return nil }); !errors.Is(err, ErrOperationOwnerClosed) {
		t.Fatalf("Start after shutdown error = %v, want closed owner", err)
	}
	close(allowExit)
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := operation.Await(t.Context()); !errors.Is(err, ErrOperationOwnerClosed) {
		t.Fatalf("operation result = %v, want shutdown cancellation", err)
	}
	if records := logs.Records(); len(records) != 0 {
		t.Fatalf("expected shutdown cancellation emitted failure diagnostics: %+v", records)
	}
}

func TestRunCallerCancellationOnlyEndsWaiting(t *testing.T) {
	logs := testsetup.CaptureSlogRecords(t)
	owner := NewOperationOwner()
	defer func() { _ = owner.Close() }()
	caller, cancel := context.WithCancel(t.Context())
	started := make(chan context.Context, 1)
	finished := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := Run(caller, owner, func(scope OperationScope) (int, error) {
			started <- scope.Context()
			<-scope.Context().Done()
			close(finished)
			return 42, scope.Context().Err()
		})
		result <- err
	}()
	scope := <-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller wait = %v", err)
	}
	if scope.Err() != nil {
		t.Fatalf("caller canceled server work: %v", context.Cause(scope))
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	<-finished
	if records := logs.Records(); len(records) != 0 {
		t.Fatalf("caller or shutdown cancellation emitted failure diagnostics: %+v", records)
	}
}

func TestRunReturnsFailureToAttachedCaller(t *testing.T) {
	logs := testsetup.CaptureSlogRecords(t)
	owner := NewOperationOwner()
	defer func() { _ = owner.Close() }()
	failure := errors.New("operation failed")
	_, err := Run(t.Context(), owner, func(OperationScope) (int, error) {
		return 0, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("attached caller error = %v", err)
	}
	if records := logs.Records(); len(records) != 1 {
		t.Fatalf("operation failure diagnostics = %+v", records)
	}
}
