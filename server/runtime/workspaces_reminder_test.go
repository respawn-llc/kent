package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"core/server/llm"
	"core/server/metadata"
	"core/server/session"
	"core/server/tools"
	"core/shared/sessioncontract"
	"core/shared/transcript"
)

func TestFreshModelRequestIncludesProjectWorkspacesContext(t *testing.T) {
	metadataStore, store, attached := newWorkspaceReminderSession(t, 2)
	worktreeRoot := t.TempDir()
	mustSetWorktreeReminderState(t, store, testWorktreeReminderState(
		session.WorktreeReminderModeEnter,
		"feature/workspace-context",
		worktreeRoot,
		attached[0].CanonicalRoot,
		worktreeRoot,
	))
	client := &fakeClient{responses: []llm.Response{finalOutputItemResponse("ready")}}
	engine := mustNewTestEngine(
		t,
		store,
		client,
		tools.NewRegistry(),
		Config{ProjectWorkspaceContextReader: metadataStore},
	)

	if _, err := engine.SubmitUserMessage(t.Context(), "start"); err != nil {
		t.Fatalf("submit first message: %v", err)
	}
	assertModelCallCount(t, client, 1)

	request := requestMessages(client.calls[0])
	requestReminders := workspaceReminderMessages(request)
	if len(requestReminders) != 1 {
		t.Fatalf("request Workspaces reminders = %d, want one", len(requestReminders))
	}
	if messageContent(requestReminders[0]) == "" {
		t.Fatal("request Workspaces reminder has no content")
	}
	transcriptReminders := make([]ChatEntry, 0, 1)
	for _, entry := range engine.ChatSnapshot().Entries {
		if entry.MessageType == llm.MessageTypeWorkspaces {
			transcriptReminders = append(transcriptReminders, entry)
		}
	}
	if len(transcriptReminders) != 1 {
		t.Fatalf("committed transcript Workspaces reminders = %d, want one", len(transcriptReminders))
	}
	if transcriptReminders[0].Visibility != transcript.EntryVisibilityDetail {
		t.Fatalf("Workspaces reminder visibility = %q, want detail", transcriptReminders[0].Visibility)
	}
	var workspaceIndex, worktreeIndex int
	var hasWorkspace, hasWorktree bool
	for index, message := range request {
		if message.MessageType == nil {
			continue
		}
		switch *message.MessageType {
		case llm.MessageTypeWorkspaces:
			workspaceIndex = index
			hasWorkspace = true
		case llm.MessageTypeWorktreeMode:
			worktreeIndex = index
			hasWorktree = true
		}
	}
	if !hasWorkspace || !hasWorktree || worktreeIndex <= workspaceIndex {
		t.Fatalf("fresh Worktree context order = Workspaces:%d Worktree:%d", workspaceIndex, worktreeIndex)
	}
}

func TestFreshModelRequestOmitsProjectWorkspacesContextForSingleWorkspace(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 1)
	client := &fakeClient{responses: []llm.Response{finalOutputItemResponse("ready")}}
	engine := mustNewTestEngine(
		t,
		store,
		client,
		tools.NewRegistry(),
		Config{ProjectWorkspaceContextReader: metadataStore},
	)
	if _, err := engine.SubmitUserMessage(t.Context(), "start"); err != nil {
		t.Fatalf("submit first message: %v", err)
	}
	if reminders := workspaceReminderMessages(requestMessages(client.calls[0])); len(reminders) != 0 {
		t.Fatalf("request Workspaces reminders = %d, want none", len(reminders))
	}
}

func TestProjectWorkspaceContextOmitsUnavailableFirstFiveWithoutBackfill(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 6)
	projectID, err := metadataStore.ResolveSessionProjectID(t.Context(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("resolve Session Project: %v", err)
	}
	page, err := metadataStore.ListProjectWorkspaceCatalogPage(
		t.Context(),
		projectID,
		0,
		metadata.ProjectWorkspaceContextLimit,
	)
	if err != nil {
		t.Fatalf("list first catalog page: %v", err)
	}
	missing := page.Workspaces[1].CanonicalRoot
	if err := os.RemoveAll(missing); err != nil {
		t.Fatalf("remove first unavailable Workspace fixture: %v", err)
	}
	notDirectory := page.Workspaces[2].CanonicalRoot
	if err := os.RemoveAll(notDirectory); err != nil {
		t.Fatalf("remove non-directory Workspace fixture: %v", err)
	}
	if err := os.WriteFile(notDirectory, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("replace Workspace path with a file: %v", err)
	}
	lookupFailure := page.Workspaces[3].CanonicalRoot
	if err := os.Rename(lookupFailure, lookupFailure+".retained"); err != nil {
		t.Fatalf("move Workspace directory for lookup failure: %v", err)
	}
	if err := os.Symlink(lookupFailure, lookupFailure); err != nil {
		t.Fatalf("create Workspace symlink loop: %v", err)
	}

	got, err := loadProjectWorkspacesContext(t.Context(), metadataStore, store.Meta().SessionID)
	if err != nil {
		t.Fatalf("load Project Workspace context: %v", err)
	}
	if got.WorkspaceCount != 6 {
		t.Fatalf("Workspace count after unavailable paths = %d, want all 6 attachments", got.WorkspaceCount)
	}
	want := []metadata.ProjectWorkspaceCatalogRow{page.Workspaces[0], page.Workspaces[4]}
	if len(got.Workspaces) != len(want) {
		t.Fatalf("available Workspace rows = %d, want %d without backfilling", len(got.Workspaces), len(want))
	}
	for index, workspace := range want {
		if got.Workspaces[index] != workspace {
			t.Fatalf("available Workspace row %d = %+v, want %+v", index, got.Workspaces[index], workspace)
		}
	}
}

