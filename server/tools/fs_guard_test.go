package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultUserDeniedIncludesRejectionInstruction(t *testing.T) {
	workspace := t.TempDir()
	real, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		t.Fatalf("stat workspace: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	guard := NewFSGuard(
		workspace,
		real,
		info,
		true,
		false,
		func(context.Context, FSGuardRequest) (FSGuardApproval, error) {
			return FSGuardApproval{Decision: FSGuardDecisionDeny, Commentary: "no"}, nil
		},
		nil,
		nil,
		"ask user for a safe path",
		FSGuardErrorLabels{OutsidePath: "outside"},
		FSGuardFailureFactory{},
		nil,
		nil,
	)

	_, err = guard.Allow(context.Background(), outside, outside, nil)
	if err == nil {
		t.Fatal("expected denial error")
	}
	if got := err.Error(); !strings.Contains(got, "no") || !strings.Contains(got, "ask user for a safe path") {
		t.Fatalf("denial error = %q, want commentary and instruction", got)
	}
}
