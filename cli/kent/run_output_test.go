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
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/sessionenv"
)

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
