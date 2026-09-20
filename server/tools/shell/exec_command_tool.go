package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"core/server/tools"
	"core/server/tools/shell/postprocess"
	"core/shared/runtimeids"
	"core/shared/toolspec"
	"core/shared/transcript"
)

type execCommandInput struct {
	Cmd             string `json:"cmd" jsonschema_description:"Shell command to execute. Treat shell command text as executable code. Single-quote fully literal prose; single quotes suppress variable expansion too. Put intentional variable expansions in separate double-quoted segments. Double quotes still execute backtick substitutions and $(). For prose containing apostrophes or complex/multiline text, prefer a file/stdin option when the command supports it. JSON escaping is not shell escaping."`
	Command         string `json:"command,omitempty" jsonschema:"-"`
	Workdir         string `json:"workdir,omitempty" jsonschema_description:"Optional working directory to run the command in; defaults to the workspace root."`
	Shell           string `json:"shell,omitempty" jsonschema_description:"Shell binary to launch. Defaults to the user's default shell."`
	Login           *bool  `json:"login,omitempty" jsonschema_description:"Whether to run the shell with login semantics. Defaults to true."`
	TTY             bool   `json:"tty,omitempty" jsonschema_description:"Whether to keep stdin open for follow-up write_stdin calls. Defaults to false."`
	Raw             bool   `json:"raw,omitempty" jsonschema_description:"Bypass automatic optimizations that reduce noise. Rerun the command in raw mode if the original output hid important details. Defaults to false."`
	YieldTimeMS     *int   `json:"yield_time_ms,omitempty" jsonschema_description:"How long to wait for command to finish before backgrounding the process. Omit this for most commands."`
	MaxOutputTokens *int   `json:"max_output_tokens,omitempty" jsonschema_description:"Maximum amount of output to return. The full log still remains available on disk. Omit this unless there's a reason to read large chunks of text."`
}

func ExecCommandStaticContractSource() tools.StaticContractSource {
	return tools.StaticContractSource{
		ID:    toolspec.ToolExecCommand,
		Input: execCommandInput{},
	}
}

type ExecCommandTool struct {
	workspaceRoot        string
	defaultShell         string
	defaultLogin         bool
	outputLimit          int
	oversizedOutputGuard oversizedOutputGuard
	background           *Manager
	ownerSessionID       string
	postprocessor        *postprocess.Runner
	executionCorrelation *runtimeids.ExecutionCorrelation
}

type ExecCommandToolConfig struct {
	Postprocessor        *postprocess.Runner
	ExecutionCorrelation *runtimeids.ExecutionCorrelation
}

func NewExecCommandToolWithConfig(workspaceRoot string, outputLimit int, contextWindowTokens int, background *Manager, ownerSessionID string, config ExecCommandToolConfig) *ExecCommandTool {
	defaultShell := strings.TrimSpace(os.Getenv("SHELL"))
	if defaultShell == "" {
		defaultShell = "/bin/sh"
	}
	if outputLimit <= 0 {
		outputLimit = defaultLimit
	}
	return &ExecCommandTool{
		workspaceRoot:        workspaceRoot,
		defaultShell:         defaultShell,
		defaultLogin:         true,
		outputLimit:          outputLimit,
		oversizedOutputGuard: newOversizedOutputGuard(contextWindowTokens),
		background:           background,
		ownerSessionID:       strings.TrimSpace(ownerSessionID),
		postprocessor:        config.Postprocessor,
		executionCorrelation: cloneExecutionCorrelation(config.ExecutionCorrelation),
	}
}

func NewExecCommandToolWithPostprocessor(workspaceRoot string, outputLimit int, contextWindowTokens int, background *Manager, ownerSessionID string, runner *postprocess.Runner) *ExecCommandTool {
	return NewExecCommandToolWithConfig(workspaceRoot, outputLimit, contextWindowTokens, background, ownerSessionID, ExecCommandToolConfig{
		Postprocessor: runner,
	})
}

