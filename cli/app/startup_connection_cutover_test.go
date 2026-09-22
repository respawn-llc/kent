package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"core/cli/app/internal/startupconfig"
	"core/internal/testharness/testsetup"
	serverstartup "core/server/startup"
	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

func TestStartupAndHeadlessUseSelectedConnectionDespiteUnavailableDefault(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	responses, hits := newFakeResponsesServer(t, []string{"selected connection reply"})
	defer responses.Close()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	a, b := config.ConnectionID("a"), config.ConnectionID("b")
	missing := "KENT_TEST_UNAVAILABLE_CONNECTION_KEY"
	t.Setenv(missing, "")
	cfg.Settings.Connection = &a
	cfg.Settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		a: {Protocol: config.ConnectionResponses, Endpoint: &responses.URL, EnvironmentVariable: &missing},
		b: {Protocol: config.ConnectionResponses, Endpoint: &responses.URL},
	}
	testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, cfg.Settings)
	if err := os.MkdirAll(filepath.Join(workspace, config.ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, config.ConfigDirName, "config.toml"), []byte("connection = \"b\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := startConfiguredDaemonFixture(t, workspace, serverstartup.Request{WorkspaceRoot: workspace, WorkspaceRootExplicit: true})
	server := fixture.attachRemoteSessionServer(t, Options{WorkspaceRoot: workspace}, newHeadlessAuthInteractor())
	statusA, err := server.remote.GetBootstrapStatus(t.Context(), &authpb.GetBootstrapStatusRequest{Target: protoapi.ExistingConnectionTarget(a)})
	if err != nil || statusA.AuthReady {
		t.Fatalf("fixture default A must be unavailable: %v, %v", statusA, err)
	}
	plan, err := newSessionLaunchPlanner(server).PlanSession(t.Context(), sessionLaunchRequest{
		Mode: launchModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ActiveSettings.Connection == nil || *plan.ActiveSettings.Connection != b {
		t.Fatalf("Session plan selected %v instead of B", plan.ActiveSettings.Connection)
	}
	interactor := &interactiveAuthInteractor{pickMethod: func(authInteraction) (authMethodPickerResult, error) {
		t.Fatal("working B must not prompt for unavailable A")
		return authMethodPickerResult{}, nil
	}}
	if err := server.EnsureAuthReady(t.Context(), *plan.ActiveSettings.Connection, interactor, true); err != nil {
		t.Fatal(err)
	}
	resumed := fixture.attachRemoteSessionServer(t, Options{WorkspaceRoot: workspace, SessionID: plan.SessionID}, interactor)
	resumePlan, err := newSessionLaunchPlanner(resumed).PlanSession(t.Context(), sessionLaunchRequest{
		Mode: launchModeInteractive, Intent: serverapi.OpenExistingSessionLaunchIntent(sessionLifecycleSessionID(t, plan.SessionID)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumePlan.ActiveSettings.Connection == nil || *resumePlan.ActiveSettings.Connection != b {
		t.Fatalf("bound Session lost B: %v", resumePlan.ActiveSettings.Connection)
	}
	if err := resumed.EnsureAuthReady(t.Context(), *resumePlan.ActiveSettings.Connection, newHeadlessAuthInteractor(), false); err != nil {
		t.Fatalf("selected Session reauthentication: %v", err)
	}
	result, err := RunPrompt(t.Context(), Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true}, "hello", 0, nil)
	if err != nil || result.Result != "selected connection reply" || hits.Load() != 1 {
		t.Fatalf("headless B dispatch: %+v, %v, hits=%d", result, err, hits.Load())
	}
}

func TestStartupResumeKeepsInitialEndpointAndUsesServerSessionWorkspace(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	store := createAuthoritativeAppSession(t, cfg.PersistenceRoot, workspace)
	serverWorkspace, err := config.CanonicalWorkspaceRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	startConfiguredDaemonFixture(t, workspace, serverstartup.Request{WorkspaceRoot: workspace, WorkspaceRootExplicit: true})
	initialWorkspace := t.TempDir()
	for root, body := range map[string]string{
		initialWorkspace: fmt.Sprintf("server_host = %q\nserver_port = %d\nmodel = false\n", cfg.Settings.ServerHost, cfg.Settings.ServerPort),
		workspace:        "server_host = \"127.0.0.1\"\nserver_port = 1\n",
	} {
		directory := filepath.Join(root, config.ConfigDirName)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "config.toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KENT_SERVER_HOST", "")
	t.Setenv("KENT_SERVER_PORT", "")
	opts := Options{WorkspaceRoot: initialWorkspace, SessionID: store.Meta().SessionID}
	interactive, err := startSessionServer(t.Context(), opts, newHeadlessAuthInteractor(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer interactive.Close()
	binding, attached := interactive.ProjectBinding()
	if !attached || binding.WorkspaceRoot != serverWorkspace || interactive.Connection().WorkspaceRoot != initialWorkspace {
		t.Fatalf("resume binding=%+v, initial=%+v", binding, interactive.Connection())
	}
	for _, headless := range []Options{
		opts,
		{WorkspaceRoot: initialWorkspace, WorkspaceContextSessionID: store.Meta().SessionID},
	} {
		service, closeRemote, err := startRunPromptClient(t.Context(), headless)
		if err != nil {
			t.Fatal(err)
		}
		remote := service.(*client.Remote)
		binding, attached := remote.ProjectBinding()
		_ = closeRemote()
		if !attached || binding.WorkspaceRoot != serverWorkspace {
			t.Fatalf("headless resume binding: %+v", binding)
		}
		intent, err := runPromptLaunchIntent(headless, headless.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if headless.SessionID == "" && intent.Kind() != serverapi.SessionLaunchIntentCreateNew {
			t.Fatalf("inherited context changed create intent: %v", intent.Kind())
		}
	}
	for _, inherited := range []bool{false, true} {
		missing := Options{WorkspaceRoot: initialWorkspace, SessionID: "missing"}
		if inherited {
			missing.SessionID, missing.WorkspaceContextSessionID = "", "missing"
		}
		_, _, err := startRunPromptClient(t.Context(), missing)
		if !errors.Is(err, sessioncontract.ErrSessionNotFound) ||
			errors.Is(err, startupconfig.ErrWorkspaceContextSessionMissing) != inherited {
			t.Fatalf("missing Session guidance (inherited=%t): %v", inherited, err)
		}
	}
	missingCaller := Options{WorkspaceRoot: workspace, WorkspaceRootExplicit: true, WorkspaceContextSessionID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"}
	// The explicit workspace selects the endpoint here, so restore its actual
	// address before exercising caller validation at RunPrompt.
	t.Setenv("KENT_SERVER_HOST", cfg.Settings.ServerHost)
	t.Setenv("KENT_SERVER_PORT", fmt.Sprint(cfg.Settings.ServerPort))
	_, err = RunPrompt(t.Context(), missingCaller, "must not dispatch", 0, nil)
	if !errors.Is(err, startupconfig.ErrWorkspaceContextSessionMissing) {
		t.Fatalf("missing caller with explicit workspace lost guidance: %v", err)
	}
}

func TestPreSessionChatSettingsUseServerBaselineWithoutCreatingSession(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	cfg.Settings.Model = "gpt-5.4"
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, cfg.Settings)
	if err := os.MkdirAll(filepath.Join(workspace, config.ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, config.ConfigDirName, "config.toml"), []byte("model = \"gpt-5.4\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := startConfiguredDaemonFixture(t, workspace, serverstartup.Request{WorkspaceRoot: workspace, WorkspaceRootExplicit: true})
	unbound := fixture.attachRemoteSessionServer(t, Options{WorkspaceRoot: workspace, Model: "client-request-only"}, newHeadlessAuthInteractor())
	resolved, err := unbound.remote.ResolveProjectPath(t.Context(), &projectpb.ResolvePathRequest{Path: workspace})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := unbound.BindProjectWorkspace(t.Context(), resolved.Binding.ProjectId, resolved.Binding.WorkspaceId)
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	readErr := errors.New("header settings unavailable")
	planner := newSessionLaunchPlanner(failingHeaderSettingsServer{launchPlannerServer: bound, err: readErr})
	planner.pickSession = func(ctx context.Context, loader sessionPageLoader, theme string, header sessionPickerHeaderInfo) (sessionPickerResult, error) {
		model := newSessionPickerModel(ctx, loader, theme, header)
		model.Init()
		if model.main.bodyRequest == nil || model.subagents.bodyRequest == nil {
			t.Fatal("both tab loads must start independently of header settings")
		}
		model.Update(model.collectModelFactsCmd()())
		if model.main.bodyRequest == nil || model.subagents.bodyRequest == nil ||
			model.startupStatus.notice.Diagnostic != readErr {
			t.Fatal("header failure must remain visible without stopping tab loads")
		}
		return sessionPickerCancelResult{}, nil
	}
	if _, err := planner.selectSession(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	planner = newSessionLaunchPlanner(bound)
	header, err := planner.sessionPickerHeaderInfo()
	if err != nil {
		t.Fatal(err)
	}
	facts, err := header.loadModelFacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if facts == nil || facts.Name == nil || *facts.Name != cfg.Settings.Model {
		t.Fatalf("pre-Session model did not follow server settings: %+v", facts)
	}
	page, err := bound.ProjectViewClient().ListSessionPage(t.Context(), &projectpb.SessionPageRequest{
		ProjectId: resolved.Binding.ProjectId, Category: projectpb.SessionCategory_SESSION_CATEGORY_MAIN,
	})
	if err != nil || len(page.GetSessions()) != 0 {
		t.Fatalf("reading settings created a Session: %v, %v", page, err)
	}
}

type failingHeaderSettingsServer struct {
	launchPlannerServer
	err error
}

func (s failingHeaderSettingsServer) ChatSettingsClient() apicontract.ChatSettingsService {
	return failingHeaderSettingsReader{ChatSettingsService: s.launchPlannerServer.ChatSettingsClient(), err: s.err}
}

type failingHeaderSettingsReader struct {
	apicontract.ChatSettingsService
	err error
}

func (s failingHeaderSettingsReader) ReadChatSettings(context.Context, *chatsettingspb.ReadRequest) (*chatsettingspb.ReadSuccess, error) {
	return nil, s.err
}
