package serverstatus

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"core/shared/protoapi"
	serverpb "core/shared/protoapi/gen/kent/api/server"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	updateStatusFreshness = time.Hour
	updateStatusTimeout   = 2 * time.Second
)

var ErrUpdateStatusServiceClosed = errors.New("update status service is closed")

type UpdateStatusService struct {
	currentVersion string
	debug          bool
	releaseSource  releaseMetadataSource
	now            func() time.Time
	lifecycle      context.Context
	cancel         context.CancelFunc

	mu        sync.Mutex
	workers   sync.WaitGroup
	completed *completedUpdateStatus
	inflight  *updateStatusOperation
	closed    bool
}

type completedUpdateStatus struct {
	result      *serverpb.UpdateStatus
	completedAt time.Time
}

type updateStatusOperation struct {
	done   chan struct{}
	result *serverpb.UpdateStatus
	err    error
}

func currentUpdateStatusResult(current, latest string) *serverpb.UpdateStatus {
	return &serverpb.UpdateStatus{Status: &serverpb.UpdateStatus_Current{
		Current: &serverpb.UpdateVersions{CurrentVersion: current, LatestVersion: latest},
	}}
}

func availableUpdateStatusResult(current, latest string) *serverpb.UpdateStatus {
	return &serverpb.UpdateStatus{Status: &serverpb.UpdateStatus_Available{
		Available: &serverpb.UpdateVersions{CurrentVersion: current, LatestVersion: latest},
	}}
}

func checkUnavailableUpdateStatusResult() *serverpb.UpdateStatus {
	return &serverpb.UpdateStatus{Status: &serverpb.UpdateStatus_CheckUnavailable{CheckUnavailable: &emptypb.Empty{}}}
}

func failedUpdateStatusResult(cause string) *serverpb.UpdateStatus {
	return &serverpb.UpdateStatus{Status: &serverpb.UpdateStatus_CheckFailed{
		CheckFailed: &serverpb.UpdateCheckFailed{Cause: strings.TrimSpace(cause)},
	}}
}

func NewUpdateStatusService(currentVersion string, debug bool) *UpdateStatusService {
	return newUpdateStatusService(currentVersion, debug, nil, time.Now)
}