func TestProjectWorkspacesReadFailurePreventsModelDispatch(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 2)
	if err := metadataStore.Close(); err != nil {
		t.Fatalf("close metadata store to exercise read failure: %v", err)
	}

	client := &fakeClient{responses: []llm.Response{finalOutputItemResponse("ready")}}
	engine := mustNewTestEngine(
		t,
		store,
		client,
		tools.NewRegistry(),
		Config{ProjectWorkspaceContextReader: metadataStore},
	)
	if _, err := engine.SubmitUserMessage(t.Context(), "start"); err == nil {
		t.Fatal("model request succeeded without Project Workspace context facts")
	}
	assertModelCallCount(t, client, 0)
	if reminders := workspaceReminderMessages(engine.transcriptRuntimeState().SnapshotMessages()); len(reminders) != 0 {
		t.Fatalf("committed Workspaces reminders after failed preparation = %d, want none", len(reminders))
	}
}

func TestFreshPostCompactionRequestIncludesProjectWorkspacesContext(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 2)
	client := &fakeCompactionClient{
		responses: []llm.Response{
			finalOutputItemResponse("ready"),
			finalOutputItemResponse("after compaction"),
		},
		compactionResponses: []llm.CompactionResponse{
			remoteCompactionReplacement(1_000, 100, 200_000),
		},
	}
	engine := mustNewTestEngine(
		t,
		store,
		client,
		tools.NewRegistry(),
		Config{
			Model:                         "gpt-6-astra",
			ThinkingLevel:                 "medium",
			CompactionMode:                "native",
			ProjectWorkspaceContextReader: metadataStore,
		},
	)
	if _, err := engine.SubmitUserMessage(t.Context(), "start"); err != nil {
		t.Fatalf("submit first message: %v", err)
	}
	scheduleManualCompactionAndWait(t, engine)
	if _, err := engine.SubmitUserMessage(t.Context(), "after compaction"); err != nil {
		t.Fatalf("submit after compaction: %v", err)
	}

	if len(client.calls) != 2 {
		t.Fatalf("model calls = %d, want two", len(client.calls))
	}
	if reminders := workspaceReminderMessages(requestMessages(client.calls[1])); len(reminders) != 1 {
		t.Fatalf("first post-compaction request Workspaces reminders = %d, want one", len(reminders))
	}
	if reminders := workspaceReminderMessages(engine.transcriptRuntimeState().SnapshotMessages()); len(reminders) != 1 {
		t.Fatalf("active post-compaction Workspaces reminders = %d, want one", len(reminders))
	}
}

func TestProjectWorkspacesContextIsReusedForProviderRetry(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 2)
	client := &fakeCompactionClient{
		errors:    []error{errors.New("temporary provider failure")},
		responses: []llm.Response{finalOutputItemResponse("ready")},
	}
	engine := mustNewTestEngine(
		t,
		store,
		client,
		tools.NewRegistry(),
		Config{ProjectWorkspaceContextReader: metadataStore},
	)
	if _, err := engine.SubmitUserMessage(t.Context(), "start"); err != nil {
		t.Fatalf("submit first message: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("provider calls = %d, want failed attempt plus retry", len(client.calls))
	}
	first := workspaceReminderMessages(requestMessages(client.calls[0]))
	retry := workspaceReminderMessages(requestMessages(client.calls[1]))
	if len(first) != 1 || len(retry) != 1 {
		t.Fatalf("provider retry Workspaces reminders = %d then %d, want one each", len(first), len(retry))
	}
	if messageContent(first[0]) != messageContent(retry[0]) {
		t.Fatal("provider retry refreshed the prepared Workspaces reminder")
	}
	if reminders := workspaceReminderMessages(engine.transcriptRuntimeState().SnapshotMessages()); len(reminders) != 1 {
		t.Fatalf("active Workspaces reminders after retry = %d, want one", len(reminders))
	}
}

