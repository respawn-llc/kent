package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"core/server/auth"
	serverbootstrap "core/server/bootstrap"
	"core/server/metadata"
	"core/server/session"
	"core/shared/clientui"
	brand "core/shared/config"
	"core/shared/protoapi"
	capabilitypb "core/shared/protoapi/gen/kent/api/capability"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"

	"google.golang.org/protobuf/types/known/emptypb"
)

func TestNewBuildsReusableServerCore(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	if _, err := metadata.RegisterBinding(context.Background(), resolved.Config.PersistenceRoot, resolved.Config.WorkspaceRoot); err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())

	if appCore.Config().WorkspaceRoot == "" {
		t.Fatal("expected workspace root")
	}
	if appCore.ProjectID() == "" {
		t.Fatal("expected project id")
	}
	if appCore.AuthManager() == nil {
		t.Fatal("expected auth manager")
	}
	if appCore.Background() == nil {
		t.Fatal("expected background manager")
	}
	if appCore.ProjectViewClient() == nil || appCore.ProcessViewClient() == nil || appCore.ProcessControlClient() == nil || appCore.SessionLaunchClient() == nil || appCore.SessionViewClient() == nil || appCore.SessionLifecycleClient() == nil || appCore.SessionTranscriptClient() == nil || appCore.RunPromptClient() == nil {
		t.Fatal("expected core clients to be wired")
	}
	if appCore.CapabilityFactsClient() == nil {
		t.Fatal("expected capability facts client to be wired")
	}
	if _, err := appCore.ProjectViewClient().ListProjects(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("ListProjects via core client: %v", err)
	}
	facts, err := appCore.CapabilityFactsClient().GetFacts(context.Background(), &capabilitypb.GetFactsRequest{})
	if err != nil {
		t.Fatalf("GetFacts via core client: %v", err)
	}
	if facts.Defaults.PrimaryModelId == "" {
		t.Fatalf("capability facts missing defaults: %+v", facts)
	}
}

func TestCapabilityFactsClientReportsAbsenceForUnconfiguredCore(t *testing.T) {
	var zeroCore Core
	if client := zeroCore.CapabilityFactsClient(); client != nil {
		t.Fatalf("zero Core capability facts client = %T, want nil", client)
	}

	var nilCore *Core
	if client := nilCore.CapabilityFactsClient(); client != nil {
		t.Fatalf("nil Core capability facts client = %T, want nil", client)
	}
}

func TestProcessClientsReportAbsenceForUnconfiguredCore(t *testing.T) {
	var zeroCore Core
	if client := zeroCore.ProcessViewClient(); client != nil {
		t.Fatalf("zero Core process view client = %T, want nil", client)
	}
	if client := zeroCore.ProcessControlClient(); client != nil {
		t.Fatalf("zero Core process control client = %T, want nil", client)
	}

	var nilCore *Core
	if client := nilCore.ProcessViewClient(); client != nil {
		t.Fatalf("nil Core process view client = %T, want nil", client)
	}
	if client := nilCore.ProcessControlClient(); client != nil {
		t.Fatalf("nil Core process control client = %T, want nil", client)
	}
}

func TestPromptCommandCatalogUsesRequestedWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceA})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	bindingA, err := metadata.RegisterBinding(context.Background(), resolved.Config.PersistenceRoot, workspaceA)
	if err != nil {
		t.Fatalf("RegisterBinding A: %v", err)
	}
	bindingB, err := metadata.RegisterBinding(context.Background(), resolved.Config.PersistenceRoot, workspaceB)
	if err != nil {
		t.Fatalf("RegisterBinding B: %v", err)
	}
	writeCorePromptFixture(t, workspaceA, "only-a", "A")
	writeCorePromptFixture(t, workspaceB, "only_b", "B")
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())

	client, err := appCore.PromptCommandCatalogClientForProjectWorkspace(context.Background(), bindingB.ProjectID, workspaceB)
	if err != nil {
		t.Fatalf("PromptCommandCatalogClientForProjectWorkspace: %v", err)
	}
	response, err := client.GetPromptCommandCatalog(context.Background(), &promptcommandpb.GetCatalogRequest{})
	if err != nil {
		t.Fatalf("GetPromptCommandCatalog: %v", err)
	}
	foundWorkspaceB := false
	for _, command := range response.Commands {
		if command.Name == "prompt:only_b" && command.Preview == "B" {
			foundWorkspaceB = true
			break
		}
	}
	if !foundWorkspaceB {
		t.Fatalf("workspace B catalog = %+v, want prompt:only_b from B (A binding %q, B binding %q)", response.Commands, bindingA.ProjectID, bindingB.ProjectID)
	}
}

