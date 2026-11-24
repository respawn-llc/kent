package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testharness/executionfixture"
	"core/internal/testharness/postprocessfixture"
	"core/internal/testharness/testsetup"
	"core/server/tools"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	"core/shared/toolspec"

	"github.com/google/uuid"
)

func shellTestExecutionContext(t *testing.T, ctx context.Context, callID string) context.Context {
	t.Helper()
	return executionfixture.ExecutionContext(t, ctx, callID)
}

func TestExecCommandAllowsSessionWithoutShortID(t *testing.T) {
	ownerID := testsetup.ShellLogSessionID(t)
	manager := newBackgroundTestManager(t)
	workspace := t.TempDir()
	tool := NewExecCommandToolWithConfig(workspace, 16_000, 200_000, manager, ownerID.String(), ExecCommandToolConfig{
		Postprocessor: postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeNone}),
	})
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
	if result.IsError {
		t.Fatalf("legacy Session shell rejected: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "executed")); err != nil {
		t.Fatalf("command did not execute: %v", err)
	}
}
