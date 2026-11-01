//go:build darwin || linux

package shell

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/server/internal/testprocess"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
)

const (
	descendantHelperMarker  = "KENT_SHELL_DESCENDANT_HELPER"
	descendantChildMarker   = "KENT_SHELL_DESCENDANT_CHILD"
	descendantPIDPipeEnv    = "KENT_SHELL_DESCENDANT_PID_PIPE"
	descendantIgnoreTermEnv = "KENT_SHELL_DESCENDANT_IGNORE_TERM"
	completedHelperMarker   = "KENT_SHELL_COMPLETED_HELPER"
	completedGroupMarker    = "KENT_SHELL_COMPLETED_GROUP_HELPER"
)

func TestManagerKillTerminatesDescendantsInIndependentProcessGroups(t *testing.T) {
	manager := newShellTestManager(t, 50*time.Millisecond)
	pidPipe := newDescendantPIDPipe(t)
	t.Setenv(descendantHelperMarker, "1")
	t.Setenv(descendantPIDPipeEnv, pidPipe)

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	result, err := manager.Start(context.Background(), ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{executable, "-test.run=^TestManagedProcessDescendantHelper$"},
		DisplayCommand: "managed descendant test helper",
		Workdir:        t.TempDir(),
		YieldTime:      50 * time.Millisecond,
		KeepStdinOpen:  true,
	})
	if err != nil {
		t.Fatalf("start managed helper: %v", err)
	}
	if !result.MovedToBackground || !result.Running {
		t.Fatalf("managed helper did not move to background: %+v", result)
	}

	descendantPID := waitForDescendantPID(t, pidPipe)
	exited := false
	t.Cleanup(func() {
		if exited {
			return
		}
		_ = syscall.Kill(descendantPID, syscall.SIGKILL)
	})

	if err := manager.Kill(result.SessionID); err != nil {
		t.Fatalf("kill managed helper: %v", err)
	}
	testprocess.WaitForExit(t, descendantPID, 2*time.Second)
	exited = true
}

func TestManagerCloseForceKillsIndependentDescendantsAfterGracePeriod(t *testing.T) {
	manager, err := NewManager(t.TempDir(), WithMinimumExecToBgTime(50*time.Millisecond),
		WithCloseTimeouts(50*time.Millisecond, 500*time.Millisecond))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	pidPipe := newDescendantPIDPipe(t)
	t.Setenv(descendantHelperMarker, "1")
	t.Setenv(descendantPIDPipeEnv, pidPipe)
	t.Setenv(descendantIgnoreTermEnv, "1")

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	result, err := manager.Start(context.Background(), ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{executable, "-test.run=^TestManagedProcessDescendantHelper$"},
		DisplayCommand: "managed descendant test helper",
		Workdir:        t.TempDir(),
		YieldTime:      50 * time.Millisecond,
		KeepStdinOpen:  true,
	})
	if err != nil {
		t.Fatalf("start managed helper: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			snapshot, snapshotErr := manager.Snapshot(result.SessionID)
			t.Logf("managed helper at failure: %+v; snapshot error: %v", snapshot, snapshotErr)
		}
	})
	if !result.MovedToBackground || !result.Running {
		t.Fatalf("managed helper did not move to background: %+v", result)
	}

	descendantPID := waitForDescendantPID(t, pidPipe)
	exited := false
	t.Cleanup(func() {
		if exited {
			return
		}
		_ = syscall.Kill(descendantPID, syscall.SIGKILL)
	})

	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}
	testprocess.WaitForExit(t, descendantPID, 2*time.Second)
	exited = true
}

