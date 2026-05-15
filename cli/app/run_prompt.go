package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"builder/shared/client"
	"builder/shared/serverapi"
	"github.com/google/uuid"
)

const subagentSessionSuffix = "subagent"

type RunPromptResult struct {
	SessionID   string
	SessionName string
	Result      string
	Duration    time.Duration
	Warnings    []string
}

func runPrompt(ctx context.Context, client client.RunPromptClient, opts Options, initialSessionID, prompt string, timeout time.Duration, progress io.Writer) (RunPromptResult, error) {
	response, err := client.RunPrompt(ctx, serverapi.RunPromptRequest{
		ClientRequestID:   uuid.NewString(),
		SelectedSessionID: strings.TrimSpace(initialSessionID),
		Prompt:            prompt,
		Timeout:           timeout,
		Overrides:         runPromptOverridesFromOptions(opts),
	}, runPromptIOProgressSink{writer: progress})
	result := RunPromptResult{
		SessionID:   response.SessionID,
		SessionName: response.SessionName,
		Result:      response.Result,
		Duration:    response.Duration,
		Warnings:    append([]string(nil), response.Warnings...),
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func runPromptOverridesFromOptions(opts Options) serverapi.RunPromptOverrides {
	return serverapi.RunPromptOverrides{
		AgentRole:           strings.TrimSpace(opts.AgentRole),
		AgentRoleSet:        opts.AgentRoleSet,
		Model:               strings.TrimSpace(opts.Model),
		ProviderOverride:    strings.TrimSpace(opts.ProviderOverride),
		ThinkingLevel:       strings.TrimSpace(opts.ThinkingLevel),
		Theme:               strings.TrimSpace(opts.Theme),
		ModelTimeoutSeconds: opts.ModelTimeoutSeconds,
		Tools:               strings.TrimSpace(opts.Tools),
		OpenAIBaseURL:       strings.TrimSpace(opts.OpenAIBaseURL),
	}
}

type runPromptIOProgressSink struct {
	writer io.Writer
}

func (s runPromptIOProgressSink) PublishRunPromptProgress(progress serverapi.RunPromptProgress) {
	if s.writer == nil {
		return
	}
	message := strings.TrimSpace(progress.Message)
	if message == "" {
		return
	}
	_, _ = fmt.Fprintln(s.writer, message)
}
