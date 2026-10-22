package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"core/cli/app"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessionenv"
)

func TestRunWatchIsRecognizedAsLiveControl(t *testing.T) {
	if got := liveControlSubcommand([]string{"watch"}); got != "watch" {
		t.Fatalf("liveControlSubcommand(watch) = %q", got)
	}
}

func TestRunWatchMalformedSessionStillUsesWatchRoute(t *testing.T) {
	malformed := "../invalid-session"
	if got := liveControlSubcommand([]string{"watch", malformed}); got != "watch" {
		t.Fatalf("liveControlSubcommand malformed selector = %q, want watch", got)
	}
	if _, err := parseCLILiveSessionID(malformed); err == nil {
		t.Fatal("parseCLILiveSessionID accepted a malformed selector")
	}
	legacy := "legacy-session"
	if _, err := parseCLILiveSessionID(legacy); err == nil {
		t.Fatal("legacy text was accepted as a command selector")
	}
}

func TestLiveControlSubcommandPreservesHeadlessControlVerbPrompts(t *testing.T) {
	for _, args := range [][]string{
		{"wait", "for", "CI", "to", "finish"},
		{"stop", "the", "run", "after", "CI"},
		{"steer", "the", "agent", "toward", "the", "fix"},
	} {
		if got := liveControlSubcommand(args); got != "" {
			t.Fatalf("liveControlSubcommand(%v) = %q, want headless prompt", args, got)
		}
	}
}

func TestRunSteerAndStopRouteNumericSelectorsBeforePromptLaunch(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	previousSteer, previousStop, previousPrompt := runLiveSteerApp, runLiveStopApp, runPromptApp
	defer func() { runLiveSteerApp, runLiveStopApp, runPromptApp = previousSteer, previousStop, previousPrompt }()
	steers, stops, prompts := 0, 0, 0
	runLiveSteerApp = func(context.Context, app.Options, runtimeids.SessionSelector, string) (*runtimepb.LiveSteerSuccess, error) {
		steers++
		return &runtimepb.LiveSteerSuccess{}, nil
	}
	runLiveStopApp = func(context.Context, app.Options, runtimeids.SessionSelector) (*runtimepb.LiveStopSuccess, error) {
		stops++
		return &runtimepb.LiveStopSuccess{}, nil
	}
	runPromptApp = func(context.Context, app.Options, string, time.Duration, serverapi.RunPromptProgressSink) (app.RunPromptResult, error) {
		prompts++
		return app.RunPromptResult{}, nil
	}
	for _, selector := range []string{"123", "00123", runtimeids.NewSessionID().String()} {
		if code := runSubcommand([]string{"steer", selector, "continue"}); code != 0 {
			t.Fatalf("steer %s exit = %d", selector, code)
		}
		if code := runSubcommand([]string{"stop", selector}); code != 0 {
			t.Fatalf("stop %s exit = %d", selector, code)
		}
	}
	if steers != 3 || stops != 3 || prompts != 0 {
		t.Fatalf("control routing: steer=%d stop=%d prompt=%d", steers, stops, prompts)
	}
	for _, selector := range []string{" 1 ", "\t1\n", "0", "0000", "-1", "+1", "9007199254740992", "9223372036854775808"} {
		for _, verb := range []string{"steer", "stop"} {
			args := []string{verb, selector}
			if verb == "steer" {
				args = append(args, "probe")
			}
			if code := runSubcommand(args); code == 0 || steers != 3 || stops != 3 || prompts != 0 {
				t.Fatalf("invalid %s %q: exit=%d steer=%d stop=%d prompt=%d", verb, selector, code, steers, stops, prompts)
			}
		}
	}
	for _, args := range [][]string{{"steer", "the", "agent"}, {"stop", "the", "run"}, {"--", "steer", "123", "continue"}} {
		if code := runSubcommand(args); code != 0 {
			t.Fatalf("prompt %v exit = %d", args, code)
		}
	}
	if steers != 3 || stops != 3 || prompts != 3 {
		t.Fatalf("prompt routing: steer=%d stop=%d prompt=%d", steers, stops, prompts)
	}
	if code := runSubcommand([]string{"--", "steer", " 0 ", "probe"}); code != 0 || prompts != 4 {
		t.Fatalf("explicit numeric prompt escape: exit=%d prompts=%d", code, prompts)
	}
}

func TestRunWatchStreamFailureDoesNotUseInterruptExitCode(t *testing.T) {
	run := func(context.Context, app.Options, runtimeids.SessionSelector) app.RunLiveWatchResult {
		return app.RunLiveWatchResult{Error: fmt.Errorf("%w: %v", serverapi.ErrStreamFailed, context.Canceled)}
	}
	if code := runLiveWatchSubcommandWithCleanup([]string{runtimeids.NewSessionID().String()}, run); code != 1 {
		t.Fatalf("runLiveWatchSubcommand stream failure exit code = %d, want 1", code)
	}
}

