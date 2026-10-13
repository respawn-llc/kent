package runprompt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	modelstub "core/internal/testharness/pty/blackbox"
	"core/internal/testharness/requestfixture"
	"core/internal/testharness/testsetup"
	"core/server/attentionnotify"
	"core/server/auth"
	"core/server/launch"
	"core/server/llm"
	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/registry"
	"core/server/runtime"
	"core/server/runtimecontrol"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/sessionlaunch"
	"core/server/sessionruntime"
	askquestion "core/server/tools"
	shelltool "core/server/tools/shell"
	"core/shared/apicontract"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/sessionenv"
	"core/shared/textutil"

	"core/shared/toolspec"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type recordingPromptHistoryStore struct {
	entries []metadata.PromptHistoryEntry
}

func (s *recordingPromptHistoryStore) RecordPromptHistoryEntry(_ context.Context, entry metadata.PromptHistoryEntry) (metadata.PromptHistoryRecord, error) {
	s.entries = append(s.entries, entry)
	return metadata.PromptHistoryRecord{}, nil
}

type gatedPromptHistoryStore struct {
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
}

func (s *gatedPromptHistoryStore) RecordPromptHistoryEntry(ctx context.Context, _ metadata.PromptHistoryEntry) (metadata.PromptHistoryRecord, error) {
	s.startedOnce.Do(func() { close(s.started) })
	select {
	case <-s.release:
		return metadata.PromptHistoryRecord{}, nil
	case <-ctx.Done():
		return metadata.PromptHistoryRecord{}, ctx.Err()
	}
}

func (s *gatedPromptHistoryStore) Release() {
	s.releaseOnce.Do(func() { close(s.release) })
}

type failingPromptHistoryStore struct {
	err error
}

func (s failingPromptHistoryStore) RecordPromptHistoryEntry(context.Context, metadata.PromptHistoryEntry) (metadata.PromptHistoryRecord, error) {
	return metadata.PromptHistoryRecord{}, s.err
}

type fixedSessionExecutionTargetResolver struct {
	*metadata.Store
	target *worktreepb.SessionExecutionTarget
}

func (r fixedSessionExecutionTargetResolver) ResolveSessionExecutionTarget(context.Context, string) (*worktreepb.SessionExecutionTarget, error) {
	return r.target, nil
}

type headlessLaunchArtifactSnapshot struct {
	SessionIDs       []string
	SessionArtifacts []string
	PersistenceFiles []string
	WorktreeFiles    []string
	WorktreeRecords  []metadata.WorktreeRecord
}

func snapshotHeadlessLaunchArtifacts(t *testing.T, ctx context.Context, meta *metadata.Store, projectID string, workspaceID string, containerDir string, persistenceRoot string, worktreeRoot string) headlessLaunchArtifactSnapshot {
	t.Helper()
	sessionIDs, err := meta.ListProjectSessionIDs(ctx, projectID)
	if err != nil {
		t.Fatalf("ListProjectSessionIDs snapshot: %v", err)
	}
	worktreeRecords, err := meta.ListWorktreeRecordsByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatalf("ListWorktreeRecordsByWorkspaceID snapshot: %v", err)
	}
	return headlessLaunchArtifactSnapshot{
		SessionIDs:       sessionIDs,
		SessionArtifacts: snapshotArtifactPaths(t, containerDir),
		PersistenceFiles: snapshotArtifactPaths(t, persistenceRoot),
		WorktreeFiles:    snapshotArtifactPaths(t, worktreeRoot),
		WorktreeRecords:  worktreeRecords,
	}
}

func snapshotArtifactPaths(t *testing.T, root string) []string {
	t.Helper()
	paths := make([]string, 0)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return paths
	} else if err != nil {
		t.Fatalf("Stat %s: %v", root, err)
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		paths = append(paths, kind+":"+filepath.ToSlash(relative))
		return nil
	}); err != nil {
		t.Fatalf("WalkDir %s: %v", root, err)
	}
	sort.Strings(paths)
	return paths
}

func TestRunPromptProgressFromRuntimeEventPublishesUserVisibleEvents(t *testing.T) {
	tests := []struct {
		name  string
		event runtime.Event
		want  *runpromptpb.ProgressEvent
	}{
		{
			name: "assistant commentary",
			event: runtime.Event{
				Kind: runtime.EventAssistantMessage,
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Phase:   textutil.Value(llm.MessagePhaseCommentary),
					Content: textutil.Value("I am checking the runtime."),
				},
			},
			want: &runpromptpb.ProgressEvent{Payload: &runpromptpb.ProgressEvent_AssistantMessage{
				AssistantMessage: &runpromptpb.AssistantMessage{
					Phase: runpromptpb.MessagePhase_MESSAGE_PHASE_COMMENTARY, Content: "I am checking the runtime.",
				},
			}},
		},
		{
			name:  "compaction started",
			event: runtime.Event{Kind: runtime.EventCompactionStarted},
			want:  &runpromptpb.ProgressEvent{Payload: &runpromptpb.ProgressEvent_CompactionStarted{CompactionStarted: &emptypb.Empty{}}},
		},
		{
			name: "compaction failed",
			event: runtime.Event{
				Kind:       runtime.EventCompactionFailed,
				Compaction: &runtime.CompactionStatus{Error: "provider rejected compaction"},
			},
			want: &runpromptpb.ProgressEvent{Payload: &runpromptpb.ProgressEvent_CompactionFailed{
				CompactionFailed: &runpromptpb.ProgressFailure{Error: textutil.Value("provider rejected compaction")},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := RunPromptProgressFromRuntimeEvent(test.event)
			if !ok {
				t.Fatal("expected user-visible progress event")
			}
			if !proto.Equal(got, test.want) {
				t.Fatalf("progress = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestRunPromptProgressFromRuntimeEventDropsOperationalSpam(t *testing.T) {
	for _, event := range []runtime.Event{
		{Kind: runtime.EventToolCallStarted},
		{Kind: runtime.EventToolCallCompleted},
		{Kind: runtime.EventRuntimeActivityChanged},
		{
			Kind: runtime.EventQueuedUserMessageStatus,
			QueuedUserMessageStatus: &runtime.QueuedUserMessageStatusEvent{
				Status: runtime.QueuedUserMessageSubmitted,
			},
		},
		{
			Kind: runtime.EventQueuedUserMessageStatus,
			QueuedUserMessageStatus: &runtime.QueuedUserMessageStatusEvent{
				Status: runtime.QueuedUserMessageAccepted,
				Text:   "received Steering must not be echoed",
			},
		},
	} {
		if got, ok := RunPromptProgressFromRuntimeEvent(event); ok {
			t.Fatalf("unexpected progress event for %q: %+v", event.Kind, got)
		}
	}
}

func TestInProcessRunPromptClientRejectsInvalidRequestsBeforeLaunch(t *testing.T) {
	reservedRole := "self"
	validIntent := requestfixture.LaunchIntent(t, serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))
	tests := []struct {
		name string
		req  *runpromptpb.Request
	}{
		{name: "blank prompt", req: &runpromptpb.Request{Intent: validIntent, Prompt: " \n "}},
		{name: "invalid intent", req: &runpromptpb.Request{Prompt: "work"}},
		{name: "reserved role", req: &runpromptpb.Request{
			Intent:    validIntent,
			Prompt:    "work",
			Overrides: &sessionlaunchpb.RunPromptOverrides{AgentRole: &reservedRole},
		}},
	}

	client := NewInProcessRunPromptClient(HeadlessBootstrap{})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := client.RunPrompt(context.Background(), test.req, nil); err == nil {
				t.Fatal("RunPrompt succeeded before validating the request")
			}
		})
	}
}

func newTestHeadlessSessionLaunch(
	t *testing.T,
	cfg config.App,
	containerDir string,
	authManager *auth.Manager,
	persistences ...*sessiontest.Persistence,
) *sessionlaunch.Service {
	persistence := sessiontest.NewPersistence()
	if len(persistences) > 0 && persistences[0] != nil {
		persistence = persistences[0]
	}
	return sessionlaunch.NewService(launch.Planner{
		Config:            cfg,
		ContainerDir:      containerDir,
		StoreOptions:      persistence.Options(),
		PersistedSessions: persistence,
		SessionProjects:   fixedSessionProjectResolver{}, ManagedWorktreeRoots: fixedSessionProjectResolver{},
		ExecutionTargets: fixedSessionExecutionTargetResolver{Store: testsetup.OpenStore(t, cfg.PersistenceRoot), target: &worktreepb.SessionExecutionTarget{
			WorkspaceRoot:    cfg.WorkspaceRoot,
			CwdRelpath:       ".",
			EffectiveWorkdir: cfg.WorkspaceRoot,
		}},
	}, sessionlaunch.ChatSettingsOwner{}, nil)
}

type fixedSessionProjectResolver struct{}

func (fixedSessionProjectResolver) ResolveSessionProjectID(context.Context, string) (string, error) {
	return "test-project", nil
}

func (fixedSessionProjectResolver) ListManagedWorktreeRoots(context.Context) ([]string, error) {
	return nil, nil
}

func newTestHeadlessRuntimeAuthority(root string, authManager *auth.Manager, background *shelltool.Manager, storeOptions ...session.StoreOption) *sessionruntime.Authority {
	return sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot: root,
		AuthManager:     authManager,
		Background:      background,
		StoreOptions:    storeOptions,
	})
}

func newObservedTestHeadlessRuntimeAuthority(
	t *testing.T,
	root string,
	sessionStore *session.Store,
	metadataStore *metadata.Store,
	authManager *auth.Manager,
	background *shelltool.Manager,
	storeOptions ...session.StoreOption,
) (*sessionruntime.Authority, *runtimecontrol.Service) {
	observed := newObservedTestHeadlessRuntime(t, root, sessionStore, metadataStore, authManager, background, storeOptions...)
	return observed.authority, observed.observer
}

type emptyRunPromptWorkspaceMembership struct{}

