package daemonlaunch

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	goruntime "runtime"
	"sync"
	"time"
)

const (
	defaultConnectTimeout  = 10 * time.Second
	defaultPollInterval    = 50 * time.Millisecond
	defaultShutdownTimeout = 5 * time.Second
)

type Controls struct {
	Terminate       func(*os.Process) error
	Kill            func(*os.Process) error
	ShutdownTimeout time.Duration
}

type Request[T any] struct {
	ExecutablePath string
	Args           []string
	Env            []string
	ConnectTimeout time.Duration
	PollInterval   time.Duration
	Dial           func(context.Context, int) (T, bool, error)
	CloseTarget    func(T) error
	Controls       Controls
}

func Launch[T any](ctx context.Context, req Request[T]) (T, func() error, bool, error) {
	var zero T
	if req.ExecutablePath == "" {
		return zero, nil, false, errors.New("daemon executable path is required")
	}
	if req.Dial == nil {
		return zero, nil, false, errors.New("daemon dial callback is required")
	}
	timeout := req.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}
	pollInterval := req.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	cmd := exec.CommandContext(context.Background(), req.ExecutablePath, req.Args...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Env = req.Env
	if err := cmd.Start(); err != nil {
		return zero, nil, false, err
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Wait()
	}()

	failureClose := NewOwnedProcessClose(zero, nil, cmd, errCh, req.Controls)
	childPID := cmd.Process.Pid
	deadline := time.Now().Add(timeout)
	for {
		target, ok, err := req.Dial(ctx, childPID)
		if err != nil {
			_ = failureClose()
			return zero, nil, false, err
		}
		if ok {
			return target, NewOwnedProcessClose(target, req.CloseTarget, cmd, errCh, req.Controls), true, nil
		}
		select {
		case <-ctx.Done():
			_ = failureClose()
			return zero, nil, false, ctx.Err()
		case err := <-errCh:
			return zero, nil, false, err
		default:
		}
		if time.Now().After(deadline) {
			_ = failureClose()
			return zero, nil, false, context.DeadlineExceeded
		}
		time.Sleep(pollInterval)
	}
}

func NewOwnedProcessClose[T any](target T, closeTarget func(T) error, cmd *exec.Cmd, errCh <-chan error, controls Controls) func() error {
	var once sync.Once
	return func() error {
		var closeErr error
		once.Do(func() {
			if closeTarget != nil {
				closeErr = errors.Join(closeErr, closeTarget(target))
			}
			if cmd == nil || cmd.Process == nil || errCh == nil {
				return
			}
			controls := normalizeControls(controls)
			select {
			case <-errCh:
				return
			default:
			}
			if err := controls.Terminate(cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
				if killErr := controls.Kill(cmd.Process); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
					closeErr = errors.Join(closeErr, killErr)
				}
				<-errCh
				return
			}
			timer := time.NewTimer(controls.ShutdownTimeout)
			defer timer.Stop()
			select {
			case <-errCh:
			case <-timer.C:
				if err := controls.Kill(cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
					closeErr = errors.Join(closeErr, err)
				}
				<-errCh
			}
		})
		return closeErr
	}
}

func normalizeControls(controls Controls) Controls {
	if controls.Terminate == nil {
		controls.Terminate = terminateProcess
	}
	if controls.Kill == nil {
		controls.Kill = killProcess
	}
	if controls.ShutdownTimeout <= 0 {
		controls.ShutdownTimeout = defaultShutdownTimeout
	}
	return controls
}

func terminateProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	if goruntime.GOOS == "windows" {
		return process.Kill()
	}
	return process.Signal(os.Interrupt)
}

func killProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Kill()
}
