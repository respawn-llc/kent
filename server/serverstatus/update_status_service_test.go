package serverstatus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"core/shared/protoapi"
	serverpb "core/shared/protoapi/gen/kent/api/server"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestGetUpdateStatusIsolatesConcurrentAndCachedResponses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newControlledReleaseSource(releaseMetadata{Version: updateVersion{components: [3]uint64{1, 2, 0}}}, nil)
		updates := newUpdateStatusService("1.1.0", false, source, time.Now)
		defer requireUpdateStatusServiceClosed(t, updates)
		service := &ServerStatusService{updates: updates}
		synctest.Wait()
		if calls := source.calls.Load(); calls != 0 {
			t.Fatalf("release calls after construction = %d, want 0", calls)
		}
		responses := make(chan *serverpb.GetUpdateStatusSuccess, 2)
		for range 2 {
			go func() {
				response, err := service.GetUpdateStatus(context.Background(), &emptypb.Empty{})
				if err != nil {
					t.Errorf("GetUpdateStatus: %v", err)
				}
				responses <- response
			}()
		}
		synctest.Wait()
		if calls := source.calls.Load(); calls != 1 {
			t.Fatalf("pending release calls = %d, want 1", calls)
		}
		close(source.release)
		first, second := <-responses, <-responses
		want := &serverpb.UpdateStatus{Status: &serverpb.UpdateStatus_Available{
			Available: &serverpb.UpdateVersions{CurrentVersion: "1.1.0", LatestVersion: "1.2.0"},
		}}
		assertStatus := func(response *serverpb.GetUpdateStatusSuccess) {
			t.Helper()
			if response == nil || !proto.Equal(response.Status, want) {
				t.Fatalf("response = %v, want status %v", response, want)
			}
		}
		assertStatus(first)
		assertStatus(second)
		first.Status.GetAvailable().CurrentVersion = "9.0.0"
		first.Status.GetAvailable().LatestVersion = "10.0.0"
		assertStatus(second)
		first.Status.Status = &serverpb.UpdateStatus_CheckUnavailable{CheckUnavailable: &emptypb.Empty{}}
		assertStatus(second)

		for range 2 {
			cached, err := service.GetUpdateStatus(context.Background(), &emptypb.Empty{})
			if err != nil {
				t.Fatalf("cached GetUpdateStatus: %v", err)
			}
			assertStatus(cached)
			cached.Status.GetAvailable().LatestVersion = "11.0.0"
			assertStatus(second)
			cached.Status.Status = &serverpb.UpdateStatus_CheckUnavailable{CheckUnavailable: &emptypb.Empty{}}
		}
		if calls := source.calls.Load(); calls != 1 {
			t.Fatalf("release calls = %d, want 1", calls)
		}
	})
}

func TestUpdateStatusServiceCachesFailedAttempt(t *testing.T) {
	source := &countingReleaseSource{err: &releaseHTTPStatusError{StatusCode: 503, Status: "503 Service Unavailable"}}
	service := newUpdateStatusService("1.1.0", false, source, time.Now)
	t.Cleanup(func() { requireUpdateStatusServiceClosed(t, service) })

	for range 2 {
		result, err := service.status(context.Background())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if result.GetCheckFailed() == nil {
			t.Fatalf("result = %v, want check_failed", result)
		}
	}
	if calls := source.calls.Load(); calls != 1 {
		t.Fatalf("release calls = %d, want 1", calls)
	}
}

func TestUpdateStatusServiceBoundsReleaseAttempt(t *testing.T) {
	source := &blockingReleaseSource{}
	service := newUpdateStatusService("1.1.0", false, source, time.Now)
	t.Cleanup(func() { requireUpdateStatusServiceClosed(t, service) })

	ctx, cancel := context.WithTimeout(context.Background(), updateStatusTimeout+time.Second)
	defer cancel()
	result, err := service.status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if result.GetCheckUnavailable() == nil {
		t.Fatalf("result = %v, want check_unavailable", result)
	}
}