func (emptyRunPromptWorkspaceMembership) FindContainingProjectWorkspace(
	context.Context,
	string,
	string,
) (*sqlitegen.Workspace, error) {
	return nil, nil
}

type observedHeadlessTestRuntime struct {
	authority         *sessionruntime.Authority
	observer          *runtimecontrol.Service
	metadataStore     *metadata.Store
	initialPromptRead <-chan struct{}
}

func newObservedTestHeadlessRuntime(
	t *testing.T,
	root string,
	sessionStore *session.Store,
	metadataStore *metadata.Store,
	authManager *auth.Manager,
	background *shelltool.Manager,
	storeOptions ...session.StoreOption,
) observedHeadlessTestRuntime {
	if metadataStore == nil {
		metadataRoot := root
		if sessionStore != nil {
			metadataRoot = filepath.Dir(sessionStore.Dir())
		}
		metadataStore = testsetup.OpenStore(t, metadataRoot)
	}
	if sessionStore != nil {
		testsetup.ImportSessionProjectWorkspaceContext(t, metadataStore, sessionStore)
	}
	runtimeRegistry := registry.NewRuntimeRegistry(metadataStore).WithAttentionNotifications(
		attentionnotify.NewBroker(),
		testsetup.SessionNavigationBinding,
	)
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot:               root,
		AuthManager:                   authManager,
		Background:                    background,
		StoreOptions:                  storeOptions,
		WorkspaceMembership:           emptyRunPromptWorkspaceMembership{},
		ProjectWorkspaceContextReader: metadataStore,
		ResourceLifecycle:             runtimeRegistry,
		PromptFeed:                    runtimeRegistry,
		EventFeed: func(ref runtimeids.SessionResourceRef, event runtime.Event) {
			if err := runtimeRegistry.PublishAuthorityRuntimeEvent(ref, event); err != nil {
				panic(err)
			}
		},
	})
	pendingPrompts := &observedPendingPromptSource{
		registry:    runtimeRegistry,
		initialRead: make(chan struct{}),
	}
	observer := runtimecontrol.NewService(authority, metadataStore).
		WithLiveWatchPromptSources(pendingPrompts, runtimeRegistry)
	return observedHeadlessTestRuntime{
		authority:         authority,
		observer:          observer,
		metadataStore:     metadataStore,
		initialPromptRead: pendingPrompts.initialRead,
	}
}

func TestHeadlessRuntimeUsesServerManagedWorktreeNamespace(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	serverManagedBase := filepath.Join(root, "server-worktrees")
	projectManagedBase := filepath.Join(root, "project-worktrees")
	currentWorktree := filepath.Join(serverManagedBase, "current")
	workdir := filepath.Join(currentWorktree, "pkg")
	for _, dir := range []string{workspace, workdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}
	persistence := sessiontest.NewPersistence()
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	store, err := session.Create(containerDir, "workspace-a", workspace, sessioncontract.SessionCategorySubagent, persistence.Options()...)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	descriptor, err := session.NewScopedOpenSessionDescriptor(sessionID, containerDir)
	if err != nil {
		t.Fatalf("NewScopedOpenSessionDescriptor: %v", err)
	}
	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	authority := newTestHeadlessRuntimeAuthority(root, authManager, nil, persistence.Options()...)
	launcher := &headlessPromptLauncher{boot: HeadlessBootstrap{
		RuntimeAuthority:       authority,
		ManagedWorktreeBaseDir: serverManagedBase,
	}}
	runtimePlan, err := launcher.prepareRuntime(context.Background(), launch.SessionPlan{
		Descriptor: descriptor,
		ActiveSettings: testsetup.WriteProviderSettings(t, root, testsetup.WithResponsesProvider(config.Settings{
			Model: "gpt-6-sol",
			Shell: config.ShellSettings{PostprocessingMode: config.ShellPostprocessingModeBuiltin},
		}, "http://127.0.0.1:1")),
		BaseSettings: config.Settings{
			Worktrees: config.WorktreeSettings{BaseDir: projectManagedBase},
		},
		ExecutionTarget: &worktreepb.SessionExecutionTarget{
			WorkspaceRoot:    workspace,
			EffectiveWorkdir: workdir,
			Worktree: &worktreepb.SessionExecutionWorktreeTarget{
				Root: currentWorktree,
			},
		},
		ProjectID:            "project-a",
		ManagedWorktreeRoots: []string{currentWorktree},
	}, nil, nil)
	if err != nil {
		t.Fatalf("prepareRuntime: %v", err)
	}
	if err := runtimePlan.CloseWithFailure(true); err != nil {
		t.Fatalf("CloseWithFailure: %v", err)
	}
}

type selectedRunPromptFixture struct {
	store             *session.Store
	authority         *sessionruntime.Authority
	initialPromptRead <-chan struct{}
	client            apicontract.RunPromptService
}

type observedPendingPromptSource struct {
	registry    *registry.RuntimeRegistry
	initialRead chan struct{}
	once        sync.Once
}

func (s *observedPendingPromptSource) ListPendingPrompts(sessionID string) []registry.PendingPromptSnapshot {
	prompts := s.registry.ListPendingPrompts(sessionID)
	if len(prompts) == 0 {
		s.once.Do(func() { close(s.initialRead) })
	}
	return prompts
}

func newSelectedRunPromptFixture(
	t *testing.T,
	providerURL string,
	history promptHistoryStore,
	enabledTools ...toolspec.ID,
) selectedRunPromptFixture {
	t.Helper()
	root := t.TempDir()
	workspace := t.TempDir()
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(containerDir, "workspace-a", workspace, sessioncontract.SessionCategorySubagent, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)
	observed := newObservedTestHeadlessRuntime(t, root, store, nil, authManager, nil, persistence.Options()...)

	if len(enabledTools) == 0 {
		enabledTools = []toolspec.ID{toolspec.ToolAskQuestion}
	}
	enabled := make(map[toolspec.ID]bool, len(enabledTools))
	for _, tool := range enabledTools {
		enabled[tool] = true
	}
	cfg := config.App{
		WorkspaceRoot:   store.Meta().WorkspaceRoot,
		PersistenceRoot: root,
		Settings: testsetup.WriteProviderSettings(t, root, testsetup.WithResponsesProvider(config.Settings{
			Model:         "gpt-6-sol",
			ThinkingLevel: "medium",
			EnabledTools:  enabled,
			Shell:         config.ShellSettings{PostprocessingMode: config.ShellPostprocessingModeBuiltin},
		}, providerURL)),
	}
	return selectedRunPromptFixture{
		store:             store,
		authority:         observed.authority,
		initialPromptRead: observed.initialPromptRead,
		client: NewInProcessRunPromptClient(HeadlessBootstrap{
			SessionLaunch:    newTestHeadlessSessionLaunch(t, cfg, containerDir, authManager, persistence),
			RuntimeAuthority: observed.authority,
			PromptHistory:    history,
			LiveRunObserver:  observed.observer,
		}),
	}
}

func TestHeadlessSiblingWorkspacePatchUsesProjectBoundary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	root := t.TempDir()
	workspace := t.TempDir()
	sibling := t.TempDir()
	meta := testsetup.OpenStore(t, root)
	binding, err := meta.RegisterWorkspaceBinding(ctx, workspace)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	if _, err := meta.AttachWorkspaceToProject(ctx, binding.ProjectID, sibling); err != nil {
		t.Fatalf("AttachWorkspaceToProject: %v", err)
	}
	containerDir := filepath.Join(root, "projects", binding.ProjectID, "sessions")
	store, err := session.Create(containerDir, filepath.Base(containerDir), workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}

	target := filepath.Join(sibling, "headless.txt")
	patchArgs, err := json.Marshal(map[string]any{"patch": strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: " + target,
		"+headless sibling",
		"*** End Patch",
		"",
	}, "\n")})
	if err != nil {
		t.Fatalf("marshal patch arguments: %v", err)
	}
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch providerCalls.Add(1) {
		case 1:
			writeRunPromptFunctionCallResponse(w, "fc-sibling-patch", "call-sibling-patch", toolspec.ToolPatch, patchArgs)
		case 2:
			modelstub.WriteCompletedResponseStream(w, "done", 1, 1)
		default:
			t.Errorf("unexpected provider request %d", providerCalls.Load())
		}
	}))
	defer provider.Close()

	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	cfg := config.App{
		WorkspaceRoot:   workspace,
		PersistenceRoot: root,
		Settings: testsetup.WriteProviderSettings(t, root, testsetup.WithResponsesProvider(config.Settings{
			Model:        "gpt-6-sol",
			EnabledTools: map[toolspec.ID]bool{toolspec.ToolPatch: true},
			Reviewer:     config.ReviewerSettings{Frequency: "off"},
			Shell:        config.ShellSettings{PostprocessingMode: config.ShellPostprocessingModeBuiltin},
		}, provider.URL)),
	}
	authority, liveRunObserver := newObservedTestHeadlessRuntimeAuthority(t, root, store, meta, authManager, nil, meta.AuthoritativeSessionStoreOptions()...)
	client := NewInProcessRunPromptClient(HeadlessBootstrap{
		SessionLaunch: sessionlaunch.NewService(launch.Planner{
			Config:            cfg,
			ContainerDir:      containerDir,
			StoreOptions:      meta.AuthoritativeSessionStoreOptions(),
			PersistedSessions: meta,
			CallerSessions:    meta,
			ExecutionTargets:  meta,
			SessionProjects:   meta, ManagedWorktreeRoots: meta,
		}, sessionlaunch.ChatSettingsOwner{}, meta,
		),
		RuntimeAuthority: authority,
		LiveRunObserver:  liveRunObserver,
	})
	sessionID := mustRunPromptSessionID(t, store.Meta().SessionID)
	response, err := client.RunPrompt(ctx, requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(sessionID),
		Prompt: "write to the sibling Workspace",
	}), nil)
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if response.GetFinalAnswer() != "done" {
		t.Fatalf("RunPrompt result = %q, want done", response.GetFinalAnswer())
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "headless sibling\n" {
		t.Fatalf("sibling patch data = %q, error = %v", data, err)
	}
}

