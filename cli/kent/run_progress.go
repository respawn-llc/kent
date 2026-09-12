package main

import (
	"fmt"
	"io"
	"strings"

	"core/prompts"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
)

type runProgressRenderer struct {
	stdout               io.Writer
	stderr               io.Writer
	wroteStdoutBlock     bool
	finalResponseEmitted bool
}

func newRunProgressRenderer(stdout io.Writer, stderr io.Writer) *runProgressRenderer {
	return &runProgressRenderer{stdout: stdout, stderr: stderr}
}

func (r *runProgressRenderer) PublishRunPromptProgress(progress *runpromptpb.ProgressEvent) {
	if r == nil {
		return
	}
	switch event := progress.GetPayload().(type) {
	case *runpromptpb.ProgressEvent_SessionStarted:
		_, _ = fmt.Fprintf(
			r.stderr,
			"Started a new session, `%s run steer %s \"prompt\"` to send messages while it runs\n",
			prompts.LaunchCommand(),
			event.SessionStarted.SessionId,
		)
	case *runpromptpb.ProgressEvent_AssistantMessage:
		r.writeStdoutBlock(event.AssistantMessage.Content)
		if event.AssistantMessage.Phase == runpromptpb.MessagePhase_MESSAGE_PHASE_FINAL {
			r.finalResponseEmitted = true
		}
	case *runpromptpb.ProgressEvent_SteeredMessage:
		_, _ = fmt.Fprintf(r.stderr, "Steered message: %s\n", event.SteeredMessage.Content)
	case *runpromptpb.ProgressEvent_CompactionStarted:
		_, _ = fmt.Fprintln(r.stderr, "Compacting context")
	case *runpromptpb.ProgressEvent_CompactionFailed:
		r.writeFailure("Context compaction failed", event.CompactionFailed)
	case *runpromptpb.ProgressEvent_RunLoggingFailed:
		r.writeFailure("Run logging degraded", event.RunLoggingFailed)
	case *runpromptpb.ProgressEvent_RunCleanupFailed:
		r.writeFailure("Run cleanup failed", event.RunCleanupFailed)
	}
}

func (r *runProgressRenderer) writeFailure(summary string, failure *runpromptpb.ProgressFailure) {
	if failure == nil || failure.Error == nil {
		_, _ = fmt.Fprintln(r.stderr, summary)
		return
	}
	_, _ = fmt.Fprintf(r.stderr, "%s: %s\n", summary, strings.TrimSpace(*failure.Error))
}

func (r *runProgressRenderer) Complete(result string, warnings []string, continueHint string) {
	if r == nil {
		return
	}
	emitWarnings(r.stderr, warnings)
	if !r.finalResponseEmitted {
		r.writeStdoutBlock(result)
	}
	r.writeStdoutBlock(continueHint)
}

func (r *runProgressRenderer) writeStdoutBlock(content string) {
	if r == nil || r.stdout == nil || strings.TrimSpace(content) == "" {
		return
	}
	if r.wroteStdoutBlock {
		_, _ = fmt.Fprintln(r.stdout)
	}
	_, _ = fmt.Fprintln(r.stdout, strings.TrimRight(content, "\r\n"))
	r.wroteStdoutBlock = true
}