func TestPromptCommandCatalogUsesRegisteredWorktreeRoot(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	worktree := filepath.Join(t.TempDir(), "feature")
	t.Setenv("HOME", home)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	binding, err := metadata.RegisterBinding(context.Background(), resolved.Config.PersistenceRoot, workspace)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("MkdirAll worktree: %v", err)
	}
	if err := appCore.MetadataStore().UpsertWorktreeRecord(context.Background(), metadata.WorktreeRecord{
		ID:            "worktree-catalog-test",
		WorkspaceID:   binding.WorkspaceID,
		CanonicalRoot: worktree,
	}); err != nil {
		t.Fatalf("UpsertWorktreeRecord: %v", err)
	}
	writeCorePromptFixture(t, worktree, "only_worktree", "worktree")

	client, err := appCore.PromptCommandCatalogClientForProjectWorkspace(context.Background(), binding.ProjectID, worktree)
	if err != nil {
		t.Fatalf("PromptCommandCatalogClientForProjectWorkspace: %v", err)
	}
	response, err := client.GetPromptCommandCatalog(context.Background(), &promptcommandpb.GetCatalogRequest{})
	if err != nil {
		t.Fatalf("GetPromptCommandCatalog: %v", err)
	}
	for _, command := range response.Commands {
		if command.Name == "prompt:only_worktree" && command.Preview == "worktree" {
			return
		}
	}
	t.Fatalf("worktree catalog = %+v, want prompt:only_worktree", response.Commands)
}

func TestPromptCommandEffectiveWorkspaceResolverUsesSuppliedWorkspace(t *testing.T) {
	persistenceRoot := t.TempDir()
	workspace := t.TempDir()
	writeCorePromptFixture(t, workspace, "pre_session", "effective workspace body")

	resolver := promptCommandEffectiveWorkspaceResolver{persistenceRoot: persistenceRoot}
	got, err := resolver.ResolvePromptCommandForWorkspace(context.Background(), workspace, "prompt:pre_session", "")
	if err != nil {
		t.Fatalf("ResolvePromptCommandForWorkspace: %v", err)
	}
	if got != "effective workspace body" {
		t.Fatalf("resolved body = %q, want effective workspace body", got)
	}
}

func TestPromptCommandWorkspaceRootUsesCurrentWorktree(t *testing.T) {
	target := &worktreepb.SessionExecutionTarget{
		Worktree: &worktreepb.SessionExecutionWorktreeTarget{Root: "/worktrees/feature"},
	}
	got, err := clientui.SessionExecutionWorkspaceRoot(target, "/workspace/main")
	if err != nil {
		t.Fatalf("SessionExecutionWorkspaceRoot: %v", err)
	}
	if got != "/worktrees/feature" {
		t.Fatalf("workspace root = %q, want current worktree root", got)
	}
}

