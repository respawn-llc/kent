//go:build darwin

package sleepguard

import (
	"fmt"
	"io"
	"os/exec"
)

type platformGuardImpl struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  <-chan struct{}
}

func newPlatformGuardImpl() platformGuard {
	return &platformGuardImpl{}
}

func (p *platformGuardImpl) start() error {
	// -i: prevent idle sleep only; display sleep is unaffected
	cmd := exec.Command("caffeinate", "-i")
	// stdin pipe ensures child dies with parent even on SIGKILL
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("caffeinate stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("caffeinate start: %w", err)
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
	_ = p.cmd.Process.Kill()
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
