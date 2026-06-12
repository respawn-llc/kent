//go:build windows

package sleepguard

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
)

const (
	esContinuous     = uint32(0x80000000)
	esSystemRequired = uint32(0x00000001)
)

var (
	modKernel32            = syscall.NewLazyDLL("kernel32.dll")
	procSetThreadExecState = modKernel32.NewProc("SetThreadExecutionState")
)

// setThreadExecutionState returns an error if SetThreadExecutionState returns 0 (failure).
func setThreadExecutionState(flags uint32) error {
	r1, _, lastErr := procSetThreadExecState.Call(uintptr(flags))
	if r1 == 0 {
		if lastErr != syscall.Errno(0) {
			return fmt.Errorf("SetThreadExecutionState(%#x): %w", flags, lastErr)
		}
		return fmt.Errorf("SetThreadExecutionState(%#x) failed", flags)
	}
	return nil
}

type platformGuardImpl struct {
	stopCh chan struct{}
	done   <-chan struct{}
}

func newPlatformGuardImpl() platformGuard {
	return &platformGuardImpl{}
}

func (p *platformGuardImpl) start() error {
	stopCh := make(chan struct{})
	done := make(chan struct{})
	started := make(chan error, 1)
	go func() {
		defer close(done)
		// Pin this goroutine to one OS thread so SetThreadExecutionState's
		// ES_CONTINUOUS state is owned by a stable thread for the inhibit lifetime.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := setThreadExecutionState(esContinuous | esSystemRequired); err != nil {
			started <- err
			return
		}
		started <- nil
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := setThreadExecutionState(esContinuous | esSystemRequired); err != nil {
					return
				}
			case <-stopCh:
				_ = setThreadExecutionState(esContinuous)
				return
			}
		}
	}()
	if err := <-started; err != nil {
		<-done
		return err
	}
	p.stopCh = stopCh
	p.done = done
	return nil
}

func (p *platformGuardImpl) stop() {
	if p.stopCh == nil {
		return
	}
	close(p.stopCh)
	if p.done != nil {
		<-p.done
	}
	p.stopCh = nil
	p.done = nil
	// ES_CONTINUOUS clear is issued from the pinned goroutine (in start) on stopCh close
}

func (p *platformGuardImpl) running() bool {
	if p.stopCh == nil || p.done == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *platformGuardImpl) exited() <-chan struct{} {
	return p.done
}
