package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

func taskWaitSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return taskObservationSubcommand(args, stdout, stderr, taskpb.ObservationMode_OBSERVATION_MODE_WAIT)
}

func taskWatchSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return taskObservationSubcommand(args, stdout, stderr, taskpb.ObservationMode_OBSERVATION_MODE_WATCH)
}

func taskObservationSubcommand(args []string, stdout io.Writer, stderr io.Writer, mode taskpb.ObservationMode) int {
	var diagnostics bytes.Buffer
	modeName, err := protoapi.TaskObservationMode.Decode(mode)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fs := newCommandFlagSet(config.Command+" task "+modeName, &diagnostics, leafCommandUsage(
		config.Command+" task "+modeName+" <task>",
		"Wait for a Workflow Task outcome.",
	))
	project := fs.String("project", ".", "project path or ID")
	jsonOut := fs.Bool("json", false, "write a stable JSON envelope")
	positionals, ok, code := parseInterspersedPositionals(fs, args)
	if !ok {
		if code == 0 {
			_, _ = io.Copy(stderr, &diagnostics)
			return 0
		}
		if *jsonOut {
			return writeObservationUsage(stdout, strings.TrimSpace(diagnostics.String()))
		}
		_, _ = io.Copy(stderr, &diagnostics)
		return code
	}
	if len(positionals) != 1 {
		if *jsonOut {
			return writeObservationUsage(stdout, "task reference is required")
		}
		fmt.Fprintln(stderr, "task reference is required")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *jsonOut {
		return taskObservationJSON(ctx, stdout, mode, *project, positionals[0])
	}
	return runWorkflowCommandSession(stderr, func(cfg config.Connection, remote *client.Remote) int {
		detail, err := resolveWorkflowTask(ctx, cfg, remote, remote, *project, positionals[0])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		response, err := remote.ObserveWorkflowTask(ctx, &taskpb.ObserveRequest{
			TaskId: detail.Summary.Id, ProjectId: detail.Summary.ProjectId, Mode: mode,
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			if errors.Is(err, context.Canceled) {
				return 130
			}
			return 1
		}
		return writeTaskObservation(stdout, stderr, response, *project)
	})
}

func taskObservationJSON(ctx context.Context, stdout io.Writer, mode taskpb.ObservationMode, projectRef string, ref string) int {
	cfg, remote, err := openBindingCommandRemoteLifecycle(ctx, ".")
	var closeFn func() error
	if remote != nil {
		closeFn = remote.Close
	}
	operation := observationOperationTaskWait
	if mode == taskpb.ObservationMode_OBSERVATION_MODE_WATCH {
		operation = observationOperationTaskWatch
	}
	if err != nil {
		return emitObservationError(stdout, operation, nil, ctx, err, nil, closeFn)
	}
	detail, err := resolveWorkflowTask(ctx, cfg, remote, remote, projectRef, ref)
	if err != nil {
		return emitObservationError(stdout, operation, nil, ctx, err, nil, closeFn)
	}
	target := observationTargetTask(detail.Summary.Id)
	response, err := remote.ObserveWorkflowTask(ctx, &taskpb.ObserveRequest{
		TaskId: detail.Summary.Id, ProjectId: detail.Summary.ProjectId, Mode: mode,
	})
	if err != nil {
		return emitObservationError(stdout, operation, target, ctx, err, nil, closeFn)
	}
	if response.TaskId != detail.Summary.Id {
		err := &client.InvalidResponseError{
			Operation: "workflow task observation",
			Cause:     fmt.Errorf("response task ID %q does not match requested task %q", response.TaskId, detail.Summary.Id),
		}
		envelope, exitCode := projectObservationError(operation, target, ctx, err)
		return emitObservationJSONWithCleanup(stdout, envelope, exitCode, nil, closeFn)
	}
	envelope, exitCode, err := projectTaskObservationJSON(detail.Summary.Id, response)
	if err != nil {
		err = &client.InvalidResponseError{Operation: "workflow task observation", Cause: err}
		envelope, exitCode = projectObservationError(operation, target, ctx, err)
	}
	return emitObservationJSONWithCleanup(stdout, envelope, exitCode, nil, closeFn)
}

func writeTaskObservation(stdout io.Writer, stderr io.Writer, response *taskpb.ObserveSuccess, projectRef string) int {
	if stderr == nil {
		stderr = io.Discard
	}
	exitCode := 0
	questionCount := 0
	for _, outcome := range response.Outcomes {
		if outcome.GetQuestion() != nil {
			questionCount++
		}
	}
	for index, outcome := range response.Outcomes {
		if index > 0 {
			fmt.Fprintln(stdout)
		}
		switch selected := outcome.Outcome.(type) {
		case *taskpb.ObserveOutcome_Done:
			fmt.Fprintf(stdout, "Task %s entered Done status\n", response.TaskShortId)
		case *taskpb.ObserveOutcome_Question:
			detail := selected.Question
			question, err := protoapi.ObservationQuestionFromProto(detail.Question)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			hintArgs := []string{"--task", response.TaskShortId}
			if questionCount > 1 && detail.SessionId != nil {
				hintArgs = []string{"--session", *detail.SessionId}
			}
			if strings.TrimSpace(projectRef) != "" && projectRef != "." && questionCount == 1 {
				hintArgs = append(hintArgs, "--project", projectRef)
			}
			writeTaskOutcomeDiscriminator(stdout, detail.SessionId, detail.ScriptPath, detail.NodeKey)
			writeObservedQuestion(stdout, question, observationQuestionHint(hintArgs, question))
		case *taskpb.ObserveOutcome_ExecutionError:
			writeTaskObservationFailure(stdout, selected.ExecutionError)
			exitCode = 1
		case *taskpb.ObserveOutcome_Interrupted:
			writeTaskObservationFailure(stdout, selected.Interrupted)
			if exitCode == 0 {
				exitCode = 130
			}
		default:
			fmt.Fprintln(stderr, "invalid task observation response: missing outcome")
			return 1
		}
	}
	return exitCode
}

func writeTaskObservationFailure(stdout io.Writer, detail *taskpb.ObserveFailure) {
	writeTaskOutcomeDiscriminator(stdout, detail.SessionId, detail.ScriptPath, detail.NodeKey)
	fmt.Fprintln(stdout, detail.Failure.Reason)
	if detail.Failure.Diagnostic != nil {
		fmt.Fprintln(stdout, *detail.Failure.Diagnostic)
	}
}

func writeTaskOutcomeDiscriminator(stdout io.Writer, sessionID, scriptPath, nodeKey *string) {
	if sessionID != nil {
		fmt.Fprintf(stdout, "Session %s", *sessionID)
	} else if scriptPath != nil {
		fmt.Fprintf(stdout, "Script %s", *scriptPath)
	} else {
		return
	}
	if nodeKey != nil {
		fmt.Fprintf(stdout, " (Node %s)", *nodeKey)
	}
	fmt.Fprintln(stdout, ":")
}
