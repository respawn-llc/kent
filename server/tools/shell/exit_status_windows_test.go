//go:build windows

package shell

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

func TestWindowsChildExitStatus(t *testing.T) {
	const childStatus = "KENT_TEST_CHILD_EXIT_STATUS"
	if value, present := os.LookupEnv(childStatus); present {
		status, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		os.Exit(status)
	}
	for _, status := range []int{0, 255, 2147483648, 3221225477, 4294967295} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWindowsChildExitStatus$")
			cmd.Env = append(os.Environ(), childStatus+"="+strconv.Itoa(status))
			exitCode, state := processExitState(cmd.Run())
			if exitCode != status {
				t.Fatalf("exit status = %d, want %d", exitCode, status)
			}
			if (state == "completed") != (status == 0) {
				t.Fatalf("process state = %q for status %d", state, status)
			}
		})
	}
}
