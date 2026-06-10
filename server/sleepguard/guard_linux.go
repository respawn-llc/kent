//go:build linux

package sleepguard

import (
	"io"
	"log"
	"os/exec"
	"sync"
	"syscall"
)

var (
	checkInhibitOnce sync.Once
	inhibitAvailable bool
)

func lookupInhibit() bool {
	checkInhibitOnce.Do(func() {
		_, err := exec.LookPath("systemd-inhibit")
		inhibitAvailable = err == nil
		if !inhibitAvailable {
			log.Println("sleepguard: systemd-inhibit not found; sleep prevention disabled")
		}
	})
	return inhibitAvailable
}

type platformGuardImpl struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

func (p *platformGuardImpl) start() {
	if !lookupInhibit() {
		return
	}
	// --what=sleep only; idle and display sleep are unaffected
	cmd := exec.Command("systemd-inhibit", "--what=sleep", "--mode=block", "--who=builder", "--why=agent running", "sleep", "infinity")
	// New process group so we can kill systemd-inhibit and its sleep child together
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
	// Kill the entire process group to avoid orphaning the sleep child
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	_ = p.cmd.Wait()
	p.cmd = nil
	p.stdin = nil
}