func TestManagerKillRejectsCompletedRetainedProcess(t *testing.T) {
	manager := newShellTestManager(t, 50*time.Millisecond)
	t.Setenv(completedHelperMarker, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	result, err := manager.Start(context.Background(), ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{executable, "-test.run=^TestManagedProcessDescendantHelper$"},
		DisplayCommand: "completed managed helper",
		Workdir:        t.TempDir(),
		YieldTime:      50 * time.Millisecond,
		KeepStdinOpen:  true,
	})
	if err != nil {
		t.Fatalf("start completed helper: %v", err)
	}
	if !result.MovedToBackground || !result.Running {
		t.Fatalf("completed helper did not move to background: %+v", result)
	}
	completed, err := manager.WriteStdin(context.Background(), WriteRequest{
		SessionID: result.SessionID, Input: "x", YieldTime: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("release completed helper: %v", err)
	}
	if completed.Running {
		t.Fatal("completed helper did not exit after release")
	}
	if err := manager.Kill(result.SessionID); err == nil {
		t.Fatal("kill accepted a completed retained process")
	}
}

func TestManagerCloseTerminatesInheritedProcessGroupAfterRootExit(t *testing.T) {
	manager := newShellTestManager(t, 50*time.Millisecond)
	pidPipe := newDescendantPIDPipe(t)
	t.Setenv(completedGroupMarker, "1")
	t.Setenv(descendantPIDPipeEnv, pidPipe)

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	result, err := manager.Start(context.Background(), ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{executable, "-test.run=^TestManagedProcessDescendantHelper$"},
		DisplayCommand: "completed managed process group helper",
		Workdir:        t.TempDir(),
		YieldTime:      50 * time.Millisecond,
		KeepStdinOpen:  true,
	})
	if err != nil {
		t.Fatalf("start managed helper: %v", err)
	}
	if !result.MovedToBackground || !result.Running {
		t.Fatalf("managed helper did not move to background: %+v", result)
	}

	descendantPID := waitForDescendantPID(t, pidPipe)
	exited := false
	t.Cleanup(func() {
		if exited {
			return
		}
		_ = syscall.Kill(descendantPID, syscall.SIGKILL)
	})

	completed, err := manager.WriteStdin(context.Background(), WriteRequest{
		SessionID: result.SessionID, Input: "x", YieldTime: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("release managed helper root: %v", err)
	}
	if completed.Running {
		t.Fatal("managed helper root did not exit after release")
	}
	if err := syscall.Kill(descendantPID, 0); err != nil {
		t.Fatalf("inherited-group descendant exited before manager close: %v", err)
	}

	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}
	testprocess.WaitForExit(t, descendantPID, 2*time.Second)
	exited = true
}

func TestManagedProcessDescendantHelper(t *testing.T) {
	if os.Getenv(descendantChildMarker) == "1" {
		// A live pipe blocks in the OS without the Go runtime declaring deadlock.
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("create descendant lifetime pipe: %v", err)
		}
		defer reader.Close()
		defer writer.Close()
		if os.Getenv(descendantIgnoreTermEnv) == "1" {
			signal.Ignore(syscall.SIGTERM, os.Interrupt)
		}
		if os.Getenv(completedGroupMarker) == "1" {
			signal.Ignore(syscall.SIGHUP, syscall.SIGTERM, os.Interrupt)
		}
		publishDescendantPID(t, os.Getenv(descendantPIDPipeEnv), os.Getpid())
		_, err = io.Copy(io.Discard, reader)
		if err != nil {
			t.Fatalf("wait for descendant termination: %v", err)
		}
		return
	}
	if os.Getenv(completedHelperMarker) == "1" {
		waitForDescendantRelease(t)
		return
	}
	if os.Getenv(completedGroupMarker) == "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatalf("resolve test executable: %v", err)
		}
		child := exec.CommandContext(context.Background(), executable, "-test.run=^TestManagedProcessDescendantHelper$")
		child.Env = append(os.Environ(), descendantChildMarker+"=1")
		if err := child.Start(); err != nil {
			t.Fatalf("start inherited-group descendant: %v", err)
		}
		waitForDescendantRelease(t)
		return
	}
	if os.Getenv(descendantHelperMarker) != "1" {
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	child := exec.CommandContext(context.Background(), executable, "-test.run=^TestManagedProcessDescendantHelper$")
	child.Env = append(os.Environ(), descendantChildMarker+"=1")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatalf("start descendant: %v", err)
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("wait for descendant: %v", err)
	}
}

func waitForDescendantRelease(t *testing.T) {
	t.Helper()
	var release [1]byte
	if _, err := io.ReadFull(os.Stdin, release[:]); err != nil {
		t.Fatalf("wait for helper release: %v", err)
	}
}

func newDescendantPIDPipe(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/descendant.pid"
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("create descendant PID pipe: %v", err)
	}
	return path
}

func publishDescendantPID(t *testing.T, path string, pid int) {
	t.Helper()
	if path == "" {
		t.Fatal("descendant PID pipe is required")
	}
	writer, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open descendant PID pipe for publication: %v", err)
	}
	defer writer.Close()
	if err := binary.Write(writer, binary.LittleEndian, int32(pid)); err != nil {
		t.Fatalf("publish descendant PID: %v", err)
	}
}

func waitForDescendantPID(t *testing.T, path string) int {
	t.Helper()
	reader, err := os.Open(path)
	if err != nil {
		t.Fatalf("open descendant PID pipe: %v", err)
	}
	defer reader.Close()
	var pid int32
	if err := binary.Read(reader, binary.LittleEndian, &pid); err != nil {
		t.Fatalf("read descendant PID: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("invalid descendant PID: %d", pid)
	}
	return int(pid)
}