func TestPromptCommandCatalogRedactsFilesystemCauseAtClientBoundary(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	binding, err := metadata.RegisterBinding(context.Background(), resolved.Config.PersistenceRoot, workspace)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	promptRoot := filepath.Join(workspace, ".kent", "prompts")
	if err := os.MkdirAll(promptRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	missingTarget := filepath.Join(promptRoot, "missing-target.md")
	if err := os.Symlink(missingTarget, filepath.Join(promptRoot, "broken.md")); err != nil {
		t.Fatal(err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())
	client, err := appCore.PromptCommandCatalogClientForProjectWorkspace(context.Background(), binding.ProjectID, workspace)
	if err != nil {
		t.Fatalf("PromptCommandCatalogClientForProjectWorkspace: %v", err)
	}
	response, err := client.GetPromptCommandCatalog(context.Background(), &promptcommandpb.GetCatalogRequest{})
	if err != nil {
		t.Fatalf("catalog broken symlink: %v", err)
	}
	for _, entry := range response.Commands {
		if entry.Name == "prompt:broken" {
			t.Fatalf("catalog included broken symlink: %+v", response.Commands)
		}
	}
}

func writeCorePromptFixture(t *testing.T, workspace, name, content string) {
	t.Helper()
	root := filepath.Join(workspace, ".kent", "prompts")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name+".md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNewProvidesRegistrationSafeClientsForUnregisteredWorkspace(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())

	if got := appCore.ProjectID(); got != "" {
		t.Fatalf("project id = %q, want empty for unregistered workspace", got)
	}
	if appCore.SessionLaunchClient() == nil {
		t.Fatal("expected session launch client stub")
	}
	if appCore.RunPromptClient() == nil {
		t.Fatal("expected run prompt client stub")
	}
	_, err = appCore.SessionLaunchClient().PlanSession(context.Background(), &sessionlaunchpb.SessionPlanRequest{})
	if !errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
		t.Fatalf("PlanSession error = %v, want ErrWorkspaceNotRegistered", err)
	}
	_, err = appCore.RunPromptClient().RunPrompt(context.Background(), serverapi.RunPromptRequest{}, nil)
	if !errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
		t.Fatalf("RunPrompt error = %v, want ErrWorkspaceNotRegistered", err)
	}
	if _, err := appCore.ProjectViewClient().ListProjects(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("ListProjects via core client: %v", err)
	}
}

func TestNewRejectsSecondCoreForSamePersistenceRoot(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	newCoreTestApp(t, resolved.Config, auth.EmptyState())
	generatedSkillsRoot := filepath.Join(home, brand.ConfigDirName, ".generated", "skills")
	if entries, err := os.ReadDir(generatedSkillsRoot); err != nil {
		t.Fatalf("expected first core to seed generated skills: %v", err)
	} else if len(entries) == 0 {
		t.Fatal("expected first core to seed at least one generated skill")
	}

	authSupportB, err := serverbootstrap.BuildAuthSupport(auth.NewMemoryStore(auth.EmptyState()), nil, nil)
	if err != nil {
		t.Fatalf("BuildAuthSupport B: %v", err)
	}
	backgroundB, err := serverbootstrap.BuildShellManager(resolved.Config)
	if err != nil {
		t.Fatalf("BuildShellManager B: %v", err)
	}
	t.Cleanup(func() { _ = backgroundB.Close() })

	_, err = New(resolved.Config, authSupportB, backgroundB)
	if !errors.Is(err, ErrPersistenceRootBusy) {
		t.Fatalf("New second error = %v, want ErrPersistenceRootBusy", err)
	}
}

func TestSessionLaunchClientForProjectWorkspaceRejectsMissingProject(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())

	_, err = appCore.SessionLaunchClientForProjectWorkspace(context.Background(), "project-missing", workspace)
	if !errors.Is(err, serverapi.ErrProjectNotFound) {
		t.Fatalf("SessionLaunchClientForProjectWorkspace error = %v, want ErrProjectNotFound", err)
	}
}

func TestSessionLaunchClientForProjectWorkspaceRejectsUnavailableProjectRoot(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)

	resolvedA, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceA})
	if err != nil {
		t.Fatalf("ResolveConfig A: %v", err)
	}
	binding, err := metadata.RegisterBinding(context.Background(), resolvedA.Config.PersistenceRoot, resolvedA.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	missingRoot := filepath.Join(t.TempDir(), "workspace-moved")
	if err := os.Rename(workspaceA, missingRoot); err != nil {
		t.Fatalf("Rename workspaceA: %v", err)
	}

	resolvedB, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceB})
	if err != nil {
		t.Fatalf("ResolveConfig B: %v", err)
	}
	appCore := newCoreTestApp(t, resolvedB.Config, auth.EmptyState())

	_, err = appCore.SessionLaunchClientForProjectWorkspace(context.Background(), binding.ProjectID, workspaceB)
	if !errors.Is(err, serverapi.ErrProjectUnavailable) {
		t.Fatalf("SessionLaunchClientForProjectWorkspace error = %v, want ErrProjectUnavailable", err)
	}
	unavailable, ok := serverapi.AsProjectUnavailable(err)
	if !ok {
		t.Fatalf("expected ProjectUnavailableError, got %v", err)
	}
	if unavailable.ProjectID != binding.ProjectID || unavailable.Availability != clientui.ProjectAvailabilityMissing {
		t.Fatalf("unexpected unavailable project: %+v", unavailable)
	}
}

