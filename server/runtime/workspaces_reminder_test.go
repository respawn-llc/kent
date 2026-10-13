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
	"core/server/workflowruntime"
	"core/shared/sessioncontract"
	"core/shared/transcript"
)

func TestFreshModelRequestIncludesProjectWorkspacesContext(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 2)
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

	requestReminders := workspaceReminderMessages(requestMessages(client.calls[0]))
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
	assertModelCallCount(t, client, 1)
	if reminders := workspaceReminderMessages(requestMessages(client.calls[0])); len(reminders) != 0 {
		t.Fatalf("request Workspaces reminders = %d, want none", len(reminders))
	}
	for _, entry := range engine.ChatSnapshot().Entries {
		if entry.MessageType == llm.MessageTypeWorkspaces {
			t.Fatal("single-Workspace Session committed a Workspaces reminder")
		}
	}
}

func TestProjectWorkspaceContextUsesBoundedCatalogOrderAndWorkspaceDefault(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 6)
	projectID, err := metadataStore.ResolveSessionProjectID(t.Context(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("resolve Session Project: %v", err)
	}
	catalog, err := metadataStore.ListProjectWorkspaceCatalogPage(t.Context(), projectID, 0, 10)
	if err != nil {
		t.Fatalf("list complete Workspace catalog: %v", err)
	}

	got, err := loadProjectWorkspacesContext(t.Context(), metadataStore, store.Meta().SessionID)
	if err != nil {
		t.Fatalf("load Project Workspace context: %v", err)
	}
	if got.ProjectName == "" || got.WorkspaceCount != int64(len(catalog.Workspaces)) {
		t.Fatalf("Workspace context facts = %+v, want project name and total attachment count", got)
	}
	if len(got.Workspaces) != metadata.ProjectWorkspaceContextLimit {
		t.Fatalf("context Workspaces = %d, want bounded first page of %d", len(got.Workspaces), metadata.ProjectWorkspaceContextLimit)
	}
	for index, want := range catalog.Workspaces[:metadata.ProjectWorkspaceContextLimit] {
		if got.Workspaces[index] != want {
			t.Fatalf("context Workspace %d = %+v, want catalog row %+v", index, got.Workspaces[index], want)
		}
	}
}