func TestHeadlessChildUsesInheritedExecutionTargetAfterWorktreeReminderWasConsumed(t *testing.T) {
	t.Setenv("KENT_REVIEWER_FREQUENCY", "off")
	ctx := context.Background()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	managedBase := filepath.Join(root, "worktrees")
	worktreeRoot := filepath.Join(managedBase, "task")
	worktreeSubdir := filepath.Join(worktreeRoot, "pkg")
	for _, dir := range []string{workspace, worktreeSubdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}
	meta := testsetup.OpenStore(t, root)
	binding, err := meta.RegisterWorkspaceBinding(ctx, workspace)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := filepath.Join(root, "projects", binding.ProjectID, "sessions")
	parent, err := session.Create(
		containerDir,
		filepath.Base(containerDir),
		workspace,
		sessioncontract.SessionCategoryMain,
		meta.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create parent: %v", err)
	}
	if err := parent.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable parent: %v", err)
	}
	canonicalWorktreeRoot, err := config.CanonicalWorkspaceRoot(worktreeRoot)
	if err != nil {
		t.Fatalf("CanonicalWorkspaceRoot worktree: %v", err)
	}
	canonicalWorktreeSubdir, err := config.CanonicalWorkspaceRoot(worktreeSubdir)
	if err != nil {
		t.Fatalf("CanonicalWorkspaceRoot worktree subdir: %v", err)
	}
	if err := meta.UpsertWorktreeRecord(ctx, metadata.WorktreeRecord{
		ID:              "worktree-task",
		WorkspaceID:     binding.WorkspaceID,
		CanonicalRoot:   canonicalWorktreeRoot,
		DisplayName:     "task",
		Availability:    "available",
		Managed:         true,
		GitMetadataJSON: `{}`,
	}); err != nil {
		t.Fatalf("UpsertWorktreeRecord: %v", err)
	}
	if err := meta.UpdateSessionExecutionTarget(ctx, metadata.SessionExecutionTargetUpdate{
		SessionID:  parent.Meta().SessionID,
		Workspace:  &metadata.SessionExecutionTargetUpdateWorkspace{ID: binding.WorkspaceID},
		Worktree:   &metadata.SessionExecutionTargetUpdateWorktree{ID: "worktree-task"},
		CwdRelpath: "pkg",
	}); err != nil {
		t.Fatalf("UpdateSessionExecutionTarget parent: %v", err)
	}
	if err := parent.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode: session.WorktreeReminderModeEnter,
		WorktreeContext: session.WorktreeContext{
			Branch:        session.OptionalWorktreeBranch("task"),
			WorktreePath:  canonicalWorktreeRoot,
			WorkspaceRoot: workspace,
			EffectiveCwd:  canonicalWorktreeSubdir,
		},
	}); err != nil {
		t.Fatalf("SetWorktreeReminderState parent: %v", err)
	}
	if err := parent.SetWorktreeReminderState(nil); err != nil {
		t.Fatalf("consume parent worktree reminder: %v", err)
	}
	if parent.Meta().WorktreeReminder != nil {
		t.Fatalf("parent worktree reminder = %+v, want consumed reminder", parent.Meta().WorktreeReminder)
	}

	patchTarget := filepath.Join(canonicalWorktreeSubdir, "probe.txt")
	patchArgs, err := json.Marshal(map[string]any{"patch": strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: " + patchTarget,
		"+inherited target",
		"*** End Patch",
		"",
	}, "\n")})
	if err != nil {
		t.Fatalf("marshal patch arguments: %v", err)
	}
	patchOutput := make(chan string, 1)
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch providerCalls.Add(1) {
		case 1:
			writeRunPromptFunctionCallResponse(w, "fc-patch-target", "call-patch-target", toolspec.ToolPatch, patchArgs)
		case 2:
			defer r.Body.Close()
			var payload map[string]any
			if decodeErr := json.NewDecoder(r.Body).Decode(&payload); decodeErr != nil {
				t.Errorf("decode patch follow-up request: %v", decodeErr)
			}
			output, ok := findRunPromptFunctionCallOutput(payload)
			if !ok {
				t.Errorf("patch follow-up request has no function_call_output")
			} else {
				patchOutput <- output
			}
			modelstub.WriteCompletedResponseStream(w, "done", 1, 1)
		default:
			t.Errorf("unexpected provider request %d", providerCalls.Load())
		}
	}))
	defer provider.Close()

	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	cfg, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Settings.Model = "gpt-6-sol"
	cfg.Settings.ThinkingLevel = "medium"
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, provider.URL))
	cfg.Settings.EnabledTools = map[toolspec.ID]bool{toolspec.ToolPatch: true}
	cfg.Settings.AllowNonCwdEdits = true
	cfg.Settings.MaxSubagentDepth = 2
	cfg.Settings.Worktrees = config.WorktreeSettings{BaseDir: managedBase}
	cfg.Settings.ShellOutputMaxChars = 16_000
	cfg.Settings.Shell.PostprocessingMode = config.ShellPostprocessingModeBuiltin
	authority, liveRunObserver := newObservedTestHeadlessRuntimeAuthority(t, root, parent, meta, authManager, nil, meta.AuthoritativeSessionStoreOptions()...)
	client := NewInProcessRunPromptClient(HeadlessBootstrap{
		SessionLaunch: sessionlaunch.NewService(launch.Planner{
			Config:            cfg,
			ContainerDir:      containerDir,
			StoreOptions:      meta.AuthoritativeSessionStoreOptions(),
			PersistedSessions: meta,
			CallerSessions:    meta,
			ExecutionTargets:  meta,
			SessionProjects:   meta, ManagedWorktreeRoots: meta,
		}, sessionlaunch.ChatSettingsOwner{}, meta,
		),
		RuntimeAuthority:       authority,
		LiveRunObserver:        liveRunObserver,
		PromptHistory:          meta,
		Metadata:               meta,
		ManagedWorktreeBaseDir: managedBase,
	})
	parentID := parent.Meta().SessionID
	response, err := client.RunPrompt(ctx, requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(mustRunPromptSessionID(t, parentID))),
		CallerSessionID: &parentID,
		Prompt:          "verify the inherited execution target",
	}), nil)
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if response.GetFinalAnswer() != "done" {
		t.Fatalf("RunPrompt result = %q, want done", response.GetFinalAnswer())
	}
	select {
	case output := <-patchOutput:
		var result map[string]json.RawMessage
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("decode patch output %q: %v", output, err)
		}
		if warning, exists := result["warning"]; exists {
			t.Fatalf("patch inside inherited current worktree emitted warning %s", warning)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for patch output")
	}
	if data, err := os.ReadFile(patchTarget); err != nil || string(data) != "inherited target\n" {
		t.Fatalf("patch target data = %q, error = %v", data, err)
	}

	child, err := session.OpenByID(root, response.SessionId, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("OpenByID child: %v", err)
	}
	if child.Meta().WorktreeReminder != nil {
		t.Fatalf("child worktree reminder = %+v, want no reminder", child.Meta().WorktreeReminder)
	}
	target, err := meta.ResolveSessionExecutionTarget(ctx, response.SessionId)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget child: %v", err)
	}
	if target.Worktree == nil || target.Worktree.Id != "worktree-task" || target.EffectiveWorkdir != canonicalWorktreeSubdir {
		t.Fatalf("child execution target = %+v, want worktree-task at %q", target, canonicalWorktreeSubdir)
	}
	records, err := sessiontest.CollectRecords(child)
	if err != nil {
		t.Fatalf("CollectRecords child: %v", err)
	}
	wantCWD := "\nCWD: " + canonicalWorktreeSubdir + "\n"
	for _, record := range records {
		payload, payloadErr := record.Payload()
		if payloadErr != nil {
			t.Fatalf("read child event payload: %v", payloadErr)
		}
		message, ok := payload.(session.MessageRecord)
		if ok &&
			message.MessageType != nil &&
			*message.MessageType == session.MessageTypeEnvironment &&
			message.Content != nil &&
			strings.Contains(*message.Content, wantCWD) {
			return
		}
	}
	t.Fatalf("child environment did not use inherited CWD %q", canonicalWorktreeSubdir)
}

