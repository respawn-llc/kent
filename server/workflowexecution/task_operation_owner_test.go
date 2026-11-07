package workflowexecution

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
)

func TestTaskOperationDetachesCallerAndDrainsNestedSynchronousWork(t *testing.T) {
	controller := newCurrentNodeQuestionFixture(t).controller
	caller, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	nested := make(chan error, 1)
	done, err := controller.StartTaskOperation(caller, func(owned context.Context) error {
		entered <- owned
		<-owned.Done()
		return controller.RunTaskOperation(owned, func(ctx context.Context) error {
			nested <- context.Cause(ctx)
			<-release
			return context.Cause(ctx)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	owned := <-entered
	cancel()
	if owned.Err() != nil {
		t.Fatalf("caller canceled accepted work: %v", owned.Err())
	}
	closed := make(chan error, 1)
	go func() { closed <- controller.Close() }()
	select {
	case cause := <-nested:
		if cause == nil {
			t.Fatal("shutdown did not cancel nested work")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admitted operation could not finish nested work during shutdown")
	}
	var called atomic.Bool
	if _, err := controller.StartTaskOperation(t.Context(), func(context.Context) error {
		called.Store(true)
		return nil
	}); err == nil || called.Load() {
		t.Fatalf("shutdown admitted new work: called=%v, err=%v", called.Load(), err)
	}
	select {
	case <-done:
		t.Fatal("nested synchronous operation returned before its callback")
	case <-closed:
		t.Fatal("shutdown returned before accepted work")
	default:
	}
	unblock()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not join accepted work")
	}
	if err := <-done; err != context.Cause(owned) {
		t.Fatalf("completion = %v, want owner cancellation %v", err, context.Cause(owned))
	}
}

func TestTaskOperationAdmissionRacesShutdown(t *testing.T) {
	controller := newCurrentNodeQuestionFixture(t).controller
	begin := make(chan struct{})
	var callers sync.WaitGroup
	var accepted, completed atomic.Int32
	first, err := controller.StartTaskOperation(t.Context(), func(ctx context.Context) error {
		<-ctx.Done()
		completed.Add(1)
		return context.Cause(ctx)
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted.Add(1)
	for range 100 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-begin
			done, err := controller.StartTaskOperation(t.Context(), func(ctx context.Context) error {
				<-ctx.Done()
				completed.Add(1)
				return context.Cause(ctx)
			})
			if err != nil {
				return
			}
			accepted.Add(1)
			if err := <-done; err == nil {
				t.Error("shutdown operation lost cancellation")
			}
		}()
	}
	close(begin)
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	callers.Wait()
	if err := <-first; err == nil {
		t.Fatal("initial accepted operation lost shutdown cancellation")
	}
	if accepted.Load() != completed.Load() {
		t.Fatalf("accepted %d, completed %d", accepted.Load(), completed.Load())
	}
}

func TestTaskOperationShutdownLogsIndependentFailuresButNotExactCancellation(t *testing.T) {
	for _, test := range []struct {
		name         string
		cancellation func(context.Context) error
	}{
		{"cause", context.Cause},
		{"context error", func(ctx context.Context) error { return ctx.Err() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			controller := newCurrentNodeQuestionFixture(t).controller
			logs := testsetup.CaptureSlogRecords(t)
			entered := make(chan struct{}, 2)
			failure := errors.New("finalization failed")
			failed, err := controller.StartTaskOperation(t.Context(), func(ctx context.Context) error {
				entered <- struct{}{}
				<-ctx.Done()
				return errors.Join(test.cancellation(ctx), failure)
			})
			if err != nil {
				t.Fatal(err)
			}
			canceled, err := controller.StartTaskOperation(t.Context(), func(ctx context.Context) error {
				entered <- struct{}{}
				<-ctx.Done()
				return test.cancellation(ctx)
			})
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			<-entered
			if err := controller.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-failed; !errors.Is(err, failure) {
				t.Fatalf("completion lost independent failure: %v", err)
			}
			if err := <-canceled; err == nil {
				t.Fatal("completion lost cancellation")
			}
			records := logs.Records()
			if len(records) != 1 || records[0].Level != slog.LevelError {
				t.Fatalf("failure diagnostics = %+v", records)
			}
			if diagnostic, ok := records[0].Fields["error"].(error); !ok || !errors.Is(diagnostic, failure) {
				t.Fatalf("diagnostic lost independent failure: %+v", records[0])
			}
		})
	}
}

func TestRunTaskOperationStaysSynchronousAndForeignScopeCannotBypassShutdown(t *testing.T) {
	first := newCurrentNodeQuestionFixture(t).controller
	second := newCurrentNodeQuestionFixture(t).controller
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("callback failed")
	var ran bool
	err := first.RunTaskOperation(t.Context(), func(ctx context.Context) error {
		if err := second.RunTaskOperation(ctx, func(context.Context) error {
			t.Error("foreign lifecycle scope bypassed shutdown")
			return nil
		}); err == nil {
			t.Error("closed controller accepted a foreign scope")
		}
		ran = true
		return sentinel
	})
	if !ran || err != sentinel {
		t.Fatalf("synchronous operation = %v, ran=%v", err, ran)
	}
}
