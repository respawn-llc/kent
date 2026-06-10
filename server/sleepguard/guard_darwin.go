//go:build darwin

package sleepguard

import (
	"io"
	"os/exec"
)

type platformGuardImpl struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

func (p *platformGuardImpl) start() {
	// -i: prevent idle sleep only; display sleep is unaffected
	cmd := exec.Command("caffeinate", "-i")
	// stdin pipe ensures child dies with parent even on SIGKILL
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return
	}
	p.cmd = cmd
	p.stdin = stdin
}

func (p *platformGuardImpl) stop() {
	if p.cmd == nil {
		return
	}
	_ = p.stdin.Close()
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
	p.cmd = nil
	p.stdin = nil
}