func TestWorkflowCallerDeniedTargetLeavesNoHeadlessLaunchArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.PersistenceRootEnvName, home)
	ctx := context.Background()
	root := t.TempDir()
	workspace := t.TempDir()
	meta := testsetup.OpenStore(t, root)
	binding, err := meta.RegisterWorkspaceBinding(ctx, workspace)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := filepath.Join(root, "projects", binding.ProjectID, "sessions")
	parent, err := session.Create(containerDir, filepath.Base(containerDir), workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create parent: %v", err)
	}
	if err := parent.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable parent: %v", err)
	}
	testsetup.BindSessionToWorkflowTask(t, meta, binding.ProjectID, parent.Meta().SessionID)
	ordinaryCaller, err := session.Create(containerDir, filepath.Base(containerDir), workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create ordinary caller: %v", err)
	}
	callerRole := "caller"
	if err := ordinaryCaller.SetContinuationContext(session.ContinuationContext{AgentRole: &callerRole}); err != nil {
		t.Fatalf("SetContinuationContext ordinary caller: %v", err)
	}
	if err := ordinaryCaller.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable ordinary caller: %v", err)
	}

	cfg, err := config.Load(workspace, workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.PersistenceRoot = root
	cfg.Settings.Model = "gpt-6-sol"
	cfg.Settings.Workflow = config.WorkflowSettings{Subagents: false}
	cfg.Settings = testsetup.WriteProviderSettings(t, root, cfg.Settings)
	hiddenSettings := cfg.Settings
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		"caller": {
			Settings: hiddenSettings,
			Sources: map[string]config.Origin{"thinking_level": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "thinking_level",
				}}, "agent_callable": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "agent_callable",
				}},
			},
		},
		"hidden": {
			Settings: hiddenSettings,
			Sources: map[string]config.Origin{"thinking_level": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "thinking_level",
				}}, "agent_callable": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "agent_callable",
				}},
			},
			AgentCallable: true,
		},
		"blocked": {
			Settings: hiddenSettings,
			Sources: map[string]config.Origin{"thinking_level": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "thinking_level",
				}}, "agent_callable": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "agent_callable",
				}},
			},
		},
	}
	worktreeRoot := filepath.Join(root, "worktrees")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll worktree root: %v", err)
	}
	worktreeRoot, err = config.CanonicalWorkspaceRoot(worktreeRoot)
	if err != nil {
		t.Fatalf("CanonicalWorkspaceRoot worktree root: %v", err)
	}
	if err := meta.UpsertWorktreeRecord(ctx, metadata.WorktreeRecord{
		ID:              "workflow-parent-worktree",
		WorkspaceID:     binding.WorkspaceID,
		CanonicalRoot:   worktreeRoot,
		DisplayName:     filepath.Base(worktreeRoot),
		Availability:    "available",
		Managed:         true,
		GitMetadataJSON: `{}`,
	}); err != nil {
		t.Fatalf("UpsertWorktreeRecord: %v", err)
	}
	authority := newTestHeadlessRuntimeAuthority(root, nil, nil, meta.AuthoritativeSessionStoreOptions()...)
	sessionLauncher := sessionlaunch.NewService(launch.Planner{
		Config:            cfg,
		ContainerDir:      containerDir,
		StoreOptions:      meta.AuthoritativeSessionStoreOptions(),
		PersistedSessions: meta,
		CallerSessions:    meta,
		ExecutionTargets:  meta,
		SessionProjects:   meta, ManagedWorktreeRoots: meta,
	}, sessionlaunch.ChatSettingsOwner{}, meta,
	)
	client := NewInProcessRunPromptClient(HeadlessBootstrap{
		SessionLaunch:    sessionLauncher,
		Metadata:         meta,
		RuntimeAuthority: authority,
	})
	before := snapshotHeadlessLaunchArtifacts(t, ctx, meta, binding.ProjectID, binding.WorkspaceID, containerDir, root, worktreeRoot)
	role := "hidden"
	parentID := parent.Meta().SessionID
	_, err = client.RunPrompt(ctx, requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(mustRunPromptSessionID(t, parentID))),
		CallerSessionID: &parentID,
		Prompt:          "delegate this",
		Overrides:       serverapi.RunPromptOverrides{AgentRole: &role},
	}), nil)
	var denied *serverapi.SubagentLaunchDeniedError
	if !errors.As(err, &denied) || denied.Kind != serverapi.SubagentLaunchDenialNotCallable {
		t.Fatalf("RunPrompt error = %T %v, want workflow policy denial", err, err)
	}
	after := snapshotHeadlessLaunchArtifacts(t, ctx, meta, binding.ProjectID, binding.WorkspaceID, containerDir, root, worktreeRoot)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("denied new-child launch changed artifacts: before=%+v after=%+v", before, after)
	}
	ordinaryCallerID := ordinaryCaller.Meta().SessionID
	blockedRole := "blocked"
	beforeOrdinaryTargetDenial := snapshotHeadlessLaunchArtifacts(t, ctx, meta, binding.ProjectID, binding.WorkspaceID, containerDir, root, worktreeRoot)
	_, err = client.RunPrompt(ctx, requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(mustRunPromptSessionID(t, ordinaryCallerID))),
		CallerSessionID: &ordinaryCallerID,
		Prompt:          "delegate this",
		Overrides:       serverapi.RunPromptOverrides{AgentRole: &blockedRole},
	}), nil)
	if !errors.As(err, &denied) || denied.Kind != serverapi.SubagentLaunchDenialNotCallable {
		t.Fatalf("ordinary blocked-target error = %T %v, want not-callable denial", err, err)
	}
	if afterOrdinaryTargetDenial := snapshotHeadlessLaunchArtifacts(t, ctx, meta, binding.ProjectID, binding.WorkspaceID, containerDir, root, worktreeRoot); !reflect.DeepEqual(afterOrdinaryTargetDenial, beforeOrdinaryTargetDenial) {
		t.Fatalf("ordinary blocked-target denial changed artifacts: before=%+v after=%+v", beforeOrdinaryTargetDenial, afterOrdinaryTargetDenial)
	}
	selected, err := session.Create(containerDir, filepath.Base(containerDir), workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create selected: %v", err)
	}
	if err := selected.SetName(textutil.Value("selected session")); err != nil {
		t.Fatalf("SetName selected: %v", err)
	}
	if err := selected.SetContinuationContext(session.ContinuationContext{AgentRole: &role}); err != nil {
		t.Fatalf("SetContinuationContext selected: %v", err)
	}
	selectedBefore := selected.Meta()
	selectedEventLog, err := selected.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize selected event log: %v", err)
	}
	selectedRevisionBefore, err := selectedEventLog.Revision()
	if err != nil {
		t.Fatalf("read selected event log revision: %v", err)
	}
	beforeSelectedPlan := snapshotHeadlessLaunchArtifacts(t, ctx, meta, binding.ProjectID, binding.WorkspaceID, containerDir, root, worktreeRoot)
	_, err = sessionLauncher.PlanLaunchSession(ctx, sessionlaunch.PlanRequest{
		Mode:            launch.ModeHeadless,
		Intent:          serverapi.OpenExistingSessionLaunchIntent(mustRunPromptSessionID(t, selectedBefore.SessionID)),
		CallerSessionID: &parentID,
	})
	if err != nil {
		t.Fatalf("continue selected PlanLaunchSession: %v", err)
	}
	reopenedSelected, err := session.OpenByID(root, selectedBefore.SessionID, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("OpenByID selected: %v", err)
	}
	reopenedEventLog, err := reopenedSelected.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize reopened selected event log: %v", err)
	}
	reopenedRevision, err := reopenedEventLog.Revision()
	if err != nil {
		t.Fatalf("read reopened selected event log revision: %v", err)
	}
	if got := reopenedSelected.Meta(); !reflect.DeepEqual(got.Name, selectedBefore.Name) ||
		!reflect.DeepEqual(got.PreviousSessionID, selectedBefore.PreviousSessionID) ||
		!reflect.DeepEqual(got.ParentAgentSessionID, selectedBefore.ParentAgentSessionID) ||
		got.Continuation == nil ||
		got.Continuation.AgentRole == nil ||
		*got.Continuation.AgentRole != role ||
		got.ModelRequestCount != selectedBefore.ModelRequestCount ||
		reopenedRevision != selectedRevisionBefore {
		t.Fatalf("selected session changed during continuation planning: before=%+v after=%+v", selectedBefore, got)
	}
	afterSelectedPlan := snapshotHeadlessLaunchArtifacts(t, ctx, meta, binding.ProjectID, binding.WorkspaceID, containerDir, root, worktreeRoot)
	if !reflect.DeepEqual(afterSelectedPlan, beforeSelectedPlan) {
		t.Fatalf("selected-session planning changed artifacts: before=%+v after=%+v", beforeSelectedPlan, afterSelectedPlan)
	}
	substitutedCaller := selectedBefore.SessionID
	substitutedCallerID := mustRunPromptSessionID(t, substitutedCaller)
	substitutedPlan, err := sessionLauncher.PlanLaunchSession(ctx, sessionlaunch.PlanRequest{
		Mode:            launch.ModeHeadless,
		Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(substitutedCallerID)),
		CallerSessionID: &substitutedCaller,
		Overrides:       serverapi.RunPromptOverrides{AgentRole: &role},
	})
	if err != nil {
		t.Fatalf("substituted ordinary caller PlanLaunchSession: %v", err)
	}
	substitutedStore, err := session.MaterializeSessionDescriptor(root, substitutedPlan.Plan.Descriptor, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("materialize substituted caller plan: %v", err)
	}
	if got := substitutedStore.Meta().ParentAgentSessionID; got == nil || *got != substitutedCallerID {
		t.Fatalf("substituted caller child parent-agent = %v, want %q", got, substitutedCaller)
	}
	if got := substitutedStore.Meta().Continuation; got == nil || got.AgentRole == nil || *got.AgentRole != role {
		t.Fatalf("substituted caller continuation = %+v, want hidden role", got)
	}
}

