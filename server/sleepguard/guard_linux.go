//go:build linux

package sleepguard

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"

	"core/shared/brand"
)

var (
	checkInhibitOnce sync.Once
	inhibitAvailable bool
)

func lookupInhibit() (bool, error) {
	var lookupErr error
	checkInhibitOnce.Do(func() {
		_, err := exec.LookPath("systemd-inhibit")
		inhibitAvailable = err == nil
		if !inhibitAvailable {
			lookupErr = fmt.Errorf("systemd-inhibit not found: %w", err)
		}
	})
	if !inhibitAvailable {
		return false, fmt.Errorf("systemd-inhibit not available; sleep prevention disabled")
	}
	return true, lookupErr
}

type platformGuardImpl struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  <-chan struct{}
}

func newPlatformGuardImpl() platformGuard {
	return &platformGuardImpl{}
}

func (p *platformGuardImpl) start() error {
	if ok, err := lookupInhibit(); !ok {
		return err
	}
	// --what=sleep only; idle and display sleep are unaffected
	cmd := exec.Command("systemd-inhibit", "--what=sleep", "--mode=block", "--who="+brand.Command, "--why=agent running", "sleep", "infinity")
	// New process group so we can kill systemd-inhibit and its sleep child together
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("systemd-inhibit stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("systemd-inhibit start: %w", err)
	}
	done := make(chan struct{})
	p.cmd = cmd
	p.stdin = stdin
	p.done = done
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return nil
}

func (p *platformGuardImpl) stop() {
	if p.cmd == nil {
		return
	}
	_ = p.stdin.Close()
	// Kill the entire process group to avoid orphaning the sleep child
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	if p.done != nil {
		<-p.done
	}
	p.cmd = nil
	p.stdin = nil
	p.done = nil
}

func (p *platformGuardImpl) running() bool {
	if p.cmd == nil || p.done == nil {
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