func TestSessionLaunchClientForProjectWorkspaceUsesWorkspaceLocalConfig(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(workspaceB, brand.ConfigDirName), 0o755); err != nil {
		t.Fatalf("create workspace config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceB, brand.ConfigDirName, "config.toml"), []byte("model = \"workspace-b-model\"\nthinking_level = \"high\"\n"), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	resolvedA, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceA})
	if err != nil {
		t.Fatalf("ResolveConfig A: %v", err)
	}
	resolvedB, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceB})
	if err != nil {
		t.Fatalf("ResolveConfig B: %v", err)
	}
	bindingB, err := metadata.RegisterBinding(context.Background(), resolvedB.Config.PersistenceRoot, resolvedB.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding B: %v", err)
	}
	appCore := newCoreTestApp(t, resolvedA.Config, auth.EmptyState())

	client, err := appCore.SessionLaunchClientForProjectWorkspace(context.Background(), bindingB.ProjectID, workspaceB)
	if err != nil {
		t.Fatalf("SessionLaunchClientForProjectWorkspace: %v", err)
	}
	intent, err := protoapi.SessionLaunchIntentToProto(
		serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	)
	if err != nil {
		t.Fatalf("convert Session launch intent: %v", err)
	}
	plan, err := client.PlanSession(context.Background(), &sessionlaunchpb.SessionPlanRequest{
		Mode:   sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
		Intent: intent,
	})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	if plan.Plan.ActiveSettings.Model != "workspace-b-model" || plan.Plan.ActiveSettings.ThinkingLevel != "high" {
		t.Fatalf("unexpected active settings: %+v", plan.Plan.ActiveSettings)
	}
}

func createCoreSettingsSession(
	t *testing.T,
	appCore *Core,
	cfg brand.App,
	projectID string,
) *session.Store {
	t.Helper()
	store, err := session.Create(
		filepath.Join(cfg.PersistenceRoot, "projects", projectID, "sessions"),
		"settings",
		cfg.WorkspaceRoot,
		sessioncontract.SessionCategoryMain,
		appCore.MetadataStore().AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	return store
}

func TestChatSettingsReadUsesAuthoritativePersistenceRoot(t *testing.T) {
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	t.Setenv(brand.PersistenceRootEnvName, t.TempDir())
	if err := os.WriteFile(
		filepath.Join(persistenceRoot, "config.toml"),
		[]byte("[subagents.worker]\nthinking_level = \"high\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write custom-root config: %v", err)
	}
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot: workspace,
		LoadOptions:   brand.LoadOptions{ConfigRoot: persistenceRoot},
	})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	binding, err := metadata.RegisterBinding(t.Context(), persistenceRoot, workspace)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())
	store := createCoreSettingsSession(t, appCore, resolved.Config, binding.ProjectID)
	if err := store.SetContinuationContext(session.ContinuationContext{
		AgentRole: textutil.Value("worker"),
	}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	response, err := appCore.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: store.Meta().SessionID}},
	})
	if err != nil {
		t.Fatalf("ReadChatSettings: %v", err)
	}
	if response.GetSession().Settings.SelectedAgent.Thinking != "high" {
		t.Fatalf("worker Thinking = %q, want custom-root value high", response.GetSession().Settings.SelectedAgent.Thinking)
	}
}