func TestWorkflowCallerLaunchesDefaultAndCustomHeadlessSubagents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.PersistenceRootEnvName, home)
	ctx := context.Background()
	root := t.TempDir()
	workspace := t.TempDir()
	meta := testsetup.OpenStore(t, root)
	binding, err := meta.RegisterWorkspaceBinding(ctx, workspace)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := filepath.Join(root, "projects", binding.ProjectID, "sessions")
	parent, err := session.Create(containerDir, filepath.Base(containerDir), workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create parent: %v", err)
	}
	if err := parent.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable parent: %v", err)
	}
	testsetup.BindSessionToWorkflowTask(t, meta, binding.ProjectID, parent.Meta().SessionID)
	parentID := parent.Meta().SessionID
	ordinaryParent, err := session.Create(containerDir, filepath.Base(containerDir), workspace, sessioncontract.SessionCategoryMain, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create ordinary parent: %v", err)
	}
	currentRole := "current"
	if err := ordinaryParent.SetContinuationContext(session.ContinuationContext{AgentRole: &currentRole}); err != nil {
		t.Fatalf("SetContinuationContext ordinary parent: %v", err)
	}
	ordinaryParentID := ordinaryParent.Meta().SessionID

	var responseCount int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseCount++
		modelstub.WriteCompletedResponseStream(w, "workflow response", 1, 1)
	}))
	defer provider.Close()
	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	cfg, err := config.Load(workspace, workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.PersistenceRoot = root
	cfg.Settings.Model = "gpt-6-sol"
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, provider.URL))
	cfg.Settings.Workflow = config.WorkflowSettings{Subagents: true}
	workerSettings := cfg.Settings
	cfg.Settings.Subagents = map[string]config.SubagentRole{
		"current": {
			Settings: workerSettings,
			Sources: map[string]config.Origin{"model": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "model",
				}}, "agent_callable": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "agent_callable",
				}},
			},
		},
		"worker": {
			Settings: workerSettings,
			Sources: map[string]config.Origin{"model": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "model",
				}}, "agent_callable": {Kind: config.SourceInput,

				Property: config.PropertyAddress{
					Key: "agent_callable",
				}},
			},
			AgentCallable: true,
		},
	}
	authority, liveRunObserver := newObservedTestHeadlessRuntimeAuthority(t, root, parent, meta, authManager, nil, meta.AuthoritativeSessionStoreOptions()...)
	client := NewInProcessRunPromptClient(HeadlessBootstrap{
		SessionLaunch: sessionlaunch.NewService(launch.Planner{
			Config:            cfg,
			ContainerDir:      containerDir,
			StoreOptions:      meta.AuthoritativeSessionStoreOptions(),
			PersistedSessions: meta,
			CallerSessions:    meta,
			ExecutionTargets:  meta,
			SessionProjects:   meta, ManagedWorktreeRoots: meta,
		}, sessionlaunch.ChatSettingsOwner{}, meta,
		),
		RuntimeAuthority: authority,
		LiveRunObserver:  liveRunObserver,
		PromptHistory:    meta,
		Metadata:         meta,
	})

	worker := "worker"
	var sessionStarted int
	request := requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(mustRunPromptSessionID(t, parentID))),
		CallerSessionID: &parentID,
		Prompt:          "delegate this",
		Overrides:       serverapi.RunPromptOverrides{AgentRole: &worker},
	})
	parentRecord, err := meta.ResolvePersistedSession(ctx, parentID)
	if err != nil || parentRecord.ShortID == nil {
		t.Fatalf("parent identity = %+v, %v", parentRecord, err)
	}
	request.Intent.GetCreateNew().Origin = &sessionlaunchpb.SessionCreateRequestOrigin_ParentAgentSessionId{
		ParentAgentSessionId: protoapi.SessionSelectorToProto(*parentRecord.ShortID),
	}
	response, err := client.RunPrompt(ctx, request, serverapi.RunPromptProgressFunc(func(progress *runpromptpb.ProgressEvent) {
		if _, ok := progress.Payload.(*runpromptpb.ProgressEvent_SessionStarted); !ok {
			return
		}
		sessionStarted++
		if progress.GetSessionStarted().ShortId == nil {
			t.Error("new Session start omitted its assigned number")
		}
		if progress.GetSessionStarted() == nil {
			t.Errorf("session_started published before the new runtime became active: %+v", progress)
			return
		}
		sessionID, parseErr := runtimeids.ParseSessionID(progress.GetSessionStarted().SessionId)
		if parseErr != nil {
			t.Errorf("session_started session id: %v", parseErr)
			return
		}
		if _, active := authority.SessionExecution(sessionID); !active {
			t.Errorf("session_started published before the new execution became active: %+v", progress)
		}
	}))
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if response.GetFinalAnswer() != "workflow response" {
		t.Fatalf("result = %q, want provider response", response.GetFinalAnswer())
	}
	if sessionStarted != 1 {
		t.Fatalf("session_started events = %d, want 1", sessionStarted)
	}
	child, err := session.OpenByID(root, response.SessionId, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("OpenByID child: %v", err)
	}
	childMeta := child.Meta()
	if response.ShortId == nil || child.Identity().ShortID == nil || *response.ShortId != child.Identity().ShortID.Int64() {
		t.Fatalf("completion identity = %v, want %v", response.ShortId, child.Identity())
	}
	parentSessionID := mustRunPromptSessionID(t, parentID)
	if childMeta.ParentAgentSessionID == nil || *childMeta.ParentAgentSessionID != parentSessionID {
		t.Fatalf("parent-agent id = %v, want %q", childMeta.ParentAgentSessionID, parentID)
	}
	if got := childMeta.Continuation; got == nil || got.AgentRole == nil || *got.AgentRole != worker {
		t.Fatalf("continuation = %+v, want worker role", got)
	}
	responseID, err := runtimeids.ParseSessionID(response.SessionId)
	if err != nil {
		t.Fatalf("parse response session id: %v", err)
	}
	if _, active := authority.SessionExecution(responseID); active {
		t.Fatalf("completed headless launch left runtime active for %q", response.SessionId)
	}
	for _, raw := range []string{"000" + child.Identity().String(), child.Meta().SessionID} {
		selector, err := runtimeids.ParseSessionSelector(raw)
		if err != nil {
			t.Fatal(err)
		}
		resumed, err := client.RunPrompt(ctx, &runpromptpb.Request{
			Intent: &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_OpenExistingSessionId{
				OpenExistingSessionId: protoapi.SessionSelectorToProto(selector),
			}},
			Prompt: "continue",
		}, nil)
		if err != nil || resumed.SessionId != child.Meta().SessionID {
			t.Fatalf("continue %s = %+v, %v", raw, resumed, err)
		}
	}

	fast := config.BuiltInSubagentRoleFast
	ordinaryTests := []struct {
		name string
		role string
	}{
		{name: "fast", role: fast},
		{name: "custom", role: worker},
	}
	for _, test := range ordinaryTests {
		t.Run("ordinary non-callable caller "+test.name, func(t *testing.T) {
			response, err := client.RunPrompt(ctx, requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
				Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(mustRunPromptSessionID(t, ordinaryParentID))),
				CallerSessionID: &ordinaryParentID,
				Prompt:          "delegate this",
				Overrides:       serverapi.RunPromptOverrides{AgentRole: &test.role},
			}), nil)
			if err != nil {
				t.Fatalf("RunPrompt: %v", err)
			}
			if response.GetFinalAnswer() != "workflow response" {
				t.Fatalf("result = %q, want provider response", response.GetFinalAnswer())
			}
			child, err := session.OpenByID(root, response.SessionId, meta.AuthoritativeSessionStoreOptions()...)
			if err != nil {
				t.Fatalf("OpenByID child: %v", err)
			}
			childMeta := child.Meta()
			ordinaryParentSessionID := mustRunPromptSessionID(t, ordinaryParentID)
			if childMeta.ParentAgentSessionID == nil || *childMeta.ParentAgentSessionID != ordinaryParentSessionID {
				t.Fatalf("parent-agent id = %v, want %q", childMeta.ParentAgentSessionID, ordinaryParentID)
			}
			if childMeta.Continuation == nil || childMeta.Continuation.AgentRole == nil || *childMeta.Continuation.AgentRole != test.role {
				t.Fatalf("continuation = %+v, want role %q", childMeta.Continuation, test.role)
			}
			responseID, err := runtimeids.ParseSessionID(response.SessionId)
			if err != nil {
				t.Fatalf("parse response session id: %v", err)
			}
			if _, active := authority.SessionExecution(responseID); active {
				t.Fatalf("completed headless launch left runtime active for %q", response.SessionId)
			}
		})
	}
	if responseCount != 5 {
		t.Fatalf("provider responses = %d, want 5", responseCount)
	}
}

func TestInProcessRunPromptClientUsesSelectedSessionConnection(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(containerDir, "workspace-a", workspace, sessioncontract.SessionCategorySubagent, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}

	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatal("auth-less connection sent authorization")
		}
		providerCalls.Add(1)
		modelstub.WriteCompletedResponseStream(w, "from persisted continuation", 1, 1)
	}))
	defer server.Close()

	connectionID := config.ConnectionID("saved")
	if err := store.SetConnectionID(connectionID); err != nil {
		t.Fatalf("set connection: %v", err)
	}

	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	cfg := config.App{
		WorkspaceRoot:   workspace,
		PersistenceRoot: root,
		Settings: testsetup.WriteProviderSettings(t, root, testsetup.WithResponsesProvider(config.Settings{
			Model:         "gpt-6-sol",
			ThinkingLevel: "medium",
			Shell:         config.ShellSettings{PostprocessingMode: config.ShellPostprocessingModeBuiltin},
		}, "http://wrong.invalid")),
	}
	cfg.Settings.Connections[connectionID] = config.ProviderConnection{
		Protocol: config.ConnectionResponses, Endpoint: &server.URL,
	}
	testsetup.WriteProviderSettings(t, root, cfg.Settings)
	history := &recordingPromptHistoryStore{}
	authority, liveRunObserver := newObservedTestHeadlessRuntimeAuthority(t, root, store, nil, authManager, nil, persistence.Options()...)
	client := NewInProcessRunPromptClient(HeadlessBootstrap{
		SessionLaunch:    newTestHeadlessSessionLaunch(t, cfg, containerDir, authManager, persistence),
		RuntimeAuthority: authority,
		LiveRunObserver:  liveRunObserver,
		PromptHistory:    history,
	})

	var progresses []*runpromptpb.ProgressEvent
	request := serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustRunPromptSessionID(t, store.Meta().SessionID)),
		Prompt: "  hello  ",
	}
	response, err := client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, request), serverapi.RunPromptProgressFunc(func(progress *runpromptpb.ProgressEvent) {
		progresses = append(progresses, progress)
	}))
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if response.SessionId != store.Meta().SessionID {
		t.Fatalf("session id = %q, want %q", response.SessionId, store.Meta().SessionID)
	}
	if response.GetFinalAnswer() != "from persisted continuation" {
		t.Fatalf("result = %q, want from persisted continuation", response.GetFinalAnswer())
	}
	for _, progress := range progresses {
		if progress.GetSessionStarted() != nil {
			t.Fatalf("continued run announced a new session: %+v", progress)
		}
	}
	if got := store.Meta().ConnectionID; got == nil || *got != connectionID {
		t.Fatalf("expected persisted connection preserved, got %+v", got)
	}
	replayed, err := client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, request), nil)
	if err != nil {
		t.Fatalf("replayed RunPrompt: %v", err)
	}
	if replayed.SessionId != response.SessionId || replayed.GetFinalAnswer() != response.GetFinalAnswer() || providerCalls.Load() != 2 {
		t.Fatalf("repeated response=%+v provider calls=%d, want a second direct operation", replayed, providerCalls.Load())
	}
	if len(history.entries) != 2 {
		t.Fatalf("prompt history = %+v, want one entry per direct operation", history.entries)
	}
	mismatch := request
	mismatch.Prompt = "different"
	if _, err := client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, mismatch), nil); err != nil {
		t.Fatalf("repeated request with changed input: %v", err)
	}
	if providerCalls.Load() != 3 {
		t.Fatalf("provider calls = %d, want three explicit operations", providerCalls.Load())
	}
	keyName := "RUNPROMPT_BOUND_CONNECTION_KEY"
	t.Setenv(keyName, "")
	definition := cfg.Settings.Connections[connectionID]
	definition.EnvironmentVariable = &keyName
	cfg.Settings.Connections[connectionID] = definition
	testsetup.WriteProviderSettings(t, root, cfg.Settings)
	if _, err := client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, request), nil); err == nil {
		t.Fatal("missing credential must fail the bound connection")
	}
	reopened, err := session.Open(store.Dir(), persistence.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	if saved := reopened.Meta().ConnectionID; saved == nil || *saved != connectionID || providerCalls.Load() != 3 {
		t.Fatalf("credential failure changed binding or dispatched: binding=%v calls=%d", saved, providerCalls.Load())
	}
}

