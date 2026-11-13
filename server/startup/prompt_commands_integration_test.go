package startup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	modelstub "core/internal/testharness/pty/blackbox"
	"core/internal/testharness/requestfixture"
	"core/internal/testharness/testsetup"
	"core/server/authmanager"
	"core/server/authservice"
	"core/server/onboardingimports"
	"core/server/onboardingservice"
	"core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	onboardingpb "core/shared/protoapi/gen/kent/api/onboarding"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"

	"core/shared/runtimeids"
	"github.com/google/uuid"
)

func TestRemotePromptCommandStartupCatalogAndInvocationUseImportedServerContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configureServeTestServerPort(t)
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	cfg := loadServeTestConfig(t, Request{WorkspaceRoot: workspaceA})
	store := testsetup.OpenStore(t, cfg.PersistenceRoot)
	binding, err := store.RegisterWorkspaceBinding(t.Context(), workspaceA)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	if _, err := store.AttachWorkspaceToProject(context.Background(), binding.ProjectID, workspaceB); err != nil {
		t.Fatalf("AttachWorkspaceToProject: %v", err)
	}

	sourceRoot := filepath.Join(os.Getenv("HOME"), ".claude", "commands")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "remote_demo.md"), []byte("server body $ARGUMENTS"), 0o600); err != nil {
		t.Fatal(err)
	}
	providerID := string(onboardingimports.ProviderClaudeCode)
	connections := authservice.NewBootstrapService(t.Context(), authservice.NewConnectionResolver(cfg.PersistenceRoot, auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil), nil), auth.OpenAIOAuthOptions{})
	endpoint := "http://localhost:1234/v1"
	if _, err := connections.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{Change: &authpb.ConfigureConnectionRequest_PendingSetup{
		PendingSetup: &authpb.ConnectionDefinition{Id: "local", Protocol: authpb.ConnectionProtocol_CONNECTION_PROTOCOL_RESPONSES, Endpoint: &endpoint},
	}}); err != nil {
		t.Fatal(err)
	}
	finalizer, err := onboarding.NewFinalizer(onboarding.Options{Baseline: config.DefaultOnboardingSettings(), PersistenceRoot: cfg.PersistenceRoot, WorkspaceRoot: workspaceA, HomeDir: os.Getenv("HOME"), SettingsPath: cfg.Source.File(config.FileGlobal).Path, Connections: connections})
	if err != nil {
		t.Fatalf("NewFinalizer: %v", err)
	}
	if _, err := finalizer.Finalize(context.Background(), &onboardingpb.FinalizeRequest{
		CommandsImport: &onboardingpb.ImportSelection{
			Mode:             onboardingpb.ImportMode_IMPORT_MODE_SYMLINK_SOURCE,
			ImportProviderId: &providerID,
			SourceRootPath:   &sourceRoot,
		},
	}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	output := "accepted"
	responseServer, err := modelstub.StartResponsesStub([]modelstub.RequiredOperation{{
		ID: uuid.New(), Route: modelstub.RouteResponses, Outcome: modelstub.OutcomeStream,
		Output: &output, ResponsePhase: modelstub.NewResponsePhase(modelstub.ResponsePhaseFinal),
	}})
	if err != nil {
		t.Fatalf("StartResponsesStub: %v", err)
	}
	t.Cleanup(func() { _ = responseServer.Stop() })
	workspaceConfigDir := filepath.Join(workspaceB, config.ConfigDirName)
	if err := os.MkdirAll(workspaceConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceConfigDir, "config.toml"), []byte(
		"model = \"gpt-6-sol\"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, responseServer.URL()))
	request := Request{
		WorkspaceRoot:         workspaceA,
		WorkspaceRootExplicit: true,
		LoadOptions: config.LoadOptions{
			Model: "gpt-6-sol",
		},
	}
	cfg = loadServeTestConfig(t, request)
	server := newMemoryServeTestServer(t, request, store)
	connection, err := cfg.Settings.SelectedConnection()
	if err != nil || connection.Endpoint == nil || *connection.Endpoint != responseServer.URL() {
		t.Fatalf("selected connection = %+v, error = %v", connection, err)
	}
	startServingTestServer(t, server, cfg)

	var remote *client.Remote
	testsetup.RequireUntil(t, time.Now().Add(5*time.Second), 10*time.Millisecond, func() bool {
		remote, err = client.DialRemoteURLForProjectWorkspace(context.Background(), config.ServerRPCURL(cfg), binding.ProjectID, workspaceB)
		return err == nil
	}, "DialRemoteURLForProjectWorkspace")
	defer func() { _ = remote.Close() }()
	catalog, err := remote.GetPromptCommandCatalog(context.Background(), &promptcommandpb.GetCatalogRequest{})
	if err != nil {
		t.Fatalf("GetPromptCommandCatalog: %v", err)
	}
	if !slices.ContainsFunc(catalog.Commands, func(command *promptcommandpb.CatalogEntry) bool {
		return command.Name == "prompt:remote_demo" && command.Preview == "server body $ARGUMENTS"
	}) {
		t.Fatalf("catalog = %+v", catalog.Commands)
	}

	intent, err := protoapi.SessionLaunchRequestIntentToProto(
		serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	)
	if err != nil {
		t.Fatalf("convert Session launch intent: %v", err)
	}
	plan, err := remote.PlanSession(context.Background(), &sessionlaunchpb.SessionPlanRequest{
		Mode:   sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
		Intent: intent,
	})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	if _, err := remote.Steer(context.Background(), &chatpb.SteerRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(),
		Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{Session: &chatpb.ExistingSessionRequestTarget{
			SessionId: requestfixture.Selector(t, plan.Plan.SessionId),
		}}},
		Activation: &chatpb.Activation{Input: &chatpb.Activation_Command{Command: &chatpb.CommandInvocation{
			CatalogIdentity: "prompt:remote_demo", Token: "/prompt:remote_demo", SeparatorWhitespace: " ", Arguments: "hello world",
		}}},
	}); err != nil {
		t.Fatalf("Steer: %v", err)
	}
	var body json.RawMessage
	testsetup.RequireUntil(t, time.Now().Add(10*time.Second), 10*time.Millisecond, func() bool {
		for _, call := range responseServer.Snapshot().Observed {
			if call.Route == modelstub.RouteResponses {
				body = append(json.RawMessage(nil), call.Body...)
				return true
			}
		}
		return false
	}, "timed out waiting for provider request")
	type responseContent struct{ Type, Text string }
	type responseInput struct {
		Type, Role string
		Content    []responseContent
	}
	var payload struct{ Input []responseInput }
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode provider request: %v", err)
	}
	if !slices.ContainsFunc(payload.Input, func(item responseInput) bool {
		return item.Type == "message" && item.Role == "user" &&
			slices.ContainsFunc(item.Content, func(content responseContent) bool {
				return content.Type == "input_text" && content.Text == "server body hello world"
			})
	}) {
		t.Fatalf("provider request omitted resolved prompt body: %+v", payload.Input)
	}
}