func TestChatSettingsMaterializedReadUsesDetachedSessionSnapshotWithoutRebinding(t *testing.T) {
	workspace := t.TempDir()
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot: workspace,
		LoadOptions:   brand.LoadOptions{ConfigRoot: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	binding, err := metadata.RegisterBinding(t.Context(), resolved.Config.PersistenceRoot, workspace)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())
	store, err := session.Create(
		filepath.Join(resolved.Config.PersistenceRoot, "projects", binding.ProjectID, "sessions"),
		"detached",
		resolved.Config.WorkspaceRoot,
		sessioncontract.SessionCategoryMain,
		appCore.MetadataStore().AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	if err := appCore.MetadataStore().UpdateSessionExecutionTarget(
		t.Context(),
		metadata.SessionExecutionTargetUpdate{SessionID: store.Meta().SessionID},
	); err != nil {
		t.Fatalf("detach Session execution target: %v", err)
	}
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}

	response, err := appCore.ChatSettingsClient().ReadChatSettings(
		t.Context(),
		&chatsettingspb.ReadRequest{
			Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: sessionID.String()}},
		},
	)
	if err != nil {
		t.Fatalf("ReadChatSettings detached Session: %v", err)
	}
	if response.GetSession() == nil || response.GetSession().Session.SessionId != sessionID.String() {
		t.Fatalf("detached response = %+v", response)
	}
	executionTarget, err := appCore.MetadataStore().ResolveSessionExecutionTarget(
		t.Context(),
		store.Meta().SessionID,
	)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget: %v", err)
	}
	if executionTarget.WorkspaceId != nil {
		t.Fatalf("detached read rebound workspace %q", executionTarget.GetWorkspaceId())
	}
}

func TestChatSettingsUsesPersistedPromptFacingEndpoint(t *testing.T) {
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(persistenceRoot, "config.toml"), []byte(
		"model = \"gpt-5.6-sol\"\nopenai_base_url = \"https://api.openai.com/v1\"\npriority_request_mode = true\n[subagents.worker]\nthinking_level = \"high\"\n",
	), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot: workspace,
		LoadOptions:   brand.LoadOptions{ConfigRoot: persistenceRoot},
	})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	binding, err := metadata.RegisterBinding(t.Context(), persistenceRoot, workspace)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())
	store := createCoreSettingsSession(t, appCore, resolved.Config, binding.ProjectID)
	if err := store.SetContinuationContext(session.ContinuationContext{
		OpenAIBaseURL: textutil.Value("https://compatible.example/v1"),
	}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}

	response, err := appCore.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: store.Meta().SessionID}},
	})
	if err != nil {
		t.Fatalf("ReadChatSettings: %v", err)
	}
	if response.GetSession().Settings.Fast != nil {
		t.Fatalf("Fast = %+v, want unavailable for compatible endpoint", response.GetSession().Settings.Fast)
	}
	mutated, err := appCore.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
		Session: &chatsettingspb.SessionTarget{SessionId: store.Meta().SessionID},
		Operation: &chatsettingspb.MutationOperation{
			Operation: &chatsettingspb.MutationOperation_FastEnabled{FastEnabled: true},
		},
	})
	if err != nil {
		t.Fatalf("MutateChatSettings: %v", err)
	}
	if mutated.Result.GetRejected().GetReason() != chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_FAST_UNAVAILABLE ||
		mutated.Settings.Fast != nil {
		t.Fatalf("Fast mutation = %+v, want unavailable rejection", mutated)
	}
	switched, err := appCore.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
		Session: &chatsettingspb.SessionTarget{SessionId: store.Meta().SessionID},
		Operation: &chatsettingspb.MutationOperation{
			Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: "worker"},
		},
	})
	if err != nil {
		t.Fatalf("switch Agent: %v", err)
	}
	if switched.Result.GetApplied() == nil || switched.Settings.SelectedAgent.Role != "worker" ||
		switched.Settings.Fast == nil || !switched.Settings.Fast.Value {
		t.Fatalf("Agent switch = %+v, want configured worker with Fast enabled", switched)
	}
}

