package imagefileio

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ExitIfWorker handles Kent's private image-reader subprocess command.
func ExitIfWorker(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) {
	handled, exitCode := runWorkerCommand(args, stdin, stdout, stderr)
	if handled {
		os.Exit(exitCode)
	}
}

func runWorkerCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (bool, int) {
	if len(args) != 1 || args[0] != workerCommand {
		return false, 0
	}
	if err := runWorker(stdin, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		var acquisitionError *AcquisitionError
		if errors.As(err, &acquisitionError) {
			return true, acquisitionFailureExitCode
		}
		return true, 1
	}
	return true, 0
}

func runWorker(stdin io.Reader, stdout io.Writer) error {
	var request workerRequest
	decoder := json.NewDecoder(io.LimitReader(stdin, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("decode image file read request: %w", err)
	}
	if strings.TrimSpace(request.Path) == "" {
		return errors.New("image file read path is required")
	}
	if request.Limit < 0 || request.Limit > MaxReadBytes {
		return fmt.Errorf("invalid image file read limit %d", request.Limit)
	}

	file, _, err := OpenRegularFile(request.Path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	data, err := readLimited(file, request.Limit)
	if err != nil {
		return &AcquisitionError{Cause: err}
	}
	if _, err := stdout.Write(data); err != nil {
		return fmt.Errorf("write image file data: %w", err)
	}
	return nil
}

// OpenRegularFile opens a regular file without following a final symlink.
// The caller owns the returned file and must close it.
func OpenRegularFile(path string) (*os.File, os.FileInfo, error) {
	file, info, err := openReadOnlyRegularFile(path)
	if err != nil {
		return nil, nil, &AcquisitionError{Cause: err}
	}
	return file, info, nil
}

func openReadOnlyRegularFile(path string) (*os.File, os.FileInfo, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("stat path at %q: %v", path, err)
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("path %q is not a regular file", path)
	}
	file, err := openReadOnlyNoFollow(path)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to locate file at %q: %v", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return nil, nil, fmt.Errorf("stat file at %q: %v; close file: %w", path, err, closeErr)
		}
		return nil, nil, fmt.Errorf("stat file at %q: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		if closeErr := file.Close(); closeErr != nil {
			return nil, nil, fmt.Errorf("path %q is not a regular file; close file: %w", path, closeErr)
		}
		return nil, nil, fmt.Errorf("path %q is not a regular file", path)
	}
	if !os.SameFile(pathInfo, info) {
		if closeErr := file.Close(); closeErr != nil {
			return nil, nil, fmt.Errorf("path %q changed while opening; retry the tool call; close file: %w", path, closeErr)
		}
		return nil, nil, fmt.Errorf("path %q changed while opening; retry the tool call", path)
	}
	if err := setReadBlocking(file); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return nil, nil, fmt.Errorf("set file blocking mode at %q: %v; close file: %w", path, err, closeErr)
		}
		return nil, nil, fmt.Errorf("set file blocking mode at %q: %v", path, err)
	}
	return file, info, nil
}

func readLimited(file *os.File, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds max readable size of %d bytes (10 MiB)", limit)
	}
	return data, nil
}