func TestInProcessRunPromptTimeoutCoversHistoryAndRunCleanup(t *testing.T) {
	t.Run("prompt history", func(t *testing.T) {
		var providerCalls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			providerCalls.Add(1)
			modelstub.WriteCompletedResponseStream(w, "history delayed run", 1, 1)
		}))
		defer server.Close()

		history := &gatedPromptHistoryStore{
			started: make(chan struct{}),
			release: make(chan struct{}),
		}
		defer history.Release()
		fixture := newSelectedRunPromptFixture(t, server.URL, history)
		sessionID := mustRunPromptSessionID(t, fixture.store.Meta().SessionID)
		type result struct {
			response *runpromptpb.Success
			err      error
		}
		done := make(chan result, 1)
		go func() {
			response, err := fixture.client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
				Intent:  serverapi.OpenExistingSessionLaunchIntent(sessionID),
				Prompt:  "hello",
				Timeout: 2 * time.Second,
			}), nil)
			done <- result{response: response, err: err}
		}()

		select {
		case <-history.started:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for prompt history")
		}
		select {
		case got := <-done:
			t.Fatalf("RunPrompt returned before prompt history was released: response=%+v err=%v", got.response, got.err)
		case <-time.After(2500 * time.Millisecond):
		}
		if got := providerCalls.Load(); got != 0 {
			t.Fatalf("provider calls=%d while prompt history was pending, want 0", got)
		}

		history.Release()
		select {
		case got := <-done:
			if got.err != nil {
				t.Fatalf("RunPrompt error after prompt history release: %v", got.err)
			}
			if got.response.SessionId != fixture.store.Meta().SessionID || got.response.GetFinalAnswer() != "history delayed run" {
				t.Fatalf("RunPrompt response=%+v, want selected session and delayed result", got.response)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("RunPrompt did not finish after prompt history release")
		}
	})

	t.Run("runtime", func(t *testing.T) {
		const runTimeout = 2 * time.Second
		started := make(chan struct{})
		release := make(chan struct{})
		var startedOnce sync.Once
		var providerCalls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedOnce.Do(func() { close(started) })
			providerCalls.Add(1)
			<-release
		}))
		defer func() {
			close(release)
			server.Close()
		}()

		history := &gatedPromptHistoryStore{
			started: make(chan struct{}),
			release: make(chan struct{}),
		}
		defer history.Release()
		fixture := newSelectedRunPromptFixture(t, server.URL, history)
		type result struct {
			response *runpromptpb.Success
			err      error
		}
		done := make(chan result, 1)
		go func() {
			response, err := fixture.client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
				Intent:  serverapi.OpenExistingSessionLaunchIntent(mustRunPromptSessionID(t, fixture.store.Meta().SessionID)),
				Prompt:  "hello",
				Timeout: runTimeout,
			}), nil)
			done <- result{response: response, err: err}
		}()

		select {
		case <-history.started:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for prompt history")
		}
		select {
		case got := <-done:
			t.Fatalf("RunPrompt returned before prompt history was released: response=%+v err=%v", got.response, got.err)
		case <-time.After(runTimeout + 500*time.Millisecond):
		}
		if got := providerCalls.Load(); got != 0 {
			t.Fatalf("provider calls=%d while prompt history was pending, want 0", got)
		}

		historyReleasedAt := time.Now()
		history.Release()
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for provider request")
		}
		select {
		case got := <-done:
			if !errors.Is(got.err, context.DeadlineExceeded) {
				t.Fatalf("RunPrompt error = %v, want deadline exceeded", got.err)
			}
			if got.response.SessionId != fixture.store.Meta().SessionID {
				t.Fatalf("partial response session = %q, want %q", got.response.SessionId, fixture.store.Meta().SessionID)
			}
			if elapsed := time.Since(historyReleasedAt); elapsed < runTimeout-100*time.Millisecond {
				t.Fatalf("RunPrompt expired after %s from prompt history release, want at least %s", elapsed, runTimeout-100*time.Millisecond)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("RunPrompt did not finish after timeout")
		}
		sessionID := mustRunPromptSessionID(t, fixture.store.Meta().SessionID)
		if _, active := fixture.authority.SessionExecution(sessionID); active {
			t.Fatal("timed out headless execution remained active")
		}
	})

	t.Run("omitted timeout", func(t *testing.T) {
		history := &gatedPromptHistoryStore{
			started: make(chan struct{}),
			release: make(chan struct{}),
		}
		defer history.Release()

		providerStarted := make(chan struct{})
		providerRelease := make(chan struct{})
		var providerStartedOnce sync.Once
		var providerReleaseOnce sync.Once
		var providerCalls atomic.Int32
		releaseProvider := func() {
			providerReleaseOnce.Do(func() { close(providerRelease) })
		}
		defer releaseProvider()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			providerCalls.Add(1)
			providerStartedOnce.Do(func() { close(providerStarted) })
			<-providerRelease
			modelstub.WriteCompletedResponseStream(w, "unbounded run", 1, 1)
		}))
		defer server.Close()

		fixture := newSelectedRunPromptFixture(t, server.URL, history)
		sessionID := mustRunPromptSessionID(t, fixture.store.Meta().SessionID)
		type result struct {
			response *runpromptpb.Success
			err      error
		}
		done := make(chan result, 1)
		go func() {
			response, err := fixture.client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
				Intent: serverapi.OpenExistingSessionLaunchIntent(sessionID),
				Prompt: "hello",
			}), nil)
			done <- result{response: response, err: err}
		}()

		select {
		case <-history.started:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for prompt history")
		}
		select {
		case got := <-done:
			t.Fatalf("RunPrompt returned before prompt history was released: response=%+v err=%v", got.response, got.err)
		case <-time.After(500 * time.Millisecond):
		}
		if got := providerCalls.Load(); got != 0 {
			t.Fatalf("provider calls=%d while prompt history was pending, want 0", got)
		}

		history.Release()
		select {
		case <-providerStarted:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for provider request")
		}
		select {
		case got := <-done:
			t.Fatalf("RunPrompt returned while provider was pending: response=%+v err=%v", got.response, got.err)
		case <-time.After(500 * time.Millisecond):
		}

		releaseProvider()
		select {
		case got := <-done:
			if got.err != nil {
				t.Fatalf("RunPrompt error after provider release: %v", got.err)
			}
			if got.response.SessionId != fixture.store.Meta().SessionID || got.response.GetFinalAnswer() != "unbounded run" {
				t.Fatalf("RunPrompt response=%+v, want selected session and unbounded result", got.response)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("RunPrompt did not finish after provider release")
		}
	})
}

func TestInProcessRunPromptFailsBeforeProviderWhenPromptHistoryFails(t *testing.T) {
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		providerCalls.Add(1)
	}))
	defer server.Close()

	historyErr := errors.New("prompt history failed")
	fixture := newSelectedRunPromptFixture(t, server.URL, failingPromptHistoryStore{err: historyErr})
	sessionID := mustRunPromptSessionID(t, fixture.store.Meta().SessionID)
	response, err := fixture.client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(sessionID),
		Prompt: "hello",
	}), nil)
	if !errors.Is(err, historyErr) {
		t.Fatalf("RunPrompt error = %v, want prompt history failure", err)
	}
	if response != nil {
		t.Fatalf("RunPrompt response = %+v, want no success response", response)
	}
	if got := providerCalls.Load(); got != 0 {
		t.Fatalf("provider calls=%d after prompt history failure, want 0", got)
	}
	if _, active := fixture.authority.SessionExecution(sessionID); active {
		t.Fatal("prompt history failure left runtime active")
	}
}

