package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"core/cli/app/internal/startupconfig"
	"core/internal/testharness/testsetup"
	"core/server/runprompt"
	serverstartup "core/server/startup"
	askquestion "core/server/tools"
	"core/shared/config"
	"core/shared/protoapi"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/protocol"
	"core/shared/serverapi"
	"core/shared/sessioncontract"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/types/known/durationpb"
)

type staticRunPromptService struct {
	response *runpromptpb.Success
}

func (s staticRunPromptService) RunPrompt(
	context.Context,
	serverapi.RunPromptRequest,
	serverapi.RunPromptProgressSink,
) (*runpromptpb.Success, error) {
	return s.response, nil
}

func TestRunPromptCarriesTypedSelectionWarningsIntoCLIWarnings(t *testing.T) {
	result, err := runPrompt(t.Context(), staticRunPromptService{
		response: &runpromptpb.Success{
			SessionId: "session-id",
			Duration:  durationpb.New(time.Millisecond),
			SelectionWarnings: []runpromptpb.RunSelectionWarning{
				runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_AGENT_IGNORED_TO_PRESERVE_CACHE,
				runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_MODEL_IGNORED_TO_PRESERVE_CACHE,
			},
		},
	}, Options{}, "", "prompt", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 2 || strings.TrimSpace(result.Warnings[0]) == "" || strings.TrimSpace(result.Warnings[1]) == "" {
		t.Fatalf("selection warning count/content = %q", result.Warnings)
	}
}

func TestLoadRemoteAttachConfigRetainsInvocationWorkspaceForResume(t *testing.T) {
	home := newAppTestHome(t)
	workspace := t.TempDir()
	worktree := filepath.Join(home, config.ConfigDirName, "worktrees", "project", "feature")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	configureAppTestServerPort(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	store := createAuthoritativeAppSession(t, cfg.PersistenceRoot, cfg.WorkspaceRoot)

	got, err := loadRemoteAttachConfig(Options{
		WorkspaceRoot: worktree,
		SessionID:     store.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("loadRemoteAttachConfig: %v", err)
	}
	gotCanonical, err := config.CanonicalWorkspaceRoot(got.WorkspaceRoot)
	if err != nil {
		t.Fatalf("canonical got workspace: %v", err)
	}
	wantCanonical, err := config.CanonicalWorkspaceRoot(worktree)
	if err != nil {
		t.Fatalf("canonical want workspace: %v", err)
	}
	if gotCanonical != wantCanonical {
		t.Fatalf("initial targeting root = %q, want invocation workspace %q", got.WorkspaceRoot, worktree)
	}
}

func TestRunPromptFromWorktreeUsesKentSessionWorkspaceContext(t *testing.T) {
	home := newAppTestHome(t)
	workspace := t.TempDir()
	worktree := filepath.Join(home, config.ConfigDirName, "worktrees", "project", "feature")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	configureAppTestServerPort(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	parent := createAuthoritativeAppSession(t, cfg.PersistenceRoot, cfg.WorkspaceRoot)

	fakeResponses, hits := newFakeResponsesServer(t, []string{"worktree reply"})
	defer fakeResponses.Close()

	stopServer := startStandingRunPromptServer(t, workspace, fakeResponses.URL)
	defer stopServer()

	result, err := RunPrompt(context.Background(), Options{
		WorkspaceRoot:             worktree,
		WorkspaceContextSessionID: parent.Meta().SessionID,
		Model:                     "gpt-6-sol",
	}, "hello from worktree", 0, nil)
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if result.Result != "worktree reply" {
		t.Fatalf("result = %q, want worktree reply", result.Result)
	}
	if result.SessionID == parent.Meta().SessionID {
		t.Fatal("expected worktree run to create a child run instead of continuing parent session")
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one llm call, got %d", hits.Load())
	}
}

func TestRunPromptRejectsStaleWorkspaceContextSession(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	selected := createAuthoritativeAppSession(t, cfg.PersistenceRoot, cfg.WorkspaceRoot)

	fakeResponses, hits := newFakeResponsesServer(t, []string{"workspace reply"})
	defer fakeResponses.Close()
	stopServer := startStandingRunPromptServer(t, workspace, fakeResponses.URL)
	defer stopServer()

	const missingID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	for _, tc := range []struct {
		name      string
		options   Options
		inherited bool
		denied    bool
	}{
		{
			name:      "inherited caller attachment",
			options:   Options{WorkspaceRoot: workspace, WorkspaceContextSessionID: missingID},
			inherited: true,
		},
		{
			name:      "caller validation with explicit workspace",
			options:   Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true, WorkspaceContextSessionID: missingID},
			inherited: true,
			denied:    true,
		},
		{
			name:    "selected continued session",
			options: Options{WorkspaceRoot: workspace, SessionID: missingID},
		},
		{
			name:      "selected session does not bypass missing caller authorization",
			options:   Options{WorkspaceRoot: workspace, SessionID: selected.Meta().SessionID, WorkspaceContextSessionID: missingID},
			inherited: true,
			denied:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RunPrompt(context.Background(), tc.options, "must not dispatch", 0, nil)
			var missing *sessioncontract.SessionNotFoundError
			if !errors.Is(err, sessioncontract.ErrSessionNotFound) ||
				!errors.As(err, &missing) || missing.SessionID != missingID {
				t.Fatalf("error = %v, want typed missing session %s", err, missingID)
			}
			if errors.Is(err, startupconfig.ErrWorkspaceContextSessionMissing) != tc.inherited {
				t.Fatalf("incorrect inherited context classification: %v", err)
			}
			var denied *serverapi.SubagentLaunchDeniedError
			if errors.As(err, &denied) != tc.denied ||
				(denied != nil && denied.Kind != serverapi.SubagentLaunchDenialCallerMissing) {
				t.Fatalf("incorrect caller authorization classification: %v", err)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatalf("expected no llm calls, got %d", hits.Load())
	}
}

func TestRunPromptPreservesNotCallableDenialFromRemote(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	caller := createAuthoritativeAppSession(t, cfg.PersistenceRoot, cfg.WorkspaceRoot)
	role := "blocked"
	if err := os.MkdirAll(filepath.Join(workspace, config.ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, config.ConfigDirName, "config.toml"), []byte("[subagents.blocked]\nagent_callable = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	responses, hits := newFakeResponsesServer(t, nil)
	defer responses.Close()
	stop := startStandingRunPromptServer(t, workspace, responses.URL)
	defer stop()

	_, err := RunPrompt(t.Context(), Options{
		WorkspaceRoot:             workspace,
		WorkspaceContextSessionID: caller.Meta().SessionID,
		AgentRole:                 &role,
	}, "must not dispatch", 0, nil)
	var denied *serverapi.SubagentLaunchDeniedError
	if !errors.As(err, &denied) || denied.Kind != serverapi.SubagentLaunchDenialNotCallable ||
		denied.Target == nil || *denied.Target != role {
		t.Fatalf("remote error = %T %v, want typed NotCallable denial for %s", err, err, role)
	}
	if hits.Load() != 0 {
		t.Fatalf("denied role dispatched %d model requests", hits.Load())
	}
}

func waitForConfiguredRunPromptDaemon(t *testing.T, workspace string) {
	t.Helper()
	loadCfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	healthURL := config.ServerHTTPBaseURL(loadCfg) + protocol.HealthPath
	client := &http.Client{Timeout: 250 * time.Millisecond}
	testsetup.RequireUntil(t, time.Now().Add(5*time.Second), 10*time.Millisecond, func() bool {
		resp, err := client.Get(healthURL)
		if err == nil {
			_ = resp.Body.Close()
			return resp.StatusCode == http.StatusOK
		}
		return false
	}, "configured daemon did not become healthy at %s", healthURL)
}

func TestRunPromptAskHandlerReturnsError(t *testing.T) {
	_, err := runprompt.RunPromptAskHandler(askquestion.AskQuestionRequest{Question: "Need approval?"})
	if !errors.Is(err, runprompt.ErrHeadlessAskUnsupported) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPromptUsesConfiguredDaemonWithoutLocalAuth(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)

	fakeResponses, hits := newFakeResponsesServer(t, []string{"daemon reply"})
	defer fakeResponses.Close()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, fakeResponses.URL))

	srv, err := serverstartup.StartServeServer(context.Background(), serverstartup.Request{
		WorkspaceRoot:         workspace,
		WorkspaceRootExplicit: true,
		Model:                 "gpt-6-sol",
	})

	if err != nil {
		t.Fatalf("serve.Start: %v", err)
	}
	defer func() { _ = srv.Close() }()

	stopServing := serveAppServer(t, srv)
	defer stopServing()

	waitForConfiguredRunPromptDaemon(t, workspace)

	result, err := RunPrompt(context.Background(), Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true}, "hello through daemon", 0, nil)
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if result.Result != "daemon reply" {
		t.Fatalf("result = %q, want %q", result.Result, "daemon reply")
	}
	if hits.Load() != 1 {
		t.Fatalf("expected daemon-backed llm call once, got %d", hits.Load())
	}

}

func TestStartRunPromptClientWithoutServerRequiresRunningServer(t *testing.T) {
	newAppTestHome(t)
	workspace := t.TempDir()
	configureAppTestServerPort(t)

	// kent run is a pure client: with no server running it cannot start one of
	// its own, so it must fail with the "server required" error.
	runClient, closeFn, err := startRunPromptClient(context.Background(), Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true})
	if !errors.Is(err, errRunRequiresServer) {
		t.Fatalf("startRunPromptClient error = %v, want errRunRequiresServer", err)
	}
	if runClient != nil {
		t.Fatalf("expected no run client, got %v", runClient)
	}
	if closeFn != nil {
		t.Fatal("expected no close function when startup fails")
	}
}

func publishConfiguredRemoteForWorkspace(t *testing.T, workspace string) func() {
	t.Helper()
	identity := protocol.ServerIdentity{
		ProtocolVersion: protocol.Version,
		ServerID:        "stale-daemon",
		PID:             222,
	}
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer func() { _ = ws.Close() }()
		if err := serveConfiguredRemoteHandshake(ws, identity); err != nil {
			return
		}
		for {
			var encoded []byte
			if err := websocket.Message.Receive(ws, &encoded); err != nil {
				return
			}
			envelope, err := protoapi.DecodeEnvelope(encoded)
			if err != nil || envelope.GetCall() == nil {
				return
			}
			call := envelope.GetCall()
			response, err := protoapi.EncodeEnvelope(&sharedpb.Envelope{
				Frame: &sharedpb.Envelope_TransportFailure{TransportFailure: &sharedpb.TransportFailure{
					Code:        sharedpb.TransportFailureCode_TRANSPORT_FAILURE_CODE_UNKNOWN_OPERATION,
					Correlation: call.Correlation,
				}},
			})
			if err != nil || websocket.Message.Send(ws, response) != nil {
				return
			}
		}
	}))
	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		server.Close()
		t.Fatalf("SplitHostPort: %v", err)
	}
	t.Setenv("KENT_SERVER_HOST", host)
	t.Setenv("KENT_SERVER_PORT", port)
	return server.Close
}