func TestUpdateStatusServiceRefreshesCompletedAttemptAtOneHour(t *testing.T) {
	now := time.Date(2026, time.July, 18, 10, 0, 0, 0, time.UTC)
	source := &sequenceReleaseSource{results: []releaseMetadata{
		{Version: updateVersion{components: [3]uint64{1, 1, 0}}},
		{Version: updateVersion{components: [3]uint64{1, 2, 0}}},
	}}
	service := newUpdateStatusService("1.1.0", false, source, func() time.Time { return now })
	t.Cleanup(func() { requireUpdateStatusServiceClosed(t, service) })

	first, err := service.status(context.Background())
	if err != nil {
		t.Fatalf("first Status: %v", err)
	}
	if first.GetCurrent() == nil {
		t.Fatalf("first result = %v, want current", first)
	}
	now = now.Add(updateStatusFreshness - time.Nanosecond)
	beforeExpiry, err := service.status(context.Background())
	if err != nil {
		t.Fatalf("Status before expiry: %v", err)
	}
	if beforeExpiry.GetCurrent() == nil {
		t.Fatalf("before-expiry result = %v, want current", beforeExpiry)
	}
	now = now.Add(time.Nanosecond)
	afterExpiry, err := service.status(context.Background())
	if err != nil {
		t.Fatalf("Status at expiry: %v", err)
	}
	if afterExpiry.GetAvailable() == nil {
		t.Fatalf("at-expiry result = %v, want available", afterExpiry)
	}
	if calls := source.calls.Load(); calls != 2 {
		t.Fatalf("release calls = %d, want 2", calls)
	}
}

func TestUpdateStatusCallerCancellationIsLocal(t *testing.T) {
	source := newControlledReleaseSource(releaseMetadata{Version: updateVersion{components: [3]uint64{1, 2, 0}}}, nil)
	service := newUpdateStatusService("1.1.0", false, source, time.Now)
	t.Cleanup(func() { requireUpdateStatusServiceClosed(t, service) })

	ctx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan error, 1)
	go func() {
		_, err := service.status(ctx)
		cancelled <- err
	}()
	<-source.started
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Status error = %v, want context canceled", err)
	}

	completed := make(chan *serverpb.UpdateStatus, 1)
	completionErrors := make(chan error, 1)
	go func() {
		result, err := service.status(context.Background())
		completed <- result
		completionErrors <- err
	}()
	if calls := source.calls.Load(); calls != 1 {
		t.Fatalf("release calls after caller cancellation = %d, want 1", calls)
	}
	close(source.release)
	if err := <-completionErrors; err != nil {
		t.Fatalf("Status after cancellation: %v", err)
	}
	if result := <-completed; result.GetAvailable() == nil {
		t.Fatalf("result = %v, want available", result)
	}
}

func TestUpdateStatusServiceCloseWakesPendingCallers(t *testing.T) {
	source := newControlledReleaseSource(releaseMetadata{Version: updateVersion{components: [3]uint64{1, 2, 0}}}, nil)
	service := newUpdateStatusService("1.1.0", false, source, time.Now)

	statusErrors := make(chan error, 1)
	go func() {
		result, err := service.status(context.Background())
		if result != nil {
			t.Errorf("closed pending Status returned result: %v", result)
		}
		statusErrors <- err
	}()
	<-source.started
	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-statusErrors; !errors.Is(err, ErrUpdateStatusServiceClosed) {
		t.Fatalf("pending Status error = %v, want ErrUpdateStatusServiceClosed", err)
	}
	if result, err := service.status(context.Background()); result != nil || !errors.Is(err, ErrUpdateStatusServiceClosed) {
		t.Fatalf("post-close Status = %v, %v, want nil, ErrUpdateStatusServiceClosed", result, err)
	}
}

func TestUpdateStatusServiceReportsInvalidCalculatedStatus(t *testing.T) {
	source := &countingReleaseSource{err: errors.New(" ")}
	updates := newUpdateStatusService("1.1.0", false, source, time.Now)
	t.Cleanup(func() { requireUpdateStatusServiceClosed(t, updates) })
	service := &ServerStatusService{updates: updates}
	for range 2 {
		response, err := service.GetUpdateStatus(context.Background(), &emptypb.Empty{})
		if err != nil {
			t.Fatalf("GetUpdateStatus: %v", err)
		}
		if err := protoapi.Validate(response); err != nil {
			t.Fatalf("invalid failure response: %v", err)
		}
		if response.Status.GetCheckFailed() == nil {
			t.Fatalf("status = %v, want check_failed", response.Status)
		}
	}
	if calls := source.calls.Load(); calls != 1 {
		t.Fatalf("release calls = %d, want 1", calls)
	}
}

