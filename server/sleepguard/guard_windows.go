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
}

func (p *platformGuardImpl) start() error {
	// Perform an initial synchronous check on this goroutine before launching
	// the pinned goroutine so start() can return a meaningful error.
	if err := setThreadExecutionState(esContinuous | esSystemRequired); err != nil {
		return err
	}
	p.stopCh = make(chan struct{})
	go func() {
		// Pin this goroutine to one OS thread so SetThreadExecutionState's
		// ES_CONTINUOUS state is owned by a stable thread for the inhibit lifetime.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		// Re-set on the pinned thread so ES_CONTINUOUS is owned here.
		_ = setThreadExecutionState(esContinuous | esSystemRequired)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = setThreadExecutionState(esContinuous | esSystemRequired)
			case <-p.stopCh:
				_ = setThreadExecutionState(esContinuous)
				return
			}
		}
	}()
	return nil
}

func (p *platformGuardImpl) stop() {
	if p.stopCh == nil {
		return
	}
	close(p.stopCh)
	p.stopCh = nil
	// ES_CONTINUOUS clear is issued from the pinned goroutine (in start) on stopCh close
}
