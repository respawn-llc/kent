package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"core/server/tools"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type writeStdinInput struct {
	SessionID       int    `json:"session_id" jsonschema_description:"Identifier of the running exec_command session."`
	Chars           string `json:"chars,omitempty" jsonschema_description:"Bytes to write to stdin. May be empty to poll for output."`
	YieldTimeMS     *int   `json:"yield_time_ms,omitempty" jsonschema_description:"How long to wait in milliseconds for output before yielding."`
	MaxOutputTokens *int   `json:"max_output_tokens,omitempty" jsonschema_description:"Optional maximum amount of output to return back. Full logs are always still available on disk. Prefer defaults unless you want to read large chunks of text."`
}

func WriteStdinStaticContractSource() tools.StaticContractSource {
	return tools.StaticContractSource{ID: toolspec.ToolWriteStdin, Input: writeStdinInput{}}
}

const (
	minimumOutputPollWaitMS = 15_000
	maximumOutputPollWaitMS = 24 * 60 * 60 * 1_000
	shortOutputPollError    = "Avoid polling repeatedly for short intervals, prefer 3-15min polls depending on task. Pick a better interval and retry"
	longOutputPollError     = "This poll is too long. Consider using system cron jobs and `kent run` headless runs for tasks that require such long wait periods"
)

type WriteStdinTool struct {
	outputLimit          int
	oversizedOutputGuard oversizedOutputGuard
	background           *Manager
}

func NewWriteStdinTool(outputLimit int, contextWindowTokens int, background *Manager) *WriteStdinTool {
	if outputLimit <= 0 {
		outputLimit = defaultLimit
	}
	return &WriteStdinTool{
		outputLimit:          outputLimit,
		oversizedOutputGuard: newOversizedOutputGuard(contextWindowTokens),
		background:           background,
	}
}

func (t *WriteStdinTool) Call(ctx context.Context, c tools.Call) (tools.Result, error) {
	if t.background == nil {
		return ErrorResult(c, "write_stdin is not configured"), nil
	}
	var in writeStdinInput
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return ErrorResult(c, fmt.Sprintf("invalid input: %v", err)), nil
	}
	if in.SessionID <= 0 {
		return ErrorResult(c, "session_id is required"), nil
	}
	yieldTime := defaultWriteYieldTime
	if in.YieldTimeMS != nil {
		if in.Chars == "" && *in.YieldTimeMS < minimumOutputPollWaitMS {
			return ErrorResult(c, shortOutputPollError), nil
		}
		if in.Chars == "" && *in.YieldTimeMS > maximumOutputPollWaitMS {
			return ErrorResult(c, longOutputPollError), nil
		}
		yieldTime = writeStdinYieldDuration(*in.YieldTimeMS)
	}
	maxChars := t.outputLimit
	if in.MaxOutputTokens != nil && *in.MaxOutputTokens > 0 {
		maxChars = *in.MaxOutputTokens * 4
	}
	result, err := t.background.WriteStdin(ctx, WriteRequest{
		SessionID:      strconv.Itoa(in.SessionID),
		Input:          in.Chars,
		YieldTime:      yieldTime,
		MaxOutputChars: maxChars,
	})
	if err != nil {
		return ErrorResult(c, formatToolCallError("write_stdin", err)), nil
	}
	if strings.TrimSpace(result.ToolError) != "" {
		return ErrorResult(c, formatToolError(result.Warning, result.ToolError)), nil
	}
	presentation := shellResultPresentationDelta(
		result.RawOutputRequested,
		result.Truncated,
		false,
		result.ExitCode,
	)
	modelVisibleOutput := formatExecResponse(result)
	body, marshalErr := marshalNoHTMLEscape(modelVisibleOutput)
	if marshalErr != nil {
		return tools.Result{}, marshalErr
	}
	var completedBackgroundSessionID *int
	if result.Backgrounded && !result.Running && result.ExitCode != nil {
		completedBackgroundSessionID = textutil.Value(in.SessionID)
	}
	if guarded, ok := t.oversizedOutputGuard.FailedResult(c, in.MaxOutputTokens, modelVisibleOutput, result.OutputPath, presentation); ok {
		guarded.CompletedBackgroundSessionID = completedBackgroundSessionID
		return guarded, nil
	}
	toolResult := tools.Result{
		CallID:                       c.ID,
		Name:                         c.Name,
		Output:                       body,
		PresentationDelta:            presentation,
		CompletedBackgroundSessionID: completedBackgroundSessionID,
	}
	return toolResult, nil
}

func writeStdinYieldDuration(milliseconds int) time.Duration {
	value := int64(milliseconds)
	if value > math.MaxInt64/int64(time.Millisecond) {
		return time.Duration(math.MaxInt64)
	}
	if value < math.MinInt64/int64(time.Millisecond) {
		return time.Duration(math.MinInt64)
	}
	return time.Duration(value) * time.Millisecond
}
