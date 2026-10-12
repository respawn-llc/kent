package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"core/cli/app"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/sessionenv"
)

func TestRunCommandReturnsPendingQuestionsInHumanAndJSONModes(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	previousStdout, previousStderr, previousRun := os.Stdout, os.Stderr, runPromptApp
	t.Cleanup(func() {
		os.Stdout, os.Stderr, runPromptApp = previousStdout, previousStderr, previousRun
	})

	accessTarget := clientui.FileAccessTarget{RequestedPath: "/requested/target", ResolvedPath: "/resolved/target"}
	for _, outcome := range []struct {
		name     string
		question serverapi.ObservationQuestion
		visible  string
		id       string
		options  int
	}{
		{
			name: "ordinary",
			question: serverapi.ObservationQuestion{Ask: &clientui.PendingAsk{
				ToolCallID: "ask-pending",
				SessionID:  runtimeids.NewSessionID(),
				StepID:     questionCommandStepID(),
				Question:   "Distinctive pending question",
				Suggestions: []string{
					"continue",
					"stop",
				},
				CreatedAt: time.Unix(1, 0),
			}},
			visible: "Distinctive pending question",
			id:      "ask-pending",
			options: 2,
		},
		{
			name: "approval",
			question: serverapi.ObservationQuestion{Approval: &clientui.PendingApproval{
				ToolCallID: "approval-pending",
				SessionID:  runtimeids.NewSessionID(),
				StepID:     questionCommandStepID(),
				Question:   "Approval request",
				Options: []clientui.ApprovalOption{
					{Decision: clientui.ApprovalDecisionAllowOnce},
					{Decision: clientui.ApprovalDecisionAllowSession},
					{Decision: clientui.ApprovalDecisionDeny},
				},
				AccessTargets: []clientui.FileAccessTarget{accessTarget},
				CreatedAt:     time.Unix(1, 0),
			}},
			visible: accessTarget.RequestedPath,
			id:      "approval-pending",
			options: 3,
		},
	} {
		for _, mode := range []struct {
			name string
			args []string
			json bool
		}{
			{name: "normal", args: []string{"pending prompt"}},
			{name: "quiet", args: []string{"--quiet", "pending prompt"}},
			{name: "json", args: []string{"--output-mode=json", "pending prompt"}, json: true},
		} {
			t.Run(outcome.name+"/"+mode.name, func(t *testing.T) {
				sessionID := runtimeids.NewSessionID().String()
				stdout, err := os.CreateTemp(t.TempDir(), "stdout")
				if err != nil {
					t.Fatal(err)
				}
				stderr, err := os.CreateTemp(t.TempDir(), "stderr")
				if err != nil {
					_ = stdout.Close()
					t.Fatal(err)
				}
				defer stdout.Close()
				defer stderr.Close()
				os.Stdout, os.Stderr = stdout, stderr
				runPromptApp = func(context.Context, app.Options, string, time.Duration, serverapi.RunPromptProgressSink) (app.RunPromptResult, error) {
					return app.RunPromptResult{
						SessionID: sessionID,
						Outcome:   app.RunPromptPendingQuestion{Question: outcome.question},
					}, nil
				}

				if code := runSubcommand(mode.args); code != 0 {
					t.Fatalf("exit = %d, want success", code)
				}
				output, err := os.ReadFile(stdout.Name())
				if err != nil {
					t.Fatal(err)
				}
				diagnostics, err := os.ReadFile(stderr.Name())
				if err != nil {
					t.Fatal(err)
				}
				if len(diagnostics) != 0 {
					t.Fatalf("stderr = %q, want no diagnostics", diagnostics)
				}
				if !mode.json {
					answerCommand := commandString([]string{
						config.Command, "question", "answer", "--session", sessionID, "--option", "<number>",
					})
					if !strings.Contains(string(output), outcome.visible) ||
						!strings.Contains(string(output), answerCommand) ||
						strings.Contains(string(output), buildRunContinueHint(sessionID, "")) {
						t.Fatalf("human output does not present the pending Question and targeted answer: %q", output)
					}
					if outcome.question.Approval != nil && !strings.Contains(string(output), accessTarget.ResolvedPath) {
						t.Fatalf("human Approval output omits the resolved access target: %q", output)
					}
					return
				}

				var envelope map[string]any
				if err := json.Unmarshal(output, &envelope); err != nil {
					t.Fatalf("decode JSON output: %v", err)
				}
				projectedOutcome, ok := envelope["outcome"].(map[string]any)
				if !ok || envelope["status"] != "ok" || envelope["result"] != nil ||
					projectedOutcome["kind"] != "question" || projectedOutcome["question_id"] != outcome.id {
					t.Fatalf("JSON output does not identify a pending Question: %#v", envelope)
				}
				options, ok := projectedOutcome["suggestions"].([]any)
				if !ok || len(options) != outcome.options {
					t.Fatalf("JSON Question options = %#v, want %d options", projectedOutcome["suggestions"], outcome.options)
				}
				for _, option := range options {
					label, ok := option.(string)
					if !ok || label == "" {
						t.Fatalf("JSON Question contains invalid option label %#v", option)
					}
				}
				answerTarget, ok := projectedOutcome["answer_target"].(map[string]any)
				if !ok || answerTarget["session_id"] != sessionID {
					t.Fatalf("JSON Question has no Session answer target: %#v", projectedOutcome)
				}
				if outcome.question.Approval != nil {
					accessTargets, ok := projectedOutcome["access_targets"].([]any)
					if !ok || len(accessTargets) != 1 ||
						accessTargets[0].(map[string]any)["requested_path"] != accessTarget.RequestedPath ||
						accessTargets[0].(map[string]any)["resolved_path"] != accessTarget.ResolvedPath {
						t.Fatalf("JSON Approval does not disclose its access target: %#v", projectedOutcome)
					}
				}
			})
		}
	}
}