func (t *ExecCommandTool) Call(ctx context.Context, c tools.Call) (tools.Result, error) {
	if t.background == nil {
		return ErrorResult(c, "exec_command is not configured"), nil
	}
	var in execCommandInput
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return ErrorResult(c, fmt.Sprintf("invalid input: %v", err)), nil
	}
	cmdText := strings.TrimSpace(in.Cmd)
	if cmdText == "" {
		return ErrorResult(c, "cmd is required"), nil
	}
	workdir := ResolveWorkdir(t.workspaceRoot, in.Workdir)
	if workdir != "" {
		normalizedWorkdir, err := filepath.Abs(workdir)
		if err != nil {
			return ErrorResult(c, err.Error()), nil
		}
		workdir = normalizedWorkdir
		info, err := os.Stat(workdir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				return ErrorResult(c, formatMissingWorkingDirectoryError(workdir)), nil
			}
			return ErrorResult(c, err.Error()), nil
		}
		if !info.IsDir() {
			return ErrorResult(c, formatNonDirectoryWorkingDirectoryError(workdir)), nil
		}
	}
	resolvedShell := strings.TrimSpace(in.Shell)
	if resolvedShell == "" {
		resolvedShell = t.defaultShell
	}
	useLogin := t.defaultLogin
	if in.Login != nil {
		useLogin = *in.Login
	}
	argv := []string{resolvedShell}
	if useLogin {
		argv = append(argv, "-lc", cmdText)
	} else {
		argv = append(argv, "-c", cmdText)
	}
	var yieldTime time.Duration
	if in.YieldTimeMS != nil {
		yieldTime = time.Duration(*in.YieldTimeMS) * time.Millisecond
	}
	maxChars := t.outputLimit
	if in.MaxOutputTokens != nil && *in.MaxOutputTokens > 0 {
		maxChars = *in.MaxOutputTokens * 4
	}
	result, err := t.background.Start(ctx, ExecRequest{
		Command:              argv,
		DisplayCommand:       cmdText,
		OwnerSessionID:       t.ownerSessionID,
		OwnerRunID:           strings.TrimSpace(c.RunID),
		OwnerStepID:          strings.TrimSpace(c.StepID),
		ExecutionCorrelation: cloneExecutionCorrelation(t.executionCorrelation),
		Workdir:              workdir,
		YieldTime:            yieldTime,
		MaxOutputChars:       maxChars,
		KeepStdinOpen:        in.TTY,
		Raw:                  in.Raw,
		Postprocessor:        t.postprocessor,
	})
	if err != nil {
		return ErrorResult(c, formatToolCallErrorBase(err)), nil
	}
	if strings.TrimSpace(result.ToolError) != "" {
		return ErrorResult(c, formatToolError(result.Warning, result.ToolError)), nil
	}
	presentation := shellResultPresentationDelta(
		in.Raw,
		result.Truncated,
		result.MovedToBackground,
		result.ExitCode,
	)
	modelVisibleOutput := formatExecResponse(result)
	if guarded, ok := t.oversizedOutputGuard.FailedResult(c, in.MaxOutputTokens, modelVisibleOutput, result.OutputPath, presentation); ok {
		return guarded, nil
	}
	body, marshalErr := marshalNoHTMLEscape(modelVisibleOutput)
	if marshalErr != nil {
		return tools.Result{}, marshalErr
	}
	toolResult := tools.Result{
		CallID:            c.ID,
		Name:              c.Name,
		Output:            body,
		PresentationDelta: presentation,
	}
	return toolResult, nil
}

func shellResultPresentationDelta(rawRequested bool, truncated bool, movedToBackground bool, shellExitCode *int) *transcript.ToolResultPresentationDelta {
	if !rawRequested && !truncated && !movedToBackground && shellExitCode == nil {
		return nil
	}
	delta := &transcript.ToolResultPresentationDelta{
		RawOutputRequested: rawRequested,
		OutputTruncated:    truncated,
		MovedToBackground:  movedToBackground,
	}
	if shellExitCode != nil {
		exitCode := *shellExitCode
		delta.ShellExitCode = &exitCode
	}
	return delta
}