func TestInProcessRunPromptReturnsPendingQuestionAndExecutionContinues(t *testing.T) {
	var calls atomic.Int32
	providerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	continued := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			close(providerStarted)
			select {
			case <-releaseProvider:
			case <-r.Context().Done():
				return
			}
			writeRunPromptAskQuestionResponse(w, "partial progress")
		case 2:
			defer r.Body.Close()
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode Question follow-up request: %v", err)
			}
			if _, ok := findRunPromptFunctionCallOutput(payload); !ok {
				t.Errorf("Question follow-up request has no function_call_output")
			}
			close(continued)
			modelstub.WriteCompletedResponseStream(w, "resumed", 1, 1)
		default:
			t.Errorf("unexpected provider request %d", calls.Load())
		}
	}))
	defer server.Close()

	fixture := newSelectedRunPromptFixture(t, server.URL, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = fixture.authority.Close(ctx)
	})
	responseCh := make(chan struct {
		response *runpromptpb.Success
		err      error
	}, 1)
	selectedSessionID := mustRunPromptSessionID(t, fixture.store.Meta().SessionID)
	go func() {
		response, err := fixture.client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
			Intent: serverapi.OpenExistingSessionLaunchIntent(selectedSessionID),
			Prompt: "ask before continuing",
		}), nil)
		responseCh <- struct {
			response *runpromptpb.Success
			err      error
		}{response, err}
	}()
	select {
	case <-providerStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("provider request did not start")
	}
	select {
	case <-fixture.initialPromptRead:
	case <-time.After(3 * time.Second):
		t.Fatal("launch observer did not perform its initial empty prompt read")
	}
	close(releaseProvider)
	var response *runpromptpb.Success
	select {
	case result := <-responseCh:
		if result.err != nil {
			t.Fatalf("RunPrompt: %v", result.err)
		}
		response = result.response
	case <-time.After(3 * time.Second):
		t.Fatal("RunPrompt did not return while the Question was pending")
	}
	question := response.GetQuestion()
	if response.SessionId != fixture.store.Meta().SessionID ||
		question == nil ||
		question.GetAsk() == nil ||
		question.GetAsk().GetToolCallId() == "" {
		t.Fatalf("RunPrompt response = %+v, want pending Question", response)
	}
	sessionID := mustRunPromptSessionID(t, response.SessionId)
	stepID, err := runtimeids.ParseStepID(question.GetAsk().GetStepId())
	if err != nil {
		t.Fatalf("parse pending Question Step ID: %v", err)
	}
	before, active := fixture.authority.SessionExecution(sessionID)
	if !active {
		t.Fatal("execution stopped when the launch returned a pending Question")
	}
	_, err = fixture.authority.ResolvePromptBatch(context.Background(), sessionID, stepID, []sessionruntime.PromptAnswerCommand{{
		ToolCallID: clientui.ToolCallID(question.GetAsk().GetToolCallId()),
		Payload: sessionruntime.PromptQuestionAnswerCommand{Answer: askquestion.AskQuestionAnswer{
			Freeform: textutil.Value("continue"),
		}},
	}})
	if err != nil {
		t.Fatalf("answer pending Question through Session Runtime Authority: %v", err)
	}
	select {
	case <-continued:
	case <-time.After(3 * time.Second):
		t.Fatal("the same execution did not continue after answering the Question")
	}
	after, active := fixture.authority.SessionExecution(sessionID)
	if !active || after != before {
		t.Fatalf("execution after answer = %v active=%t, want the same active execution", after, active)
	}
	if _, err := before.Wait(context.Background()); err != nil {
		t.Fatalf("continued execution: %v", err)
	}
}

func TestInProcessRunPromptReturnsPendingApprovalAndExecutionContinues(t *testing.T) {
	var calls atomic.Int32
	providerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	continued := make(chan struct{})
	outside := testsetup.NonTemporaryDirectory(t, "kent-runprompt-outside-", askquestion.IsPathInTemporaryDir)
	target := filepath.Join(outside, "approved.txt")
	patchArgs, err := json.Marshal(map[string]any{"patch": strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: " + target,
		"+approved",
		"*** End Patch",
		"",
	}, "\n")})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			close(providerStarted)
			select {
			case <-releaseProvider:
			case <-r.Context().Done():
				return
			}
			writeRunPromptFunctionCallResponse(w, "fc-approval", "call-approval", toolspec.ToolPatch, patchArgs)
		case 2:
			defer r.Body.Close()
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode Approval follow-up request: %v", err)
			}
			_, ok := findRunPromptFunctionCallOutput(payload)
			if !ok {
				t.Errorf("Approval follow-up request has no function_call_output")
			}
			close(continued)
			modelstub.WriteCompletedResponseStream(w, "resumed", 1, 1)
		default:
			// The scenario verifies that the same execution resumes, not an exact model-turn count.
			modelstub.WriteCompletedResponseStream(w, "resumed", 1, 1)
		}
	}))
	defer server.Close()

	fixture := newSelectedRunPromptFixture(t, server.URL, nil, toolspec.ToolPatch)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = fixture.authority.Close(ctx)
	})
	responseCh := make(chan struct {
		response *runpromptpb.Success
		err      error
	}, 1)
	selectedSessionID := mustRunPromptSessionID(t, fixture.store.Meta().SessionID)
	go func() {
		response, err := fixture.client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
			Intent: serverapi.OpenExistingSessionLaunchIntent(selectedSessionID),
			Prompt: "write outside the workspace",
		}), nil)
		responseCh <- struct {
			response *runpromptpb.Success
			err      error
		}{response, err}
	}()
	select {
	case <-providerStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("provider request did not start")
	}
	select {
	case <-fixture.initialPromptRead:
	case <-time.After(3 * time.Second):
		t.Fatal("launch observer did not perform its initial empty prompt read")
	}
	close(releaseProvider)
	var response *runpromptpb.Success
	select {
	case result := <-responseCh:
		if result.err != nil {
			t.Fatalf("RunPrompt: %v", result.err)
		}
		response = result.response
	case <-time.After(3 * time.Second):
		t.Fatal("RunPrompt did not return while the Approval was pending")
	}
	approval := response.GetQuestion().GetApproval()
	if response.SessionId != fixture.store.Meta().SessionID ||
		approval == nil ||
		approval.GetToolCallId() == "" ||
		len(approval.GetOptions()) != 3 ||
		len(approval.GetAccessTargets()) != 1 {
		t.Fatalf("RunPrompt response = %+v, want pending access Approval", response)
	}
	wantDecisions := []promptpb.ApprovalDecision{
		promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_ONCE,
		promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_SESSION,
		promptpb.ApprovalDecision_APPROVAL_DECISION_DENY,
	}
	for i, option := range approval.GetOptions() {
		if option.GetDecision() != wantDecisions[i] {
			t.Fatalf("Approval option %d = %v, want %v", i, option.GetDecision(), wantDecisions[i])
		}
	}
	if approval.GetAccessTargets()[0].GetRequestedPath() != target {
		t.Fatalf("Approval access target = %+v, want requested path %q", approval.GetAccessTargets()[0], target)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside-workspace operation ran before approval, stat error = %v", err)
	}
	stepID, err := runtimeids.ParseStepID(approval.GetStepId())
	if err != nil {
		t.Fatalf("parse pending Approval Step ID: %v", err)
	}
	before, active := fixture.authority.SessionExecution(selectedSessionID)
	if !active {
		t.Fatal("execution stopped when the launch returned a pending Approval")
	}
	_, err = fixture.authority.ResolvePromptBatch(context.Background(), selectedSessionID, stepID, []sessionruntime.PromptAnswerCommand{{
		ToolCallID: clientui.ToolCallID(approval.GetToolCallId()),
		Payload: sessionruntime.PromptApprovalAnswerCommand{Answer: askquestion.AskQuestionApproval{
			Decision: askquestion.AskQuestionApprovalDecisionAllowOnce,
		}},
	}})
	if err != nil {
		t.Fatalf("answer pending Approval through Session Runtime Authority: %v", err)
	}
	select {
	case <-continued:
	case <-time.After(3 * time.Second):
		t.Fatal("the same execution did not continue after answering the Approval")
	}
	after, active := fixture.authority.SessionExecution(selectedSessionID)
	if !active || after != before {
		t.Fatalf("execution after answer = %v active=%t, want the same active execution", after, active)
	}
	if _, err := before.Wait(context.Background()); err != nil {
		t.Fatalf("continued execution: %v", err)
	}
	output, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read approved operation output: %v", err)
	}
	if string(output) != "approved\n" {
		t.Fatalf("approved operation output = %q, want file created by patch tool", output)
	}
}