func TestChatSettingsReadUsesLockedPromptFacingModelCapabilities(t *testing.T) {
	workspace := t.TempDir()
	persistenceRoot := t.TempDir()
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot: workspace,
		LoadOptions:   brand.LoadOptions{ConfigRoot: persistenceRoot},
	})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	resolved.Config.Settings.Model = "gpt-5.6-sol"
	binding, err := metadata.RegisterBinding(t.Context(), persistenceRoot, workspace)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())
	store := createCoreSettingsSession(t, appCore, resolved.Config, binding.ProjectID)
	if err := store.MarkModelDispatchLocked(session.LockedContract{
		Model: "gpt-5",
		ProviderContract: session.LockedProviderCapabilities{
			ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true,
		},
	}); err != nil {
		t.Fatalf("MarkModelDispatchLocked: %v", err)
	}

	response, err := appCore.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: store.Meta().SessionID}},
	})
	if err != nil {
		t.Fatalf("ReadChatSettings: %v", err)
	}
	settings := response.GetSession().Settings
	if settings.SelectedAgent.Model != "gpt-5" || settings.Thinking == nil || slices.Contains(settings.Thinking.Values, "ultra") {
		t.Fatalf("locked gpt-5 settings = %+v, want locked model Thinking values without ultra", settings)
	}
}

func TestSessionLaunchClientForProjectWorkspaceRejectsInaccessibleProjectRoot(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(t.TempDir(), "blocked-parent")
	workspaceA := filepath.Join(parent, "workspace-a")
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(workspaceA, 0o755); err != nil {
		t.Fatalf("create workspace A: %v", err)
	}

	resolvedA, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceA})
	if err != nil {
		t.Fatalf("ResolveConfig A: %v", err)
	}
	binding, err := metadata.RegisterBinding(context.Background(), resolvedA.Config.PersistenceRoot, resolvedA.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatalf("make workspace parent inaccessible: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(parent, 0o755); err != nil {
			t.Fatalf("restore workspace parent permissions: %v", err)
		}
	})
	if _, err := os.Stat(workspaceA); err == nil {
		t.Skip("filesystem permissions do not prevent stat for current user")
	}
	metadataStore, err := metadata.Open(resolvedA.Config.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	overview, err := metadataStore.GetProjectOverview(context.Background(), binding.ProjectID)
	if err != nil {
		t.Fatalf("GetProjectOverview: %v", err)
	}
	if overview.Project.RootPath != binding.CanonicalRoot || overview.Project.Availability != clientui.ProjectAvailabilityInaccessible {
		t.Fatalf("overview = %+v, want inaccessible root %q", overview.Project, binding.CanonicalRoot)
	}

	resolvedB, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceB})
	if err != nil {
		t.Fatalf("ResolveConfig B: %v", err)
	}
	appCore := newCoreTestApp(t, resolvedB.Config, auth.EmptyState())

	_, err = appCore.SessionLaunchClientForProjectWorkspace(context.Background(), binding.ProjectID, workspaceB)
	if !errors.Is(err, serverapi.ErrProjectUnavailable) {
		t.Fatalf("SessionLaunchClientForProjectWorkspace error = %v, want ErrProjectUnavailable", err)
	}
	unavailable, ok := serverapi.AsProjectUnavailable(err)
	if !ok {
		t.Fatalf("expected ProjectUnavailableError, got %v", err)
	}
	if unavailable.ProjectID != binding.ProjectID || unavailable.Availability != clientui.ProjectAvailabilityInaccessible {
		t.Fatalf("unexpected unavailable project: %+v", unavailable)
	}
}

func newCoreTestApp(t *testing.T, cfg brand.App, state auth.State) *Core {
	return newCoreTestAppWithLoadOptions(t, cfg, state, brand.LoadOptions{})
}

func newCoreTestAppWithLoadOptions(t *testing.T, cfg brand.App, state auth.State, loadOptions brand.LoadOptions) *Core {
	t.Helper()
	return newCoreTestAppWithOptions(t, cfg, state, Options{WorkspaceConfigLoadOptions: loadOptions})
}

func newCoreTestAppWithOptions(t *testing.T, cfg brand.App, state auth.State, options Options) *Core {
	t.Helper()
	authSupport, err := serverbootstrap.BuildAuthSupport(auth.NewMemoryStore(state), nil, nil)
	if err != nil {
		t.Fatalf("BuildAuthSupport: %v", err)
	}
	background, err := serverbootstrap.BuildShellManager(cfg)
	if err != nil {
		t.Fatalf("BuildShellManager: %v", err)
	}
	t.Cleanup(func() { _ = background.Close() })
	appCore, err := NewWithContextOptions(t.Context(), cfg, authSupport, background, options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = appCore.Close() })
	return appCore
}
