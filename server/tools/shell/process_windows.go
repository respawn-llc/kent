//go:build windows

package shell

import (
	"errors"
	"os"
	"os/exec"
)

func prepareManagedExec(cmd *exec.Cmd) {}

func deprioritizeManagedProcess(process *os.Process) error { return nil }

func killManagedProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Kill()
}

func forceKillManagedProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Kill()
}

func processExitState(err error) (int, string) {
	if err == nil {
		return 0, "completed"
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode := exitErr.ExitCode()
		if exitCode == -1 {
			exitCode = 1
		}
		return exitCode, "failed"
	}
	return 130, "killed"
}