func TestInProcessRunPromptClientUsesActiveShellPostprocessorWithSuppliedBackgroundManager(t *testing.T) {
	root := t.TempDir()
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(containerDir, "workspace-a", t.TempDir(), sessioncontract.SessionCategorySubagent, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}

	effectiveHook := writeRunPromptHook(t, fmt.Sprintf(
		`printf '{"processed":true,"replaced_output":"EFFECTIVE:%%s"}' "$%s"`,
		sessionenv.SessionIDEnv,
	))
	background, err := shelltool.NewManager(root)
	if err != nil {
		t.Fatalf("new supplied background manager: %v", err)
	}
	t.Cleanup(func() { _ = background.Close() })

	toolOutput := make(chan string, 1)
	var responseCount int
	var responseMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}

		responseMu.Lock()
		responseCount++
		currentResponse := responseCount
		responseMu.Unlock()

		switch currentResponse {
		case 1:
			args, marshalErr := json.Marshal(map[string]any{
				"cmd":           "printf raw",
				"shell":         "/bin/sh",
				"login":         false,
				"yield_time_ms": 1000,
			})
			if marshalErr != nil {
				t.Fatalf("marshal exec_command arguments: %v", marshalErr)
			}
			writeRunPromptExecCommandResponse(w, args)
		case 2:
			defer r.Body.Close()
			var payload map[string]any
			if decodeErr := json.NewDecoder(r.Body).Decode(&payload); decodeErr != nil {
				t.Fatalf("decode second provider request: %v", decodeErr)
			}
			output, ok := findRunPromptFunctionCallOutput(payload)
			if !ok {
				t.Fatalf("second provider request has no function_call_output: %#v", payload)
			}
			toolOutput <- output
			modelstub.WriteCompletedResponseStream(w, "done", 1, 1)
		default:
			t.Fatalf("unexpected provider response request %d", currentResponse)
		}
	}))
	defer server.Close()

	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	cfg := config.App{
		WorkspaceRoot:   store.Meta().WorkspaceRoot,
		PersistenceRoot: root,
		Settings: testsetup.WriteProviderSettings(t, root, testsetup.WithResponsesProvider(config.Settings{
			Model:               "gpt-6-sol",
			ThinkingLevel:       "medium",
			ShellOutputMaxChars: 16_000,
			EnabledTools:        map[toolspec.ID]bool{toolspec.ToolExecCommand: true},
			Shell: config.ShellSettings{
				PostprocessingMode: config.ShellPostprocessingModeUser,
				PostprocessHook:    &effectiveHook,
			},
		}, server.URL)),
	}
	authority, liveRunObserver := newObservedTestHeadlessRuntimeAuthority(t, root, store, nil, authManager, background, persistence.Options()...)
	client := NewInProcessRunPromptClient(HeadlessBootstrap{
		SessionLaunch:    newTestHeadlessSessionLaunch(t, cfg, containerDir, authManager, persistence),
		RuntimeAuthority: authority,
		LiveRunObserver:  liveRunObserver,
	})

	response, err := client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustRunPromptSessionID(t, store.Meta().SessionID)),
		Prompt: "run the shell probe",
	}), nil)
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if response.GetFinalAnswer() != "done" {
		t.Fatalf("result = %q, want done", response.GetFinalAnswer())
	}
	select {
	case output := <-toolOutput:
		want := "EFFECTIVE:" + store.Meta().SessionID
		if output != want {
			t.Fatalf("exec_command output = %q, want %q", output, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for exec_command output")
	}
}

func writeRunPromptHook(t *testing.T, body string) string {
	t.Helper()
	script := "#!/bin/sh\n" + body + "\n"
	return testsetup.WriteExecutable(t, "postprocess-hook.sh", script)
}

func writeRunPromptExecCommandResponse(w http.ResponseWriter, args json.RawMessage) {
	writeRunPromptFunctionCallResponse(w, "fc-runprompt-1", "call-runprompt-1", toolspec.ToolExecCommand, args)
}

func writeRunPromptFunctionCallResponse(w http.ResponseWriter, itemID string, callID string, toolID toolspec.ID, args json.RawMessage) {
	writeSSEJSON(w, map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{
			"id":        itemID,
			"type":      "function_call",
			"name":      string(toolID),
			"call_id":   callID,
			"arguments": "",
		},
	})
	writeSSEJSON(w, map[string]any{
		"type":    "response.function_call_arguments.delta",
		"item_id": itemID,
		"delta":   string(args),
	})
	writeSSEJSON(w, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"usage": map[string]any{
				"input_tokens":  1,
				"output_tokens": 1,
				"total_tokens":  2,
			},
			"output": []any{
				map[string]any{
					"type":      "function_call",
					"id":        itemID,
					"name":      string(toolID),
					"call_id":   callID,
					"arguments": string(args),
				},
			},
		},
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeRunPromptAskQuestionResponse(w http.ResponseWriter, commentary string) {
	args := json.RawMessage(`{"question":"Need input?"}`)
	writeSSEJSON(w, map[string]any{
		"type":         "response.output_item.added",
		"output_index": 0,
		"item":         map[string]any{"type": "message", "role": "assistant", "phase": "commentary", "content": []any{}},
	})
	writeSSEJSON(w, map[string]any{"type": "response.output_text.delta", "output_index": 0, "delta": commentary})
	writeSSEJSON(w, map[string]any{
		"type":         "response.output_item.added",
		"output_index": 1,
		"item":         map[string]any{"id": "fc-ask", "type": "function_call", "name": string(toolspec.ToolAskQuestion), "call_id": "call-ask", "arguments": ""},
	})
	writeSSEJSON(w, map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc-ask", "delta": string(args)})
	writeSSEJSON(w, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
			"output": []any{
				map[string]any{"type": "message", "role": "assistant", "phase": "commentary", "content": []any{map[string]any{"type": "output_text", "text": commentary}}},
				map[string]any{"id": "fc-ask", "type": "function_call", "name": string(toolspec.ToolAskQuestion), "call_id": "call-ask", "arguments": string(args)},
			},
		},
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeSSEJSON(w http.ResponseWriter, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal run prompt SSE event: %v", err))
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
}

func findRunPromptFunctionCallOutput(payload map[string]any) (string, bool) {
	items, ok := payload["input"].([]any)
	if !ok {
		return "", false
	}
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok || item["type"] != "function_call_output" {
			continue
		}
		output, ok := item["output"].(string)
		if !ok {
			return "", false
		}
		return output, true
	}
	return "", false
}

func mustRunPromptSessionID(t *testing.T, raw string) runtimeids.SessionID {
	t.Helper()
	id, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatalf("ParseSessionID(%q): %v", raw, err)
	}
	return id
}

func TestInProcessRunPromptClientRejectsSelectedSessionWithGoal(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(containerDir, "workspace-a", workspace, sessioncontract.SessionCategorySubagent, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, _, err := store.SetGoal("ship feature", session.GoalActorUser); err != nil {
		t.Fatalf("set goal: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}

	cfg := config.App{
		WorkspaceRoot:   workspace,
		PersistenceRoot: root,
		Settings:        testsetup.WriteProviderSettings(t, root, config.Settings{Model: "gpt-6-sol"}),
	}
	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)
	authority, liveRunObserver := newObservedTestHeadlessRuntimeAuthority(t, root, store, nil, authManager, nil, persistence.Options()...)
	client := NewInProcessRunPromptClient(HeadlessBootstrap{
		SessionLaunch:    newTestHeadlessSessionLaunch(t, cfg, containerDir, authManager, persistence),
		RuntimeAuthority: authority,
		LiveRunObserver:  liveRunObserver,
	})

	_, err = client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustRunPromptSessionID(t, store.Meta().SessionID)),
		Prompt: "continue",
	}), nil)
	if !errors.Is(err, ErrHeadlessGoalSession) {
		t.Fatalf("RunPrompt error = %v, want ErrHeadlessGoalSession", err)
	}
}

func TestInProcessRunPromptClientUnregistersRuntimeAfterCompletion(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(containerDir, "workspace-a", workspace, sessioncontract.SessionCategorySubagent, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		startedOnce.Do(func() { close(started) })
		<-release
		modelstub.WriteCompletedResponseStream(w, "done", 1, 1)
	}))
	defer server.Close()

	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	cfg := config.App{
		WorkspaceRoot:   workspace,
		PersistenceRoot: root,
		Settings: testsetup.WriteProviderSettings(t, root, testsetup.WithResponsesProvider(config.Settings{
			Model:         "gpt-6-sol",
			ThinkingLevel: "medium",
			Shell:         config.ShellSettings{PostprocessingMode: config.ShellPostprocessingModeBuiltin},
		}, server.URL)),
	}
	authority, liveRunObserver := newObservedTestHeadlessRuntimeAuthority(t, root, store, nil, authManager, nil, persistence.Options()...)
	client := NewInProcessRunPromptClient(HeadlessBootstrap{
		SessionLaunch:    newTestHeadlessSessionLaunch(t, cfg, containerDir, authManager, persistence),
		RuntimeAuthority: authority,
		LiveRunObserver:  liveRunObserver,
	})

	done := make(chan error, 1)
	go func() {
		_, err := client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
			Intent: serverapi.OpenExistingSessionLaunchIntent(mustRunPromptSessionID(t, store.Meta().SessionID)),
			Prompt: "hello",
		}), nil)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for /responses request")
	}
	sessionID := mustRunPromptSessionID(t, store.Meta().SessionID)
	if _, active := authority.SessionExecution(sessionID); !active {
		t.Fatal("expected headless execution active while request is in flight")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunPrompt: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunPrompt did not finish")
	}
	if _, active := authority.SessionExecution(sessionID); active {
		t.Fatal("expected headless execution to finalize after completion")
	}
}

func TestHeadlessRunPromptOverridesRespectLockedModelContract(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.PersistenceRootEnvName, home)

	root := t.TempDir()
	workspace := t.TempDir()
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(containerDir, "workspace-a", workspace, sessioncontract.SessionCategorySubagent, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.MarkModelDispatchLocked(session.LockedContract{Model: "locked-model", EnabledTools: []string{string(toolspec.ToolExecCommand)}}); err != nil {
		t.Fatalf("mark model dispatch locked: %v", err)
	}

	requestBodies := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request payload: %v", err)
		}
		requestBodies <- payload
		modelstub.WriteCompletedResponseStream(w, "locked response", 1, 1)
	}))
	defer server.Close()

	authManager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)

	cfg, err := config.Load(workspace, workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.PersistenceRoot = root
	cfg.Settings.Model = "base-model"
	cfg.Settings = testsetup.WriteProviderSettings(t, cfg.PersistenceRoot, testsetup.WithResponsesProvider(cfg.Settings, server.URL))
	cfg.Settings.EnabledTools = map[toolspec.ID]bool{toolspec.ToolPatch: true}
	authority, liveRunObserver := newObservedTestHeadlessRuntimeAuthority(t, root, store, nil, authManager, nil, persistence.Options()...)
	client := NewInProcessRunPromptClient(HeadlessBootstrap{
		SessionLaunch:    newTestHeadlessSessionLaunch(t, cfg, containerDir, authManager, persistence),
		RuntimeAuthority: authority,
		LiveRunObserver:  liveRunObserver,
	})

	response, err := client.RunPrompt(context.Background(), requestfixture.RunPrompt(t, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(mustRunPromptSessionID(t, store.Meta().SessionID)),
		Prompt: "hello",
		Overrides: serverapi.RunPromptOverrides{
			Model: "override-model",
			Tools: "patch",
		},
	}), nil)
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}
	if response.GetFinalAnswer() != "locked response" {
		t.Fatalf("result = %q, want locked response", response.GetFinalAnswer())
	}
	select {
	case payload := <-requestBodies:
		if got := payload["model"]; got != "locked-model" {
			t.Fatalf("provider model = %#v, want locked-model", got)
		}
		toolsPayload, ok := payload["tools"].([]any)
		if !ok || len(toolsPayload) != 1 {
			t.Fatalf("expected one locked tool in request payload, got %#v", payload["tools"])
		}
		toolPayload, ok := toolsPayload[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected tool payload: %#v", toolsPayload[0])
		}
		if got := toolPayload["name"]; got != string(toolspec.ToolExecCommand) {
			t.Fatalf("expected locked shell tool, got %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for provider request payload")
	}
}
