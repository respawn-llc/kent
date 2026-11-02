package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/tools"
	"core/shared/clientui"
	"core/shared/toolspec"

	"github.com/google/uuid"
)

func shellTestExecutionContext(t *testing.T, ctx context.Context, callID string) context.Context {
	t.Helper()
	shortID := testsetup.ShellLogSessionShortID(t)
	return tools.WithExecutionIdentity(ctx, tools.ExecutionIdentity{
		StepID:         uuid.NewString(),
		ToolCallID:     clientui.ToolCallID(callID),
		SessionShortID: &shortID,
	})
}

func TestExecCommandRejectsMissingSessionShortIDWithoutExecuting(t *testing.T) {
	manager := newBackgroundTestManager(t)
	workspace := t.TempDir()
	tool := NewExecCommandToolWithConfig(workspace, 16_000, 200_000, manager, "", ExecCommandToolConfig{})
	input, err := json.Marshal(map[string]any{
		"cmd": "touch executed", "shell": "/bin/sh", "login": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionIdentity(context.Background(), tools.ExecutionIdentity{
		StepID: uuid.NewString(), ToolCallID: "missing-session",
	})
	result, err := tool.Call(ctx, tools.Call{
		ID: "missing-session", Name: toolspec.ToolExecCommand, Input: input,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("missing Session short ID accepted: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "executed")); !os.IsNotExist(err) {
		t.Fatalf("command side effect = %v, want no execution", err)
	}
}