func TestFreshWorktreeModelRequestIncludesProjectWorkspacesContext(t *testing.T) {
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
	if _, err := engine.SubmitUserMessage(t.Context(), "start in the Worktree"); err != nil {
		t.Fatalf("submit first message: %v", err)
	}

	var skillIndex, workspaceIndex, worktreeIndex int
	var hasSkill, hasWorkspace, hasWorktree bool
	for index, message := range requestMessages(client.calls[0]) {
		if message.MessageType == nil {
			continue
		}
		switch *message.MessageType {
		case llm.MessageTypeSkills:
			skillIndex = index
			hasSkill = true
		case llm.MessageTypeWorkspaces:
			if hasWorkspace {
				t.Fatal("fresh Worktree request contains duplicate Workspaces reminders")
			}
			workspaceIndex = index
			hasWorkspace = true
		case llm.MessageTypeWorktreeMode:
			worktreeIndex = index
			hasWorktree = true
		}
	}
	if !hasWorkspace || !hasWorktree || worktreeIndex <= workspaceIndex ||
		(hasSkill && skillIndex >= workspaceIndex) {
		t.Fatalf(
			"fresh Worktree context order = Skills:%d Workspaces:%d Worktree:%d, want Workspaces after Skills and before Worktree",
			skillIndex,
			workspaceIndex,
			worktreeIndex,
		)
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

func TestProjectWorkspacesReadFailurePreventsWorkflowModelDispatch(t *testing.T) {
	metadataStore, store, _ := newWorkspaceReminderSession(t, 2)
	if err := metadataStore.Close(); err != nil {
		t.Fatalf("close metadata store to exercise read failure: %v", err)
	}

	client := &fakeClient{responses: []llm.Response{finalOutputItemResponse("ready")}}
	engine := mustNewWorkflowTestEngine(
		t,
		store,
		client,
		&workflowruntime.CurrentNodeExecutionConfig{
			CompletionMode: workflowruntime.CompletionModeTool,
			Controller:     &externallyCompletedWorkflowController{},
		},
		Config{ProjectWorkspaceContextReader: metadataStore},
	)
	if _, err := engine.SubmitWorkflowTurn(t.Context()); err == nil {
		t.Fatal("Workflow model request succeeded without Project Workspace context facts")
	}
	if len(client.calls) != 0 {
		t.Fatalf("Workflow model calls = %d, want none after context read failure", len(client.calls))
	}
	if reminders := workspaceReminderMessages(engine.transcriptRuntimeState().SnapshotMessages()); len(reminders) != 0 {
		t.Fatalf("committed Workflow Workspaces reminders after failed preparation = %d, want none", len(reminders))
	}
}

func TestFreshPostCompactionRequestIncludesProjectWorkspacesContext(t *testing.T) {
	metadataStore, store, attached := newWorkspaceReminderSession(t, 2)
	worktreeRoot := t.TempDir()
	mustSetWorktreeReminderState(t, store, testWorktreeReminderState(
		session.WorktreeReminderModeEnter,
		"feature/post-compaction-context",
		worktreeRoot,
		attached[0].CanonicalRoot,
		worktreeRoot,
	))
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
	worktreeReminders := 0
	for _, message := range requestMessages(client.calls[1]) {
		if message.MessageType != nil && *message.MessageType == llm.MessageTypeWorktreeMode {
			worktreeReminders++
		}
	}
	if worktreeReminders != 1 {
		t.Fatalf("first post-compaction request Worktree reminders = %d, want one", worktreeReminders)
	}
	if reminders := workspaceReminderMessages(engine.transcriptRuntimeState().SnapshotMessages()); len(reminders) != 1 {
		t.Fatalf("active post-compaction Workspaces reminders = %d, want one", len(reminders))
	}
	activeWorktreeReminders := 0
	for _, message := range engine.transcriptRuntimeState().SnapshotMessages() {
		if message.MessageType != nil && *message.MessageType == llm.MessageTypeWorktreeMode {
			activeWorktreeReminders++
		}
	}
	if activeWorktreeReminders != 1 {
		t.Fatalf("active post-compaction Worktree reminders = %d, want one", activeWorktreeReminders)
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

func TestLaterDormantWorkflowAssignmentDoesNotRefreshProjectWorkspacesContext(t *testing.T) {
	metadataStore, store, attached := newWorkspaceReminderSession(t, 2)
	deliveryContext := PersistedWorkflowAssignmentContext{
		Workdir:                       attached[0].CanonicalRoot,
		Model:                         "gpt-6-sol",
		ProjectWorkspaceContextReader: metadataStore,
	}
	first, err := SteerPersistedWorkflowAssignment(
		t.Context(),
		store,
		workflowAssignmentForCompactionTest(),
		deliveryContext,
	)
	if err != nil {
		t.Fatalf("prepare initial dormant Workflow assignment: %v", err)
	}
	if _, err := first.Wait(t.Context()); err != nil {
		t.Fatalf("commit initial dormant Workflow assignment: %v", err)
	}
	initialReminders := persistedWorkspaceReminderRecords(t, store)
	if len(initialReminders) != 1 {
		t.Fatalf("initial dormant Workspaces reminders = %d, want one", len(initialReminders))
	}

	projectID, err := metadataStore.ResolveSessionProjectID(t.Context(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("resolve Session Project: %v", err)
	}
	if _, err := metadataStore.AttachWorkspaceToProject(t.Context(), projectID, t.TempDir()); err != nil {
		t.Fatalf("attach Workspace before later assignment: %v", err)
	}
	later, err := SteerPersistedWorkflowAssignment(
		t.Context(),
		store,
		workflowAssignmentForCompactionTest(),
		deliveryContext,
	)
	if err != nil {
		t.Fatalf("prepare later dormant Workflow assignment: %v", err)
	}
	if _, err := later.Wait(t.Context()); err != nil {
		t.Fatalf("commit later dormant Workflow assignment: %v", err)
	}
	laterReminders := persistedWorkspaceReminderRecords(t, store)
	if len(laterReminders) != 1 || messageContent(llm.Message{
		Content: laterReminders[0].Content,
	}) != messageContent(llm.Message{Content: initialReminders[0].Content}) {
		t.Fatalf("later dormant Workspaces context = %+v, want the initial reminder reused", laterReminders)
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

func persistedWorkspaceReminderRecords(t *testing.T, store *session.Store) []session.MessageRecord {
	t.Helper()
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize Session event log: %v", err)
	}
	window, err := eventLog.ReadRecentRecords(128)
	if err != nil {
		t.Fatalf("read recent Session records: %v", err)
	}
	reminders := make([]session.MessageRecord, 0, 1)
	for _, event := range window.Records {
		payload, err := event.Payload()
		if err != nil {
			t.Fatalf("read Session event payload: %v", err)
		}
		message, ok := payload.(session.MessageRecord)
		if ok && message.MessageType != nil && *message.MessageType == session.MessageTypeWorkspaces {
			reminders = append(reminders, message)
		}
	}
	return reminders
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

	project, err := metadataStore.CreateProjectForWorkspace(t.Context(), t.TempDir(), "Workspace Context Project")
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