func TestUpdateStatusServiceClassifiesReleaseSourceFailures(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		unavailable bool
	}{
		{name: "transport", err: &releaseTransportError{Cause: errors.New("offline")}, unavailable: true},
		{name: "timeout", err: context.DeadlineExceeded, unavailable: true},
		{name: "http", err: &releaseHTTPStatusError{StatusCode: 503, Status: "503 Service Unavailable"}},
		{name: "metadata", err: &releaseMetadataError{Cause: errors.New("invalid release metadata")}},
		{name: "internal", err: errors.New("unexpected release source failure")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newUpdateStatusService("1.1.0", false, &countingReleaseSource{err: test.err}, time.Now)
			t.Cleanup(func() { requireUpdateStatusServiceClosed(t, service) })
			result, err := service.status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if test.unavailable {
				if result.GetCheckUnavailable() == nil {
					t.Fatalf("result = %v, want check_unavailable", result)
				}
			} else if failure := result.GetCheckFailed(); failure == nil || failure.Cause == "" {
				t.Fatalf("result = %v, want check_failed with cause", result)
			}
		})
	}
}

func TestUpdateStatusServiceReportsDevelopmentAndInvalidVersions(t *testing.T) {
	t.Run("development build", func(t *testing.T) {
		service := newUpdateStatusService("dev", false, &countingReleaseSource{metadata: releaseMetadata{Version: updateVersion{components: [3]uint64{1, 2, 0}}}}, time.Now)
		t.Cleanup(func() { requireUpdateStatusServiceClosed(t, service) })
		result, err := service.status(context.Background())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if versions := result.GetAvailable(); versions == nil || versions.CurrentVersion != "0.0.0" || versions.LatestVersion != "1.2.0" {
			t.Fatalf("development result = %#v", result)
		}
	})

	t.Run("overflowed configured version", func(t *testing.T) {
		source := &countingReleaseSource{metadata: releaseMetadata{Version: updateVersion{components: [3]uint64{1, 2, 0}}}}
		service := newUpdateStatusService("18446744073709551616.0.0", false, source, time.Now)
		t.Cleanup(func() { requireUpdateStatusServiceClosed(t, service) })
		result, err := service.status(context.Background())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if result.GetCheckFailed() == nil {
			t.Fatalf("result = %v, want check_failed", result)
		}
		if calls := source.calls.Load(); calls != 0 {
			t.Fatalf("release calls = %d, want 0", calls)
		}
	})
}

type countingReleaseSource struct {
	calls    atomic.Int32
	metadata releaseMetadata
	err      error
}

func (s *countingReleaseSource) LatestRelease(context.Context) (releaseMetadata, error) {
	s.calls.Add(1)
	return s.metadata, s.err
}

type sequenceReleaseSource struct {
	calls   atomic.Int32
	mu      sync.Mutex
	results []releaseMetadata
}

func (s *sequenceReleaseSource) LatestRelease(context.Context) (releaseMetadata, error) {
	call := s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.results[int(call)-1], nil
}

type blockingReleaseSource struct{}

func (*blockingReleaseSource) LatestRelease(ctx context.Context) (releaseMetadata, error) {
	<-ctx.Done()
	return releaseMetadata{}, ctx.Err()
}

type controlledReleaseSource struct {
	calls    atomic.Int32
	started  chan struct{}
	release  chan struct{}
	metadata releaseMetadata
	err      error
	once     sync.Once
}

func newControlledReleaseSource(metadata releaseMetadata, err error) *controlledReleaseSource {
	return &controlledReleaseSource{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		metadata: metadata,
		err:      err,
	}
}

func (s *controlledReleaseSource) LatestRelease(ctx context.Context) (releaseMetadata, error) {
	s.calls.Add(1)
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
		return s.metadata, s.err
	case <-ctx.Done():
		return releaseMetadata{}, ctx.Err()
	}
}

func requireUpdateStatusServiceClosed(t *testing.T, service *UpdateStatusService) {
	t.Helper()
	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
