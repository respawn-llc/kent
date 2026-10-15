package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"core/cli/app"
	"core/server/transport"
	"core/shared/apicontract"
	"core/shared/config"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/protocol"
	"core/shared/serverapi"
)

type acceptedSteerFailure struct {
	release <-chan struct{}
}

func (s acceptedSteerFailure) RunPrompt(ctx context.Context, _ *runpromptpb.Request, progress serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	progress.PublishRunPromptProgress(&runpromptpb.ProgressEvent{Payload: &runpromptpb.ProgressEvent_ActiveSteerAccepted{
		ActiveSteerAccepted: &runpromptpb.ActiveSteerAccepted{IgnoredSettings: []runpromptpb.IgnoredRunSetting{
			runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_AGENT,
			runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_MODEL,
		}},
	}})
	select {
	case <-s.release:
		return nil, errors.New("observed execution failed")
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

type admissionRunGateway struct {
	transport.GatewayDependencies
	run apicontract.RunPromptService
}

func (g admissionRunGateway) RunPromptClientForProjectWorkspace(context.Context, string, string) (apicontract.RunPromptService, error) {
	return g.run, nil
}

func (g admissionRunGateway) RunPromptClientForProjectWorkspaceID(context.Context, string, string) (apicontract.RunPromptService, error) {
	return g.run, nil
}

func TestRunAdmissionWarningsSurviveRemoteTerminalFailure(t *testing.T) {
	for _, mode := range []struct {
		name string
		args []string
		json bool
	}{
		{name: "text"},
		{name: "quiet", args: []string{"--quiet"}},
		{name: "json", args: []string{"--output-mode", "json"}, json: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			fixture := newWorktreeCommandFixture(t)
			release := make(chan struct{})
			gateway, err := transport.NewGateway(admissionRunGateway{GatewayDependencies: fixture.core, run: acceptedSteerFailure{release: release}}, protocol.ServerIdentity{
				ProtocolVersion: protocol.Version, ServerID: "admission-test", PID: os.Getpid(),
				PersistenceRootID: config.PersistenceRootHash(fixture.core.Config().PersistenceRoot),
			})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(gateway.Handler())
			defer server.Close()
			host, port, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("KENT_SERVER_HOST", host)
			t.Setenv("KENT_SERVER_PORT", port)
			previous := runPromptApp
			defer func() { runPromptApp = previous }()
			var beforeOut, beforeErr []byte
			runPromptApp = func(ctx context.Context, opts app.Options, prompt string, timeout time.Duration, progress serverapi.RunPromptProgressSink) (app.RunPromptResult, error) {
				ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				return app.RunPrompt(ctx, opts, prompt, timeout, serverapi.RunPromptProgressFunc(func(event *runpromptpb.ProgressEvent) {
					if progress != nil {
						progress.PublishRunPromptProgress(event)
					}
					if event.GetActiveSteerAccepted() != nil {
						var err error
						beforeOut, err = os.ReadFile(os.Stdout.Name())
						if err != nil {
							t.Error(err)
						}
						beforeErr, err = os.ReadFile(os.Stderr.Name())
						if err != nil {
							t.Error(err)
						}
						close(release)
					}
				}))
			}
			args := append([]string{"--continue", fixture.sessionID, "--fast", "--model", "gpt-6-sol"}, mode.args...)
			args = append(args, "redirect")
			code, stdout, stderr := captureRunOutput(t, args)
			if code != 1 || len(beforeOut) != 0 {
				t.Fatalf("exit=%d stdout before outcome=%q, stderr=%s", code, beforeOut, stderr)
			}
			if mode.json {
				var result runJSONResult
				if err := json.Unmarshal(stdout, &result); err != nil {
					t.Fatal(err)
				}
				if result.Status != "error" || len(result.Warnings) != 2 || len(beforeErr) != 0 || len(stderr) != 0 {
					t.Fatalf("result=%+v before stderr=%q stderr=%q", result, beforeErr, stderr)
				}
			} else {
				// Two acceptance warnings and their separator precede the terminal
				// failure. Completion adds only the error, without repeating them.
				if len(stdout) != 0 || bytes.Count(beforeErr, []byte("\n")) != 3 || bytes.Count(stderr, []byte("\n")) != 4 {
					t.Fatalf("before stderr=%q final stderr=%q stdout=%q", beforeErr, stderr, stdout)
				}
			}
		})
	}
}
