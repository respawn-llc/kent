//go:build darwin

package sleepguard

import (
	"os/exec"
	"testing"
	"time"

	"core/shared/config"
)

func TestGuardRestartsCaffeinateWhenProcessExitedWhileActive(t *testing.T) {
	if _, err := exec.LookPath("caffeinate"); err != nil {
		t.Skipf("caffeinate unavailable: %v", err)
	}

	var guard Guard
	if err := guard.Acquire(); err != nil {
		t.Fatalf("acquire guard: %v", err)
	}
	t.Cleanup(guard.Release)

	first := currentCaffeinateCommand(t, &guard)
	firstPID := first.Process.Pid
	if err := first.Process.Kill(); err != nil {
		t.Fatalf("kill caffeinate pid %d: %v", firstPID, err)
	}

	waitForRestartedCaffeinate(t, &guard, firstPID)
}

func TestManagerRestartsCaffeinateWhenActiveGuardProcessExited(t *testing.T) {
	if _, err := exec.LookPath("caffeinate"); err != nil {
		t.Skipf("caffeinate unavailable: %v", err)
	}

	manager, err := NewManager(config.SleepPreventionModeActive, nil)
	if err != nil {
		t.Fatalf("create sleep manager: %v", err)
	}
	t.Cleanup(manager.Close)

	manager.RuntimeActiveObserver()(true)
	guard, ok := manager.guard.(*Guard)
	if !ok {
		t.Fatalf("manager guard = %T, want *Guard", manager.guard)
	}
	first := waitForCaffeinateCommand(t, guard)
	firstPID := first.Process.Pid
	if err := first.Process.Kill(); err != nil {
		t.Fatalf("kill caffeinate pid %d: %v", firstPID, err)
	}

	waitForRestartedCaffeinate(t, guard, firstPID)
}

func currentCaffeinateCommand(t *testing.T, guard *Guard) *exec.Cmd {
	t.Helper()
	guard.mu.Lock()
	defer guard.mu.Unlock()
	impl, ok := guard.impl.(*platformGuardImpl)
	if !ok || impl.cmd == nil || impl.cmd.Process == nil {
		t.Fatal("expected active caffeinate process")
	}
	return impl.cmd
}

func waitForRestartedCaffeinate(t *testing.T, guard *Guard, previousPID int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current := currentCaffeinateCommand(t, guard)
		if current.Process.Pid != previousPID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for caffeinate pid %d to be replaced", previousPID)
}

func waitForCaffeinateCommand(t *testing.T, guard *Guard) *exec.Cmd {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cmd := currentCaffeinateCommandOrNil(guard)
		if cmd != nil {
			return cmd
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for active caffeinate process")
	return nil
}

func currentCaffeinateCommandOrNil(guard *Guard) *exec.Cmd {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	impl, ok := guard.impl.(*platformGuardImpl)
	if !ok || impl.cmd == nil || impl.cmd.Process == nil {
		return nil
	}
	return impl.cmd
}