func TestRunWatchCallerCancellationKeepsInterruptExitCode(t *testing.T) {
	run := func(context.Context, app.Options, runtimeids.SessionSelector) app.RunLiveWatchResult {
		return app.RunLiveWatchResult{Error: context.Canceled}
	}
	if code := runLiveWatchSubcommandWithCleanup([]string{runtimeids.NewSessionID().String()}, run); code != 130 {
		t.Fatalf("runLiveWatchSubcommand cancellation exit code = %d, want 130", code)
	}
}

func TestTaskObservationRendersDiscriminatorAndTaskTargetForOneQuestion(t *testing.T) {
	sessionID := "11111111-1111-4111-8111-111111111111"
	response := &taskpb.ObserveSuccess{
		TaskId: "task-1", TaskShortId: "T-1",
		Outcomes: []*taskpb.ObserveOutcome{{Outcome: &taskpb.ObserveOutcome_Question{Question: &taskpb.ObserveQuestion{
			SessionId: &sessionID, NodeKey: stringPointerForTest("build"),
			Question: observationQuestionForTest(t, serverapi.ObservationQuestion{Ask: &clientui.PendingAsk{
				ToolCallID: "ask-1", SessionID: taskObservationSessionID(t, sessionID),
				StepID: questionCommandStepID(), Question: "Proceed?", Suggestions: []string{"Yes"},
				CreatedAt: time.UnixMilli(1),
			}}),
		}}}},
	}
	var output bytes.Buffer
	projectRef := "workspace path/$branch"
	if code := writeTaskObservation(&output, io.Discard, response, projectRef); code != 0 {
		t.Fatalf("writeTaskObservation exit code = %d", code)
	}
	text := output.String()
	if !strings.Contains(text, sessionID) ||
		!strings.Contains(text, "build") ||
		!strings.Contains(text, commandString([]string{
			config.Command, "question", "answer",
			"--task", response.TaskShortId,
		})) ||
		!strings.Contains(text, commandString([]string{
			"--project", projectRef,
		})) ||
		strings.Contains(text, "--session "+sessionID) {
		t.Fatalf("task observation output = %q", text)
	}
}

func TestTaskObservationUsesSessionTargetsForParallelQuestions(t *testing.T) {
	firstSession, secondSession := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	firstShort, secondShort := int64(123), int64(124)
	response := &taskpb.ObserveSuccess{
		TaskId: "task-1", TaskShortId: "T-1",
		Outcomes: []*taskpb.ObserveOutcome{
			{Outcome: &taskpb.ObserveOutcome_Question{Question: &taskpb.ObserveQuestion{
				SessionId: &firstSession,
				ShortId:   &firstShort,
				Question: observationQuestionForTest(t, serverapi.ObservationQuestion{Ask: &clientui.PendingAsk{
					ToolCallID: "ask-1", SessionID: taskObservationSessionID(t, firstSession),
					StepID: questionCommandStepID(), Question: "One?", CreatedAt: time.UnixMilli(1),
				}}),
			}}},
			{Outcome: &taskpb.ObserveOutcome_Question{Question: &taskpb.ObserveQuestion{
				SessionId: &secondSession,
				ShortId:   &secondShort,
				Question: observationQuestionForTest(t, serverapi.ObservationQuestion{Ask: &clientui.PendingAsk{
					ToolCallID: "ask-2", SessionID: taskObservationSessionID(t, secondSession),
					StepID: questionCommandStepID(), Question: "Two?", CreatedAt: time.UnixMilli(1),
				}}),
			}}},
		},
	}
	var output bytes.Buffer
	if code := writeTaskObservation(&output, io.Discard, response, "."); code != 0 {
		t.Fatalf("writeTaskObservation exit code = %d", code)
	}
	text := output.String()
	firstHint := commandString([]string{config.Command, "question", "answer", "--session", "123"})
	secondHint := commandString([]string{config.Command, "question", "answer", "--session", "124"})
	if !strings.Contains(text, firstHint) || !strings.Contains(text, secondHint) {
		t.Fatalf("parallel task observation output = %q", text)
	}
}

func observationQuestionForTest(t *testing.T, question serverapi.ObservationQuestion) *promptpb.ObservationQuestion {
	t.Helper()
	value, err := protoapi.ObservationQuestionToProto(question)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func taskObservationSessionID(t *testing.T, raw string) runtimeids.SessionID {
	t.Helper()
	sessionID, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	return sessionID
}

func stringPointerForTest(value string) *string {
	return &value
}

func TestTaskWaitAndWatchReachObservationArgumentValidation(t *testing.T) {
	for _, verb := range []string{"wait", "watch"} {
		var stdout, stderr bytes.Buffer
		if code := taskSubcommand([]string{verb}, &stdout, &stderr); code != 2 {
			t.Fatalf("task %s exit code = %d, want 2", verb, code)
		}
	}
}