func serveConfiguredRemoteHandshake(ws *websocket.Conn, identity protocol.ServerIdentity) error {
	var encoded []byte
	if err := websocket.Message.Receive(ws, &encoded); err != nil {
		return err
	}
	envelope, err := protoapi.DecodeEnvelope(encoded)
	if err != nil {
		return err
	}
	call := envelope.GetCall()
	if call == nil || call.Correlation == nil {
		return errors.New("correlated Handshake call is required")
	}
	method := connectionpb.File_kent_api_connection_connection_proto.Services().
		ByName("ConnectionService").Methods().ByName("Handshake")
	operation, err := protoapi.OperationFromDescriptor(method)
	if err != nil {
		return err
	}
	if call.Operation != operation.Name {
		return errors.New("Handshake must be the first operation")
	}
	resultIdentity := &connectionpb.ServerIdentity{
		ProtocolVersion: identity.ProtocolVersion,
		ServerId:        identity.ServerID,
		Pid:             int32(identity.PID),
	}
	if identity.PersistenceRootID != "" {
		resultIdentity.PersistenceRootId = &identity.PersistenceRootID
	}
	payload, err := protoapi.Encode(&connectionpb.HandshakeResult{
		Outcome: &connectionpb.HandshakeResult_Success{
			Success: &connectionpb.HandshakeSuccess{Identity: resultIdentity},
		},
	})
	if err != nil {
		return err
	}
	response, err := protoapi.EncodeEnvelope(&sharedpb.Envelope{
		Frame: &sharedpb.Envelope_Result{Result: &sharedpb.Result{
			Operation: operation.Name, Correlation: call.Correlation, Payload: payload,
		}},
	})
	if err != nil {
		return err
	}
	return websocket.Message.Send(ws, response)
}