func TestRunErrorMessagePreservesCallerContext(t *testing.T) {
	err := fmt.Errorf("caller context remediation: %w", &serverapi.SubagentLaunchDeniedError{
		Kind: serverapi.SubagentLaunchDenialCallerMissing,
	})
	if got := runErrorMessage(err); !strings.HasSuffix(got, err.Error()) {
		t.Fatalf("caller context discarded: %v", got)
	}
	result := newRunJSONError(err)
	if result.Code != "subagent_denied" || result.Message != runErrorMessage(err) {
		t.Fatalf("JSON output lost launch denial: %+v", result)
	}
}

func TestRunCommandEmitsLaunchFailures(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	const sessionID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{
			name: "selected attachment",
			err:  fmt.Errorf("attach session: %w", &sessioncontract.SessionNotFoundError{SessionID: sessionID}),
			code: "runtime",
		},
		{
			name: "inherited caller",
			err: fmt.Errorf("calling session context: %w", errors.Join(
				&serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialCallerMissing},
				&sessioncontract.SessionNotFoundError{SessionID: sessionID},
			)),
			code: "subagent_denied",
		},
		{
			name: "not callable",
			err:  &serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialNotCallable},
			code: "subagent_denied",
		},
	} {
		for _, mode := range []runOutputMode{runOutputModeFinalText, runOutputModeJSON} {
			t.Run(tc.name+"/"+string(mode), func(t *testing.T) {
				stdout, err := os.CreateTemp(t.TempDir(), "stdout")
				if err != nil {
					t.Fatal(err)
				}
				stderr, err := os.CreateTemp(t.TempDir(), "stderr")
				if err != nil {
					_ = stdout.Close()
					t.Fatal(err)
				}
				previousStdout, previousStderr, previousRun := os.Stdout, os.Stderr, runPromptApp
				os.Stdout, os.Stderr = stdout, stderr
				runPromptApp = func(context.Context, app.Options, string, time.Duration, serverapi.RunPromptProgressSink) (app.RunPromptResult, error) {
					return app.RunPromptResult{}, tc.err
				}
				t.Cleanup(func() {
					os.Stdout, os.Stderr, runPromptApp = previousStdout, previousStderr, previousRun
					_ = stdout.Close()
					_ = stderr.Close()
				})
				if code := runSubcommand([]string{"--quiet", "--output-mode", string(mode), "must not dispatch"}); code != 1 {
					t.Fatalf("exit = %d, want failure", code)
				}
				out, err := os.ReadFile(stdout.Name())
				if err != nil {
					t.Fatal(err)
				}
				diagnostics, err := os.ReadFile(stderr.Name())
				if err != nil {
					t.Fatal(err)
				}
				if mode == runOutputModeFinalText {
					if len(out) != 0 || string(diagnostics) != runErrorMessage(tc.err)+"\n" {
						t.Fatalf("human output lost error: stdout=%s, stderr=%s", out, diagnostics)
					}
					return
				}
				var result runJSONResult
				if err := json.Unmarshal(out, &result); err != nil {
					t.Fatal(err)
				}
				if len(diagnostics) != 0 || result.Status != "error" || result.Error == nil ||
					result.Error.Code != tc.code || result.Error.Message != runErrorMessage(tc.err) {
					t.Fatalf("JSON output lost error: %+v, stderr=%s", result, diagnostics)
				}
			})
		}
	}
}
