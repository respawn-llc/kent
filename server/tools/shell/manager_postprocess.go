package shell

import (
	"context"
	"fmt"
	"io"
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
		ExitCode:        postprocess.CloneIntPtr(exitCode),
		Raw:             entry.raw,
		Output:          output,
		MaxDisplayChars: maxOutputChars,
		Backgrounded:    backgrounded,
	})
}

func readOutputFileLimited(path string, maxBytes int64) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("output log path is empty")
	}
	file, err := os.Open(trimmed)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	var reader io.Reader = file
	if maxBytes > 0 {
		reader = io.LimitReader(file, maxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return "", fmt.Errorf("output log exceeds full-read limit %d", maxBytes)
	}
	return string(data), nil
}
