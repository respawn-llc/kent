package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"core/cli/app"
	serverstartup "core/server/startup"
	"core/shared/config"
	"core/shared/sessionenv"
)

type stubServeServer struct {
	serveErr error
}

func (s *stubServeServer) Close() error { return nil }
func (s *stubServeServer) Serve(context.Context) error {
	return s.serveErr
}

func TestRootCommandPrintsVersion(t *testing.T) {
	original := config.Version
	config.Version = "1.2.3"
	t.Cleanup(func() {
		config.Version = original
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"--version"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := stdout.String(); got != "1.2.3\n" {
		t.Fatalf("stdout = %q, want version output", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRootHelpShowsInteractiveContinueCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"--help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	got := stderr.String()
	for _, want := range []string{
		"kent --continue <session-id>",
		"reopens a previous session in the interactive TUI",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr = %q, want %q", got, want)
		}
	}
}

func TestRootCommandRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"prompt", "--help"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	_ = stderr
}

func TestRootCommandRejectsNonInteractiveMode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand(nil, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "interactive mode requires a terminal on stdin and stdout") {
		t.Fatalf("stderr = %q, want non-interactive error", got)
	}
}

func TestRootCommandForceInteractiveBypassesTerminalCheck(t *testing.T) {
	original := runInteractiveApp
	t.Cleanup(func() {
		runInteractiveApp = original
	})
	called := false
	runInteractiveApp = func(ctx context.Context, opts app.Options) error {
		called = true
		return nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"--force-interactive"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !called {
		t.Fatal("expected interactive app to run when --force-interactive is set")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRootCommandMapsSessionFlagsToInteractiveApp(t *testing.T) {
	original := runInteractiveApp
	t.Cleanup(func() {
		runInteractiveApp = original
	})
	var got app.Options
	runInteractiveApp = func(ctx context.Context, opts app.Options) error {
		got = opts
		return nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"--force-interactive",
		"--session", "session-123",
	}
	if code := rootCommand(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got.WorkspaceRoot != "." || got.WorkspaceRootExplicit {
		t.Fatalf("unexpected workspace mapping: %+v", got)
	}
	if got.SessionID != "session-123" {
		t.Fatalf("unexpected interactive option mapping: %+v", got)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("unexpected output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRootCommandIgnoresKentSessionEnvByDefault(t *testing.T) {
	original := runInteractiveApp
	t.Cleanup(func() {
		runInteractiveApp = original
	})
	var got app.Options
	runInteractiveApp = func(ctx context.Context, opts app.Options) error {
		got = opts
		return nil
	}
	t.Setenv(sessionenv.SessionIDEnv, "session-from-env")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"--force-interactive"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got.SessionID != "" {
		t.Fatalf("session id = %q, want empty", got.SessionID)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("unexpected output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRootCommandRejectsRemovedStartupConfigFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"--force-interactive", "--model", "gpt-5"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -model") {
		t.Fatalf("stderr = %q, want undefined model flag rejection", stderr.String())
	}
}

func TestRootCommandInteractiveInterruptReturns130(t *testing.T) {
	original := runInteractiveApp
	t.Cleanup(func() {
		runInteractiveApp = original
	})
	runInteractiveApp = func(ctx context.Context, opts app.Options) error {
		return context.Canceled
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"--force-interactive"}, strings.NewReader(""), &stdout, &stderr); code != 130 {
		t.Fatalf("exit code = %d, want 130", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRootCommandServeUsesStandaloneServerPath(t *testing.T) {
	originalStart := startServeServer
	originalHandlers := newServeStartupHandlers
	t.Cleanup(func() {
		startServeServer = originalStart
		newServeStartupHandlers = originalHandlers
	})
	var called bool
	var got serverstartup.Request
	startServeServer = func(_ context.Context, req serverstartup.Request, _ serverstartup.AuthHandler, _ serverstartup.OnboardingHandler) (serveCommandServer, error) {
		called = true
		got = req
		return &stubServeServer{serveErr: context.Canceled}, nil
	}
	newServeStartupHandlers = func() (serverstartup.AuthHandler, serverstartup.OnboardingHandler) {
		return nil, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"serve"}, strings.NewReader(""), &stdout, &stderr); code != 130 {
		t.Fatalf("exit code = %d, want 130", code)
	}
	if !called {
		t.Fatal("expected serve startup path to run")
	}
	if got.WorkspaceRoot != "" || got.WorkspaceRootExplicit {
		t.Fatalf("unexpected workspace mapping: %+v", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got.SessionID != "" {
		t.Fatalf("expected empty session id for serve request, got %q", got.SessionID)
	}
}

func TestServeSubcommandRejectsRemovedStartupConfigFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"serve", "--workspace", "/tmp/work"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -workspace") {
		t.Fatalf("stderr = %q, want undefined workspace flag rejection", stderr.String())
	}
}

func TestServeSubcommandRejectsSessionFlags(t *testing.T) {
	originalHandlers := newServeStartupHandlers
	t.Cleanup(func() {
		newServeStartupHandlers = originalHandlers
	})
	newServeStartupHandlers = func() (serverstartup.AuthHandler, serverstartup.OnboardingHandler) {
		return nil, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := rootCommand([]string{"serve", "--session", "session-123"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -session") {
		t.Fatalf("stderr = %q, want undefined session flag rejection", stderr.String())
	}
}

func TestRunSubcommandMapsCommonFlagsToRunPrompt(t *testing.T) {
	original := runPromptApp
	t.Cleanup(func() {
		runPromptApp = original
	})
	var gotOpts app.Options
	var gotPrompt string
	var gotTimeout time.Duration
	runPromptApp = func(ctx context.Context, opts app.Options, prompt string, timeout time.Duration, progress io.Writer) (app.RunPromptResult, error) {
		gotOpts = opts
		gotPrompt = prompt
		gotTimeout = timeout
		return app.RunPromptResult{Result: "done"}, nil
	}

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout temp file: %v", err)
	}
	stderrFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create stderr temp file: %v", err)
	}
	os.Stdout = stdoutFile
	os.Stderr = stderrFile
	t.Cleanup(func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
	})

	args := []string{
		"run",
		"--workspace", "/tmp/run-workspace",
		"--session", "session-456",
		"--model", "gpt-5-mini",
		"--provider-override", "openai",
		"--thinking-level", "medium",
		"--theme", "light",
		"--model-timeout-seconds", "12",
		"--tools", "shell",
		"--openai-base-url", "http://run.example/v1",
		"--timeout", "2m",
		"hello from test",
	}
	if code := rootCommand(args, strings.NewReader(""), io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if gotPrompt != "hello from test" || gotTimeout != 2*time.Minute {
		t.Fatalf("unexpected run prompt mapping prompt=%q timeout=%v", gotPrompt, gotTimeout)
	}
	if gotOpts.WorkspaceRoot != "/tmp/run-workspace" || !gotOpts.WorkspaceRootExplicit {
		t.Fatalf("unexpected workspace mapping: %+v", gotOpts)
	}
	if gotOpts.SessionID != "session-456" || gotOpts.Model != "gpt-5-mini" || gotOpts.ProviderOverride != "openai" || gotOpts.ThinkingLevel != "medium" || gotOpts.Theme != "light" {
		t.Fatalf("unexpected run option mapping: %+v", gotOpts)
	}
	if gotOpts.ModelTimeoutSeconds != 12 {
		t.Fatalf("unexpected timeout mapping: %+v", gotOpts)
	}
	if gotOpts.Tools != "shell" {
		t.Fatalf("tools = %q, want shell", gotOpts.Tools)
	}
	if gotOpts.OpenAIBaseURL != "http://run.example/v1" || !gotOpts.OpenAIBaseURLExplicit {
		t.Fatalf("unexpected base url mapping: %+v", gotOpts)
	}
}

func TestHelperProcessRootCommand(t *testing.T) {
	if os.Getenv("KENT_ROOT_HELPER_PROCESS") != "1" {
		return
	}
	if os.Getenv("KENT_ROOT_HELPER_STUB_SERVE") == "1" {
		startServeServer = func(_ context.Context, _ serverstartup.Request, _ serverstartup.AuthHandler, _ serverstartup.OnboardingHandler) (serveCommandServer, error) {
			return &stubServeServer{serveErr: context.Canceled}, nil
		}
		newServeStartupHandlers = func() (serverstartup.AuthHandler, serverstartup.OnboardingHandler) {
			return nil, nil
		}
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			os.Exit(rootCommand(args[i+1:], strings.NewReader(""), os.Stdout, os.Stderr))
		}
	}
	os.Exit(2)
}

func TestRunSubcommandMapsFastFlagToAgentRole(t *testing.T) {
	original := runPromptApp
	t.Cleanup(func() {
		runPromptApp = original
	})
	var gotOpts app.Options
	runPromptApp = func(ctx context.Context, opts app.Options, prompt string, timeout time.Duration, progress io.Writer) (app.RunPromptResult, error) {
		gotOpts = opts
		return app.RunPromptResult{Result: "done"}, nil
	}

	originalStdout := os.Stdout
	stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout temp file: %v", err)
	}
	os.Stdout = stdoutFile
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = stdoutFile.Close()
	})

	if code := rootCommand([]string{"run", "--fast", "hello"}, strings.NewReader(""), io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if gotOpts.AgentRole != config.BuiltInSubagentRoleFast {
		t.Fatalf("agent role = %q, want fast", gotOpts.AgentRole)
	}
}

func TestRunSubcommandJSONModeKeepsWarningsInJSONOnly(t *testing.T) {
	original := runPromptApp
	t.Cleanup(func() {
		runPromptApp = original
	})
	runPromptApp = func(ctx context.Context, opts app.Options, prompt string, timeout time.Duration, progress io.Writer) (app.RunPromptResult, error) {
		return app.RunPromptResult{Result: "done", Warnings: []string{"warning one"}}, nil
	}

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout temp file: %v", err)
	}
	stderrFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create stderr temp file: %v", err)
	}
	os.Stdout = stdoutFile
	os.Stderr = stderrFile
	t.Cleanup(func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
	})

	if code := rootCommand([]string{"run", "--output-mode=json", "hello"}, strings.NewReader(""), io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := stdoutFile.Seek(0, 0); err != nil {
		t.Fatalf("seek stdout: %v", err)
	}
	data, err := io.ReadAll(stdoutFile)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if strings.Contains(string(data), "warning one\n\n") {
		t.Fatalf("expected stdout to stay json-only, got %q", string(data))
	}
	var decoded runJSONResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode json output: %v; raw=%q", err, string(data))
	}
	if len(decoded.Warnings) != 1 || decoded.Warnings[0] != "warning one" {
		t.Fatalf("unexpected warnings: %+v", decoded.Warnings)
	}
	if _, err := stderrFile.Seek(0, 0); err != nil {
		t.Fatalf("seek stderr: %v", err)
	}
	stderrData, err := io.ReadAll(stderrFile)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if strings.TrimSpace(string(stderrData)) != "" {
		t.Fatalf("stderr = %q, want empty", string(stderrData))
	}
}

func TestRequireInteractiveTerminalAllowsForce(t *testing.T) {
	if err := requireInteractiveTerminal(strings.NewReader(""), &bytes.Buffer{}, true); err != nil {
		t.Fatalf("require interactive terminal with force: %v", err)
	}
}

func TestParseRunTimeoutDefaultsToInfinite(t *testing.T) {
	got, err := parseRunTimeout("")
	if err != nil {
		t.Fatalf("parse run timeout: %v", err)
	}
	if got != 0 {
		t.Fatalf("timeout = %v, want 0", got)
	}
}

func TestParseRunTimeoutRejectsInvalid(t *testing.T) {
	if _, err := parseRunTimeout("not-a-duration"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRunTimeoutParsesDuration(t *testing.T) {
	got, err := parseRunTimeout("2m")
	if err != nil {
		t.Fatalf("parse run timeout: %v", err)
	}
	if got != 2*time.Minute {
		t.Fatalf("timeout = %v, want %v", got, 2*time.Minute)
	}
}

func TestRunErrorCode(t *testing.T) {
	if got := runErrorCode(context.DeadlineExceeded); got != "timeout" {
		t.Fatalf("run error code = %q, want timeout", got)
	}
	if got := runErrorCode(context.Canceled); got != "interrupted" {
		t.Fatalf("run error code = %q, want interrupted", got)
	}
	if got := runErrorCode(errors.New("boom")); got != "runtime" {
		t.Fatalf("run error code = %q, want runtime", got)
	}
}

func TestParseRunOutputMode(t *testing.T) {
	got, err := parseRunOutputMode("final-text")
	if err != nil {
		t.Fatalf("parse output mode: %v", err)
	}
	if got != runOutputModeFinalText {
		t.Fatalf("output mode = %q, want %q", got, runOutputModeFinalText)
	}
	got, err = parseRunOutputMode("json")
	if err != nil {
		t.Fatalf("parse output mode: %v", err)
	}
	if got != runOutputModeJSON {
		t.Fatalf("output mode = %q, want %q", got, runOutputModeJSON)
	}
	if _, err := parseRunOutputMode("verbose"); err == nil {
		t.Fatal("expected invalid output mode error")
	}
}

func TestParseRunProgressMode(t *testing.T) {
	got, err := parseRunProgressMode("quiet")
	if err != nil {
		t.Fatalf("parse progress mode: %v", err)
	}
	if got != runProgressModeQuiet {
		t.Fatalf("progress mode = %q, want %q", got, runProgressModeQuiet)
	}
	got, err = parseRunProgressMode("stderr")
	if err != nil {
		t.Fatalf("parse progress mode: %v", err)
	}
	if got != runProgressModeStderr {
		t.Fatalf("progress mode = %q, want %q", got, runProgressModeStderr)
	}
	if _, err := parseRunProgressMode("chatty"); err == nil {
		t.Fatal("expected invalid progress mode error")
	}
}

func TestEffectiveSessionIDPrefersContinueAlias(t *testing.T) {
	got, err := effectiveSessionID(commonFlags{SessionID: "abc", ContinueID: "abc"})
	if err != nil {
		t.Fatalf("effective session id: %v", err)
	}
	if got != "abc" {
		t.Fatalf("session id = %q, want abc", got)
	}

	got, err = effectiveSessionID(commonFlags{ContinueID: "xyz"})
	if err != nil {
		t.Fatalf("effective session id: %v", err)
	}
	if got != "xyz" {
		t.Fatalf("session id = %q, want xyz", got)
	}

	if _, err := effectiveSessionID(commonFlags{SessionID: "abc", ContinueID: "xyz"}); err == nil {
		t.Fatal("expected conflicting --session/--continue error")
	}
}

func TestRunSubcommandUsesKentSessionEnvAsWorkspaceContext(t *testing.T) {
	original := runPromptApp
	t.Cleanup(func() {
		runPromptApp = original
	})
	var gotOpts app.Options
	runPromptApp = func(ctx context.Context, opts app.Options, prompt string, timeout time.Duration, progress io.Writer) (app.RunPromptResult, error) {
		gotOpts = opts
		return app.RunPromptResult{Result: "done"}, nil
	}
	t.Setenv(sessionenv.SessionIDEnv, "session-from-env")

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout temp file: %v", err)
	}
	stderrFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create stderr temp file: %v", err)
	}
	os.Stdout = stdoutFile
	os.Stderr = stderrFile
	t.Cleanup(func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
	})

	if code := rootCommand([]string{"run", "hello"}, strings.NewReader(""), io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if gotOpts.SessionID != "" {
		t.Fatalf("session id = %q, want empty", gotOpts.SessionID)
	}
	if gotOpts.WorkspaceContextSessionID != "session-from-env" {
		t.Fatalf("workspace context session id = %q, want env session", gotOpts.WorkspaceContextSessionID)
	}
}

func TestRunSubcommandDefaultAgentWithFastUsesFastRole(t *testing.T) {
	original := runPromptApp
	t.Cleanup(func() {
		runPromptApp = original
	})
	var gotOpts app.Options
	runPromptApp = func(ctx context.Context, opts app.Options, prompt string, timeout time.Duration, progress io.Writer) (app.RunPromptResult, error) {
		gotOpts = opts
		return app.RunPromptResult{Result: "done"}, nil
	}

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout temp file: %v", err)
	}
	stderrFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create stderr temp file: %v", err)
	}
	os.Stdout = stdoutFile
	os.Stderr = stderrFile
	t.Cleanup(func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
	})

	if code := rootCommand([]string{"run", "--agent=default", "--fast", "hello"}, strings.NewReader(""), io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if gotOpts.AgentRole != config.BuiltInSubagentRoleFast {
		t.Fatalf("agent role = %q, want fast", gotOpts.AgentRole)
	}
}

func TestRunSubcommandContinueDefaultAgentAliasesMarkExplicitRoleOverride(t *testing.T) {
	for _, alias := range []string{"default", "none", "self"} {
		t.Run(alias, func(t *testing.T) {
			original := runPromptApp
			t.Cleanup(func() {
				runPromptApp = original
			})
			var gotOpts app.Options
			runPromptApp = func(ctx context.Context, opts app.Options, prompt string, timeout time.Duration, progress io.Writer) (app.RunPromptResult, error) {
				gotOpts = opts
				return app.RunPromptResult{Result: "done"}, nil
			}

			originalStdout := os.Stdout
			originalStderr := os.Stderr
			stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatalf("create stdout temp file: %v", err)
			}
			stderrFile, err := os.CreateTemp(t.TempDir(), "stderr")
			if err != nil {
				t.Fatalf("create stderr temp file: %v", err)
			}
			os.Stdout = stdoutFile
			os.Stderr = stderrFile
			t.Cleanup(func() {
				os.Stdout = originalStdout
				os.Stderr = originalStderr
				_ = stdoutFile.Close()
				_ = stderrFile.Close()
			})

			if code := rootCommand([]string{"run", "--continue", "session-123", "--agent", alias, "hello"}, strings.NewReader(""), io.Discard, io.Discard); code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if gotOpts.SessionID != "session-123" {
				t.Fatalf("session id = %q, want session-123", gotOpts.SessionID)
			}
			if gotOpts.AgentRole != "" {
				t.Fatalf("agent role = %q, want empty default role", gotOpts.AgentRole)
			}
			if !gotOpts.AgentRoleSet {
				t.Fatal("expected default alias to mark explicit role override")
			}
		})
	}
}

