package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"core/cli/app"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/sessionenv"
)

func TestRunPresentationUsesShortSelectorsAndUUIDJSON(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	id := runtimeids.NewSessionID()
	short, err := runtimeids.NewSessionShortID(123)
	if err != nil {
		t.Fatal(err)
	}
	identity := runtimeids.SessionIdentity{ID: id, ShortID: &short}
	var stdout, stderr bytes.Buffer
	renderer := newRunProgressRenderer(&stdout, &stderr)
	number := short.Int64()
	renderer.PublishRunPromptProgress(&runpromptpb.ProgressEvent{
		Payload: &runpromptpb.ProgressEvent_SessionStarted{SessionStarted: &runpromptpb.SessionStarted{SessionId: id.String(), ShortId: &number}},
	})
	renderer.Complete("done", nil, buildRunContinueHint(identity.String(), ""))
	for _, output := range []string{stdout.String(), stderr.String()} {
		if !strings.Contains(output, short.String()) || strings.Contains(output, id.String()) {
			t.Fatalf("ordinary Session output = %q", output)
		}
	}
	previousRun := runPromptApp
	t.Cleanup(func() { runPromptApp = previousRun })
	runPromptApp = func(context.Context, app.Options, string, time.Duration, serverapi.RunPromptProgressSink) (app.RunPromptResult, error) {
		return app.RunPromptResult{Identity: &identity, Result: "done"}, nil
	}
	code, ordinary, diagnostics := captureRunOutput(t, []string{"--quiet", "prompt"})
	if code != 0 || len(diagnostics) != 0 || !strings.Contains(string(ordinary), short.String()) || strings.Contains(string(ordinary), id.String()) {
		t.Fatalf("Run output = %s, %s, exit %d", ordinary, diagnostics, code)
	}
	code, encoded, diagnostics := captureRunOutput(t, []string{"--quiet", "--output-mode", "json", "prompt"})
	if code != 0 || len(diagnostics) != 0 {
		t.Fatalf("JSON Run output = %s, %s, exit %d", encoded, diagnostics, code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["session_id"] != id.String() || decoded["continue_id"] != id.String() {
		t.Fatalf("JSON identities = %v", decoded)
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
	sessionID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{
			name: "selected attachment",
			err:  fmt.Errorf("attach session: %w", &sessioncontract.SessionNotFoundError{SessionID: &sessionID}),
			code: "runtime",
		},
		{
			name: "inherited caller",
			err: fmt.Errorf("calling session context: %w", errors.Join(
				&serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialCallerMissing},
				&sessioncontract.SessionNotFoundError{SessionID: &sessionID},
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
				previousRun := runPromptApp
				runPromptApp = func(context.Context, app.Options, string, time.Duration, serverapi.RunPromptProgressSink) (app.RunPromptResult, error) {
					return app.RunPromptResult{}, tc.err
				}
				t.Cleanup(func() {
					runPromptApp = previousRun
				})
				code, out, diagnostics := captureRunOutput(t, []string{"--quiet", "--output-mode", string(mode), "must not dispatch"})
				if code != 1 {
					t.Fatalf("exit = %d, want failure", code)
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

func captureRunOutput(t *testing.T, args []string) (int, []byte, []byte) {
	t.Helper()
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	previousStdout, previousStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdout, stderr
	defer func() { os.Stdout, os.Stderr = previousStdout, previousStderr }()
	code := runSubcommand(args)
	out, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, out, diagnostics
}
