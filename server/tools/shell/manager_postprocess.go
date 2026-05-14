package shell

import (
	"context"
	"fmt"
	"os"
	"strings"

	"builder/server/tools/shell/postprocess"
	"builder/shared/toolspec"
)

func (m *Manager) applyPostprocessing(ctx context.Context, entry *processEntry, output string, exitCode *int, backgrounded bool, maxOutputChars int) (postprocess.Result, error) {
	if m == nil || m.postprocessor == nil {
		return postprocess.Result{Output: output}, nil
	}
	return m.postprocessor.Apply(ctx, postprocess.Request{
		ToolName:        toolspec.ToolExecCommand,
		CommandText:     entry.command,
		Workdir:         entry.workdir,
		OwnerSessionID:  entry.ownerSessionID,
		ExitCode:        cloneIntPtr(exitCode),
		Raw:             entry.raw,
		Output:          output,
		MaxDisplayChars: maxOutputChars,
		Backgrounded:    backgrounded,
	})
}

func readOutputFile(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("output log path is empty")
	}
	data, err := os.ReadFile(trimmed)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