func TestSessionIDSubcommandPrintsKentSessionEnv(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, " session-from-env ")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := rootCommand([]string{"session-id"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout.String() != "session-from-env\n" {
		t.Fatalf("stdout = %q, want session id", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestSessionIDSubcommandFailsOutsideKentShell(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := rootCommand([]string{"session-id"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), sessionenv.SessionIDEnv+" is not set") {
		t.Fatalf("stderr = %q, want missing env error", stderr.String())
	}
}

func TestRegisterCommonFlagsDoesNotExposeRemovedBashTimeoutAlias(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	registerCommonFlags(fs, true)
	if fs.Lookup("bash-timeout-seconds") != nil {
		t.Fatal("expected removed --bash-timeout-seconds flag to be absent")
	}
}

func TestEffectiveRunAgentRoleRejectsConflictingFastFlag(t *testing.T) {
	if _, err := effectiveRunAgentRole("worker", true); err == nil {
		t.Fatal("expected conflicting fast role error")
	}
	role, err := effectiveRunAgentRole("fast", true)
	if err != nil {
		t.Fatalf("effectiveRunAgentRole: %v", err)
	}
	if role != config.BuiltInSubagentRoleFast {
		t.Fatalf("role = %q, want fast", role)
	}
}

func TestEffectiveRunAgentRoleAliasesDefaultSelectors(t *testing.T) {
	for _, alias := range []string{"default", "none", "self"} {
		t.Run(alias, func(t *testing.T) {
			role, err := effectiveRunAgentRole(alias, false)
			if err != nil {
				t.Fatalf("effectiveRunAgentRole: %v", err)
			}
			if role != "" {
				t.Fatalf("role = %q, want empty", role)
			}
		})
	}
	role, err := effectiveRunAgentRole("default", true)
	if err != nil {
		t.Fatalf("effectiveRunAgentRole default+fast: %v", err)
	}
	if role != config.BuiltInSubagentRoleFast {
		t.Fatalf("role = %q, want fast", role)
	}
}

func TestMarkExplicitCommonFlagsTracksOnlyParsedFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags := registerCommonFlags(fs, true)
	if err := fs.Parse([]string{"--workspace", "/tmp/w", "--openai-base-url=http://local/v1", "prompt"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	markExplicitCommonFlags(fs, flags)
	if !flags.WorkspaceExplicit {
		t.Fatal("expected workspace override to be marked explicit")
	}
	if !flags.OpenAIBaseURLExplicit {
		t.Fatal("expected openai base url override to be marked explicit")
	}
}

func TestMarkExplicitCommonFlagsIgnoresFlagTextInsidePrompt(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags := registerCommonFlags(fs, true)
	prompt := "please keep --workspace unchanged and ignore --openai-base-url"
	if err := fs.Parse([]string{"--continue", "session-123", prompt}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	markExplicitCommonFlags(fs, flags)
	if flags.WorkspaceExplicit {
		t.Fatal("did not expect prompt text to mark workspace explicit")
	}
	if flags.OpenAIBaseURLExplicit {
		t.Fatal("did not expect prompt text to mark openai base url explicit")
	}
}
