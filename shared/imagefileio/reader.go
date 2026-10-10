// Package imagefileio provides bounded, cancellable local image-file reads.
package imagefileio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const workerCommand = "__kent_internal_read_image_file"
const readTimeout = 10 * time.Second
const workerWaitDelay = time.Second
// Keep source failures distinct from Go's panic exit status.
const acquisitionFailureExitCode = 66
const MaxReadBytes int64 = 10 << 20

var errReadTimeout = errors.New("image file open or read timed out")

// AcquisitionError reports an unusable source, distinct from reader execution failures.
type AcquisitionError struct {
	Cause error
}

func (e *AcquisitionError) Error() string { return e.Cause.Error() }
func (e *AcquisitionError) Unwrap() error { return e.Cause }

type workerRequest struct {
	Path  string `json:"path"`
	Limit int64  `json:"limit"`
}

// Read opens and reads one regular image file in a killable subprocess.
func Read(ctx context.Context, path string, limit int64) ([]byte, error) {
	return readWithTimeout(ctx, path, limit, readTimeout)
}

func readWithTimeout(ctx context.Context, path string, limit int64, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		return nil, errors.New("image file read timeout must be positive")
	}
	if limit < 0 || limit > MaxReadBytes {
		return nil, fmt.Errorf("invalid image file read limit %d", limit)
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate image file reader: %w", err)
	}
	request, err := json.Marshal(workerRequest{Path: path, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("encode image file read request: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(timeoutCtx, executable, workerCommand)
	command.Stdin = bytes.NewReader(request)
	command.WaitDelay = workerWaitDelay
	var stderr bytes.Buffer
	command.Stderr = &stderr
	data, runErr := command.Output()
	if runErr == nil {
		return data, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("image file read canceled: %w", ctxErr)
	}
	if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
		return nil, &AcquisitionError{Cause: fmt.Errorf("%w after %s", errReadTimeout, timeout)}
	}
	failure := fmt.Errorf("image file reader failed: %w", runErr)
	if message := strings.TrimSpace(stderr.String()); message != "" {
		failure = fmt.Errorf("%w: %s", failure, message)
	}
	var exitError *exec.ExitError
	if errors.As(runErr, &exitError) && exitError.ExitCode() == acquisitionFailureExitCode {
		return nil, &AcquisitionError{Cause: failure}
	}
	return nil, failure
}
