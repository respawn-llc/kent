//go:build windows

package sleepguard

import (
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

func setThreadExecutionState(flags uint32) {
	procSetThreadExecState.Call(uintptr(flags))
}

type platformGuardImpl struct {
	stopCh chan struct{}
}

func (p *platformGuardImpl) start() error {
	p.stopCh = make(chan struct{})
	go func() {
		// Pin this goroutine to one OS thread so SetThreadExecutionState's
		// ES_CONTINUOUS state is owned by a stable thread for the inhibit lifetime.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		setThreadExecutionState(esContinuous | esSystemRequired)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				setThreadExecutionState(esContinuous | esSystemRequired)
			case <-p.stopCh:
				setThreadExecutionState(esContinuous)
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