func TestReopenedSessionReusesProjectWorkspacesContext(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 2)
	firstClient := &fakeClient{responses: []llm.Response{finalOutputItemResponse("ready")}}
	engine := mustNewTestEngine(
		t,
		store,
		firstClient,
		tools.NewRegistry(),
		Config{ProjectWorkspaceContextReader: metadataStore},
	)
	if _, err := engine.SubmitUserMessage(t.Context(), "start"); err != nil {
		t.Fatalf("submit first message: %v", err)
	}
	first := workspaceReminderMessages(requestMessages(firstClient.calls[0]))
	if len(first) != 1 {
		t.Fatalf("first request Workspaces reminders = %d, want one", len(first))
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close Engine: %v", err)
	}

	reopenedStore := mustOpenTestSession(t, store.Dir())
	reopenedClient := &fakeClient{responses: []llm.Response{finalOutputItemResponse("resumed")}}
	reopenedEngine := mustNewTestEngine(
		t,
		reopenedStore,
		reopenedClient,
		tools.NewRegistry(),
		Config{ProjectWorkspaceContextReader: metadataStore},
	)
	if _, err := reopenedEngine.SubmitUserMessage(t.Context(), "resume"); err != nil {
		t.Fatalf("submit after reopening: %v", err)
	}
	reopened := workspaceReminderMessages(requestMessages(reopenedClient.calls[0]))
	if len(reopened) != 1 || messageContent(reopened[0]) != messageContent(first[0]) {
		t.Fatalf("reopened request Workspaces context = %+v, want the prepared reminder reused", reopened)
	}
	if reminders := workspaceReminderMessages(reopenedEngine.transcriptRuntimeState().SnapshotMessages()); len(reminders) != 1 {
		t.Fatalf("active Workspaces reminders after reopening = %d, want one", len(reminders))
	}
}

func TestFreshDormantWorkflowAssignmentFailsBeforeContextCommitWhenWorkspaceReadFails(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 2)
	if err := metadataStore.Close(); err != nil {
		t.Fatalf("close metadata store to exercise read failure: %v", err)
	}
	_, err := SteerPersistedWorkflowAssignment(
		t.Context(),
		store,
		workflowAssignmentForCompactionTest(),
		PersistedWorkflowAssignmentContext{
			ProjectWorkspaceContextReader: metadataStore,
		},
	)
	if err == nil {
		t.Fatal("fresh dormant Workflow assignment succeeded without Project Workspace context facts")
	}
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize Session event log: %v", err)
	}
	window, err := eventLog.ReadRecentRecords(1)
	if err != nil {
		t.Fatalf("read Session event log after failed assignment: %v", err)
	}
	if len(window.Records) != 0 {
		t.Fatalf("fresh dormant Session records after failed Workspace read = %d, want none", len(window.Records))
	}
}

func newWorkspaceReminderSession(
	t *testing.T,
	workspaceCount int,
) (*metadata.Store, *session.Store, []metadata.Binding) {
	t.Helper()
	persistenceRoot := t.TempDir()
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		if err := metadataStore.Close(); err != nil {
			t.Errorf("close metadata store: %v", err)
		}
	})

	project, err := metadataStore.CreateProjectForWorkspaceWithKey(t.Context(), t.TempDir(), "Workspace Context Project", "WCP")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	attached := []metadata.Binding{project}
	for range workspaceCount - 1 {
		workspace, err := metadataStore.AttachWorkspaceToProject(t.Context(), project.ProjectID, t.TempDir())
		if err != nil {
			t.Fatalf("attach Workspace: %v", err)
		}
		attached = append(attached, workspace)
	}

	container := filepath.Join(persistenceRoot, "projects", project.ProjectID, "sessions")
	store, err := session.Create(
		container,
		filepath.Base(container),
		project.CanonicalRoot,
		sessioncontract.SessionCategoryMain,
		runtimeTestSessionPersistence.Options()...,
	)
	if err != nil {
		t.Fatalf("create Session: %v", err)
	}
	if err := metadataStore.ImportSessionSnapshot(t.Context(), session.PersistedStoreSnapshot{
		SessionDir:   store.Dir(),
		Meta:         store.Meta(),
		ContextFacts: store.ContextFacts(),
	}); err != nil {
		t.Fatalf("import Session metadata: %v", err)
	}
	return metadataStore, store, attached
}

func workspaceReminderMessages(messages []llm.Message) []llm.Message {
	var reminders []llm.Message
	for _, message := range messages {
		if message.MessageType != nil && *message.MessageType == llm.MessageTypeWorkspaces {
			reminders = append(reminders, message)
		}
	}
	return reminders
}
