//go:build !windows

package shell

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func prepareManagedExec(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func deprioritizeManagedProcess(process *os.Process) error {
	if process == nil || process.Pid <= 0 {
		return nil
	}
	if err := syscall.Setpriority(syscall.PRIO_PGRP, process.Pid, 10); err != nil {
		return fmt.Errorf("renice process group %d: %w", process.Pid, err)
	}
	return nil
}

func killManagedProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	pid := process.Pid
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("terminate process group %d: %w", pid, err)
	}
	_ = process.Signal(os.Interrupt)
	return nil
}

func forceKillManagedProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	pid := process.Pid
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("kill process group %d: %w", pid, err)
	}
	return nil
}

func processExitState(err error) (int, string) {
	if err == nil {
		return 0, "completed"
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 130, "killed"
	}
	exitCode := exitErr.ExitCode()
	if exitErr.ProcessState != nil {
		if status, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			exitCode = 128 + int(status.Signal())
			if exitCode <= 0 {
				exitCode = 130
			}
			return exitCode, "killed"
		}
	}
	if exitCode == -1 {
		exitCode = 1
	}
	return exitCode, "failed"
}