func newUpdateStatusService(currentVersion string, debug bool, releaseSource releaseMetadataSource, now func() time.Time) *UpdateStatusService {
	if releaseSource == nil {
		releaseSource = newDefaultGitHubReleaseMetadataSource()
	}
	if now == nil {
		now = time.Now
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	return &UpdateStatusService{
		currentVersion: currentVersion,
		debug:          debug,
		releaseSource:  releaseSource,
		now:            now,
		lifecycle:      lifecycle,
		cancel:         cancel,
	}
}

func (s *UpdateStatusService) status(ctx context.Context) (*serverpb.UpdateStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrUpdateStatusServiceClosed
	}
	if s.completed != nil && isFreshUpdateStatusCache(s.now(), s.completed.completedAt) {
		result := s.completed.result
		s.mu.Unlock()
		return proto.CloneOf(result), nil
	}
	s.completed = nil
	if s.inflight == nil {
		s.inflight = &updateStatusOperation{done: make(chan struct{})}
		s.workers.Add(1)
		go s.runUpdateStatusCheck(s.inflight)
	}
	operation := s.inflight
	s.mu.Unlock()

	select {
	case <-operation.done:
		return proto.CloneOf(operation.result), operation.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *UpdateStatusService) runUpdateStatusCheck(operation *updateStatusOperation) {
	defer s.workers.Done()

	ctx, cancel := context.WithTimeout(s.lifecycle, updateStatusTimeout)
	defer cancel()

	result := s.checkUpdateStatus(ctx)
	if err := protoapi.Validate(result); err != nil {
		result = s.invalidCalculatedResult(result, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.inflight != operation {
		return
	}
	s.completeOperationLocked(operation, result, nil, true)
}

func (s *UpdateStatusService) invalidCalculatedResult(result *serverpb.UpdateStatus, cause error) *serverpb.UpdateStatus {
	diagnostic := fmt.Sprintf(
		"update status invariant violated: operation=publish calculated_status=%T configured_version=%q cause=%v",
		result.GetStatus(),
		strings.TrimSpace(s.currentVersion),
		cause,
	)
	if s.debug {
		panic(diagnostic)
	}
	return failedUpdateStatusResult("internal update checker failure: " + cause.Error())
}

func (s *UpdateStatusService) checkUpdateStatus(ctx context.Context) *serverpb.UpdateStatus {
	currentVersion, err := parseConfiguredUpdateVersion(s.currentVersion)

	if err != nil {
		return failedUpdateStatusResult(fmt.Sprintf("current release version is invalid: %v", err))
	}

	metadata, err := s.releaseSource.LatestRelease(ctx)
	if err != nil {
		return classifyReleaseSourceFailure(err)
	}
	current := currentVersion.String()
	latest := metadata.Version.String()
	if metadata.Version.Compare(currentVersion) > 0 {
		return availableUpdateStatusResult(current, latest)
	}
	return currentUpdateStatusResult(current, latest)
}

func classifyReleaseSourceFailure(err error) *serverpb.UpdateStatus {
	var httpStatusError *releaseHTTPStatusError
	var metadataError *releaseMetadataError
	if errors.As(err, &httpStatusError) || errors.As(err, &metadataError) {
		return failedUpdateStatusResult(err.Error())
	}
	var transportError *releaseTransportError
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.As(err, &transportError) ||
		errors.As(err, &networkError) {
		return checkUnavailableUpdateStatusResult()
	}
	return failedUpdateStatusResult(err.Error())
}

func (s *UpdateStatusService) completeOperationLocked(operation *updateStatusOperation, result *serverpb.UpdateStatus, err error, cache bool) {
	operation.result = result
	operation.err = err
	if cache {
		s.completed = &completedUpdateStatus{result: result, completedAt: s.now()}
	} else {
		s.completed = nil
	}
	s.inflight = nil
	close(operation.done)
}

func (s *UpdateStatusService) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if !s.closed {
		s.closed = true
		s.cancel()
		if s.inflight != nil {
			s.completeOperationLocked(s.inflight, nil, ErrUpdateStatusServiceClosed, false)
		} else {
			s.completed = nil
		}
	}
	s.mu.Unlock()
	s.workers.Wait()
	return nil
}

func isFreshUpdateStatusCache(observedAt time.Time, completedAt time.Time) bool {
	return observedAt.Before(completedAt.Add(updateStatusFreshness))
}

type updateVersion struct {
	components [3]uint64
}

func parseConfiguredUpdateVersion(raw string) (updateVersion, error) {
	if strings.TrimSpace(raw) == "dev" {
		return updateVersion{}, nil
	}
	return parseUpdateVersion(raw)
}

func parseUpdateVersion(raw string) (updateVersion, error) {
	normalized := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	parts := strings.Split(normalized, ".")
	if len(parts) != 3 {
		return updateVersion{}, errors.New("version must contain exactly three numeric components")
	}
	var version updateVersion
	for index, part := range parts {
		if part == "" {
			return updateVersion{}, fmt.Errorf("component %d is empty", index)
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return updateVersion{}, fmt.Errorf("component %d is invalid: %w", index, err)
		}
		version.components[index] = value
	}
	return version, nil
}

func (v updateVersion) Compare(other updateVersion) int {
	for index := range v.components {
		switch {
		case v.components[index] > other.components[index]:
			return 1
		case v.components[index] < other.components[index]:
			return -1
		}
	}
	return 0
}

func (v updateVersion) String() string {
	return strconv.FormatUint(v.components[0], 10) + "." +
		strconv.FormatUint(v.components[1], 10) + "." +
		strconv.FormatUint(v.components[2], 10)
}
