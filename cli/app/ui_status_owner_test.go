package app

import (
	"context"
	"io"
	"testing"
	"time"

	"core/shared/config"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	settingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStatusDestinationRejectsPreviousSessionCompletion(t *testing.T) {
	model := newProjectedStaticUIModel(WithUIStatusCollector(&stubProgressiveStatusCollector{
		gitResult: uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "previous"}},
	}))
	messages := collectCmdMessages(t, model.statusRefreshCmd())
	model.setKnownSessionTarget(runtimeids.NewSessionID().String())
	for _, message := range messages {
		model.Update(message)
	}
	if model.status.snapshot.Git.Visible {
		t.Fatal("previous Session's Git result reached the current destination")
	}
}

func TestStatusOpenReloadsWhenExecutionDestinationChanges(t *testing.T) {
	root := t.TempDir()
	target := &worktreepb.SessionExecutionTarget{WorkspaceRoot: root, EffectiveWorkdir: root}
	client := &runtimeControlFakeClient{sessionView: &runtimepb.SessionView{
		SessionId: detailTestSessionID, ExecutionTarget: target,
	}}
	collector := &stubProgressiveStatusCollector{
		gitResult: uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "previous"}},
	}
	model := newProjectedTestUIModel(client,
		WithUISessionID(detailTestSessionID),
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: root}),
		WithUIStatusCollector(collector))
	defer model.Close()
	old := statusReadMessages(t, model.inputController().startStatusFlowCmd())
	for _, message := range old {
		model.Update(message)
	}
	target.EffectiveWorkdir = t.TempDir()
	collector.gitResult.Git.Branch = "current"
	collector.base.Workdir = target.EffectiveWorkdir
	_, command := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if model.status.snapshot.Git.Visible || !model.status.open {
		t.Fatal("destination change retained old data or closed the overlay")
	}
	current := statusReadMessages(t, command)
	if len(current) == 0 {
		t.Fatal("open overlay did not automatically reload the new destination")
	}
	for _, message := range old {
		model.Update(message)
	}
	for _, message := range current {
		model.Update(message)
	}
	if model.status.snapshot.Git.Branch != "current" || model.status.snapshot.Workdir != target.EffectiveWorkdir ||
		model.surfaceHasPendingOperation() {
		t.Fatal("new execution destination did not finish its own status reads")
	}
}

func TestStatusAdoptionWaitsForInstalledObservationConfiguration(t *testing.T) {
	collector := &stubProgressiveStatusCollector{}
	model := newProjectedStaticUIModel(
		WithUIStatusCollector(collector),
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: t.TempDir()}))
	defer model.Close()
	model.inputController().startStatusFlowCmd()
	id := runtimeids.NewSessionID()
	model.observeSession = func(context.Context, runtimeids.SessionID) (sessionLaunchPlan, *runtimeLaunchPlan, error) {
		t.Fatal("observation should be installed explicitly in this test")
		return sessionLaunchPlan{}, nil, nil
	}
	model.pendingNewChatActions = 1
	_, command := model.Update(newSessionActionDoneMsg{origin: model, chatActionResult: chatActionResult{
		session: &chatpb.ExistingSessionTarget{SessionId: id.String()},
	}})
	_ = command // Leave observation installation outstanding.
	if !model.status.open || model.status.snapshot.Workdir != "" {
		t.Fatal("adoption did not discard the previous destination immediately")
	}
	if messages := statusReadMessages(t, model.statusRefreshCmd()); len(messages) != 0 {
		t.Fatal("status read the new Session using the previous observation's configuration")
	}
	root := t.TempDir()
	collector.base = uiStatusSnapshot{SessionID: id.String(), Workdir: root}
	client := newUIRuntimeClientWithReads(id.String(), &countingSessionViewClient{view: &runtimepb.MainView{
		Session: &runtimepb.SessionView{SessionId: id.String()}, Status: &runtimepb.Status{},
	}})
	// Installation schedules independent long-lived observers as well as status
	// reads. Check the immediate status projection without starting observers.
	_, command = model.Update(sessionObservationLoadedMsg{
		scope: model.observationScope, sessionID: id,
		observation: &runtimeLaunchPlan{Wiring: &runtimeWiring{
			runtimeClient: client, eventDispatcher: newUIEventDispatcher(nil), terminalFocus: newTerminalFocusState(),
		}},
		plan: sessionLaunchPlan{StatusConfig: uiStatusConfig{
			WorkspaceRoot: root, Settings: config.Settings{ModelContextWindow: 12345},
		}},
	})
	if model.status.snapshot.Workdir != root || model.status.snapshot.Context.WindowTokens != 12345 ||
		!model.layout().statusSectionLoading(uiStatusSectionAuth) {
		t.Fatal("installed observation did not reload status with its own configuration")
	}
	_ = command
}

func TestStatusFailedAdoptionDoesNotReadPreviousConfiguration(t *testing.T) {
	model := newProjectedStaticUIModel(
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: t.TempDir()}),
		WithUIStatusCollector(&stubProgressiveStatusCollector{}))
	defer model.Close()
	old := statusReadMessages(t, model.inputController().startStatusFlowCmd())
	id := runtimeids.NewSessionID()
	model.observeSession = func(context.Context, runtimeids.SessionID) (sessionLaunchPlan, *runtimeLaunchPlan, error) {
		return sessionLaunchPlan{}, nil, io.ErrUnexpectedEOF
	}
	model.pendingNewChatActions = 1
	model.Update(newSessionActionDoneMsg{origin: model, chatActionResult: chatActionResult{
		session: &chatpb.ExistingSessionTarget{SessionId: id.String()},
	}})
	model.Update(sessionObservationLoadedMsg{
		scope: model.observationScope, sessionID: id, err: io.ErrUnexpectedEOF,
	})
	for _, message := range old {
		model.Update(message)
	}
	if model.sessionObservationErr == nil || model.status.snapshot.Workdir != "" ||
		len(statusReadMessages(t, model.statusRefreshCmd())) != 0 {
		t.Fatal("failed observation exposed or read the previous configuration")
	}
}

func TestStatusObservationInstallationKeepsSameDestinationWork(t *testing.T) {
	statusConfig := uiStatusConfig{WorkspaceRoot: t.TempDir()}
	model := newProjectedStaticUIModel(
		WithUIStatusConfig(statusConfig),
		WithUIStatusCollector(&stubProgressiveStatusCollector{
			gitResult: uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "startup"}},
		}))
	defer model.Close()
	startup := statusReadMessages(t, model.statusLineGitRefreshCmd())
	client := newUIRuntimeClientWithReads(detailTestSessionID, &countingSessionViewClient{view: &runtimepb.MainView{
		Session: &runtimepb.SessionView{SessionId: detailTestSessionID}, Status: &runtimepb.Status{},
	}})
	model.installSessionObservation(&runtimeWiring{
		runtimeClient: client, eventDispatcher: newUIEventDispatcher(nil), terminalFocus: newTerminalFocusState(),
	}, sessionLaunchPlan{StatusConfig: statusConfig})
	for _, message := range startup {
		model.Update(message)
	}
	if model.status.snapshot.Git.Branch != "startup" || model.layout().statusSectionLoading(uiStatusSectionGit) {
		t.Fatal("installing the same destination discarded its outstanding Git read")
	}
}

func TestStatusStartupCollectsGitOnceAfterObservationInstallation(t *testing.T) {
	statusConfig := uiStatusConfig{WorkspaceRoot: t.TempDir()}
	collector := &stubProgressiveStatusCollector{}
	search := newStubUIPathReferenceSearch()
	close(search.events)
	model := newProjectedStaticUIModel(
		WithUIStatusConfig(statusConfig),
		WithUIStatusCollector(collector),
		WithUIPathReferenceSearch(search))
	defer model.Close()
	scope, stopObservation := context.WithCancel(t.Context())
	defer stopObservation()
	model.observationScope = scope
	client := newUIRuntimeClientWithReads(detailTestSessionID, &countingSessionViewClient{view: &runtimepb.MainView{
		Session: &runtimepb.SessionView{SessionId: detailTestSessionID}, Status: &runtimepb.Status{},
	}})
	model.startupCmds = append(model.startupCmds, model.installSessionObservation(&runtimeWiring{
		runtimeClient: client, eventDispatcher: newUIEventDispatcher(nil), terminalFocus: newTerminalFocusState(),
	}, sessionLaunchPlan{StatusConfig: statusConfig}))
	startup := model.Init()
	// End long-lived observation delivery before draining the startup commands.
	// Status reads have their own lifetime and must execute independently.
	stopObservation()
	statusReadMessages(t, startup)
	if collector.gitCalls != 1 {
		t.Fatalf("startup Git reads = %d, want one", collector.gitCalls)
	}
}

func TestStatusNewSessionSettingsRefreshKeepsOutstandingReads(t *testing.T) {
	fixture := startRemoteAuthTestFixture(t)
	server, err := ensureInteractiveProjectBinding(t.Context(), fixture.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	request, err := prepareNewSessionUIRun(t.Context(), server, nil, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := composeUIProgram(request, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.close()
	model := composition.model
	collector := &stubProgressiveStatusCollector{
		gitResult: uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "outstanding"}},
	}
	WithUIStatusCollector(collector)(model)
	if len(statusReadMessages(t, model.inputController().startStatusFlowCmd())) != 0 ||
		model.statusLineGitRefreshCmd() != nil {
		t.Fatal("New Session status read configuration before initial defaults were ready")
	}
	_, initial := model.Update(newSessionSettingsReadResult(t, model.loadNewSessionSettingsCmd(nil)))
	outstanding := statusReadMessages(t, initial)
	outstanding = append(outstanding, statusReadMessages(t, model.inputController().startStatusFlowCmd())...)
	model.Update(newSessionSettingsReadResult(t, model.loadNewSessionSettingsCmd(nil)))
	for _, message := range outstanding {
		model.Update(message)
	}
	if model.status.snapshot.Git.Branch != "outstanding" || model.surfaceHasPendingOperation() {
		t.Fatal("identical New Session settings refresh discarded outstanding status reads")
	}
	collector.authResult.Auth.Provider = "previous-agent"
	previous := statusReadMessages(t, model.statusRefreshCmd())
	selection := model.mutateNewSessionSettings(&settingspb.MutationOperation{
		Operation: &settingspb.MutationOperation_AgentRole{AgentRole: "fast"},
	})
	if model.status.snapshot.Git.Visible || model.statusRefreshCmd() != nil {
		t.Fatal("changing the New Session Agent used its predecessor's configuration")
	}
	collector.authResult.Auth.Provider = "current-agent"
	_, installed := model.Update(newSessionSettingsReadResult(t, selection))
	for _, message := range previous {
		model.Update(message)
	}
	if model.status.snapshot.Auth.Provider == "previous-agent" {
		t.Fatal("previous Agent's pending read reached the new configuration")
	}
	for _, message := range statusReadMessages(t, installed) {
		model.Update(message)
	}
	if model.status.snapshot.Auth.Provider != "current-agent" || model.surfaceHasPendingOperation() {
		t.Fatal("new Agent's configuration did not finish loading")
	}
}

func TestStatusReturningToSessionRejectsPreviousVisitAndCacheWrites(t *testing.T) {
	collector := &stubProgressiveStatusCollector{
		gitResult: uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "previous-visit"}},
		envResult: uiStatusEnvironmentStageResult{AgentsPaths: []string{"/previous/AGENTS.md"}},
	}
	model := newProjectedStaticUIModel(
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: t.TempDir()}),
		WithUIStatusCollector(collector),
	)
	defer model.Close()
	old := statusReadMessages(t, model.statusLineGitRefreshCmd())
	old = append(old, statusReadMessages(t, model.inputController().startStatusFlowCmd())...)
	model.setKnownSessionTarget(runtimeids.NewSessionID().String())
	model.setKnownSessionTarget(detailTestSessionID)
	collector.gitResult.Git.Branch = "current-visit"
	collector.envResult.AgentsPaths = []string{"/current/AGENTS.md"}
	current := statusReadMessages(t, model.statusRefreshCmd())
	for _, message := range old {
		model.Update(message)
	}
	if model.status.snapshot.Git.Visible || len(model.status.snapshot.AgentsPaths) != 0 ||
		!model.layout().statusSectionLoading(uiStatusSectionGit) ||
		!model.layout().statusSectionLoading(uiStatusSectionEnvironment) {
		t.Fatal("previous visit changed current values or outstanding reads")
	}
	request := model.newStatusRequest(time.Now())
	cached := model.statusRepository.SeedSnapshot(request, uiStatusSnapshot{}, request.CurrentTime)
	if cached.Snapshot.Git.Visible || len(cached.Snapshot.AgentsPaths) != 0 {
		t.Fatal("previous visit populated the destination's cache")
	}
	for _, message := range current {
		model.Update(message)
	}
	if model.status.snapshot.Git.Branch != "current-visit" || model.surfaceHasPendingOperation() {
		t.Fatal("current visit did not finish normally")
	}
}

func TestStatusHiddenCompletionSurvivesReopen(t *testing.T) {
	collector := &stubProgressiveStatusCollector{
		authResult: uiStatusAuthStageResult{Auth: uiStatusAuthInfo{Visible: true, Provider: "known"}},
		gitResult:  uiStatusGitStageResult{Git: uiStatusGitInfo{Visible: true, Branch: "known"}},
		envResult:  uiStatusEnvironmentStageResult{AgentsPaths: []string{"/known/AGENTS.md"}},
	}
	model := newProjectedStaticUIModel(WithUIStatusCollector(collector), WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: t.TempDir()}))
	defer model.Close()
	messages := statusReadMessages(t, model.statusLineGitRefreshCmd())
	messages = append(messages, statusReadMessages(t, model.inputController().startStatusFlowCmd())...)
	model.inputController().stopStatusFlowCmd()
	for _, message := range messages {
		model.Update(message)
	}
	if model.status.open || model.status.snapshot.Git.Branch != "known" {
		t.Fatal("hidden completion did not retain its result without reopening")
	}
	next := statusReadMessages(t, model.inputController().startStatusFlowCmd())
	if model.status.snapshot.Git.Branch != "known" || model.status.snapshot.Auth.Provider != "known" ||
		len(model.status.snapshot.AgentsPaths) != 1 {
		t.Fatal("reopening discarded completed sections")
	}
	if model.layout().statusSectionLoading(uiStatusSectionGit) ||
		model.layout().statusSectionLoading(uiStatusSectionEnvironment) ||
		!model.layout().statusSectionLoading(uiStatusSectionAuth) {
		t.Fatal("reopening did not reuse fresh cache and reload Auth")
	}
	for _, message := range next {
		model.Update(message)
	}
	if model.surfaceHasPendingOperation() {
		t.Fatal("reopened status remained loading after all reads completed")
	}
}

func statusReadMessages(t *testing.T, command tea.Cmd) []tea.Msg {
	t.Helper()
	var messages []tea.Msg
	for _, message := range collectCmdMessages(t, command) {
		switch message.(type) {
		case statusBaseRefreshDoneMsg, statusAuthRefreshDoneMsg, statusGitRefreshDoneMsg, statusEnvironmentRefreshDoneMsg:
			messages = append(messages, message)
		}
	}
	return messages
}

func TestStatusOverlappingRefreshKeepsSectionsPending(t *testing.T) {
	collector := &stubProgressiveStatusCollector{authResult: uiStatusAuthStageResult{Auth: uiStatusAuthInfo{Provider: "older"}}}
	model := newProjectedStaticUIModel(WithUIStatusCollector(collector))
	first := collectCmdMessages(t, model.statusRefreshCmd())
	collector.authResult.Auth.Provider = "newer"
	second := collectCmdMessages(t, model.statusRefreshCmd())
	for _, message := range second {
		model.Update(message)
	}
	if !model.layout().statusSectionLoading(uiStatusSectionAuth) {
		t.Fatal("first completion hid outstanding Auth work")
	}
	for _, message := range first {
		model.Update(message)
	}
	if model.layout().statusSectionLoading(uiStatusSectionAuth) {
		t.Fatal("Auth remained pending after all reads completed")
	}
	if model.status.snapshot.Auth.Provider != "older" {
		t.Fatal("older same-destination read was not allowed to finish after the newer read")
	}
}

func TestStatusStartupGitCannotFinishForegroundGit(t *testing.T) {
	model := newProjectedStaticUIModel(
		WithUIStatusConfig(uiStatusConfig{WorkspaceRoot: t.TempDir()}),
		WithUIStatusCollector(&stubProgressiveStatusCollector{}),
	)
	defer model.Close()
	startup := statusReadMessages(t, model.statusLineGitRefreshCmd())
	foreground := statusReadMessages(t, model.inputController().startStatusFlowCmd())
	for _, message := range startup {
		model.Update(message)
	}
	if !model.layout().statusSectionLoading(uiStatusSectionGit) || !model.surfaceHasPendingOperation() {
		t.Fatal("startup Git completion hid the outstanding foreground read")
	}
	for _, message := range foreground {
		model.Update(message)
	}
	if model.surfaceHasPendingOperation() {
		t.Fatal("status remained loading after startup and foreground reads completed")
	}
}

func TestStatusOpenStartsLoadingAnimationBeforeAnyReadCompletes(t *testing.T) {
	model := newProjectedStaticUIModel(WithUIStatusCollector(&stubProgressiveStatusCollector{}))
	for _, message := range collectCmdMessages(t, model.inputController().startStatusFlowCmd()) {
		if _, ok := message.(spinnerTickMsg); ok {
			return
		}
	}
	t.Fatal("opening status did not schedule loading animation before read completion")
}

func TestStatusLateBasePreservesCompletedEmptySectionsAndWarnings(t *testing.T) {
	collector := &stubProgressiveStatusCollector{
		base: uiStatusSnapshot{
			Git:         uiStatusGitInfo{Visible: true, Branch: "obsolete"},
			Auth:        uiStatusAuthInfo{Visible: true},
			AgentsPaths: []string{"/obsolete/AGENTS.md"},
		},
		envResult: uiStatusEnvironmentStageResult{CollectorWarning: "inspection failed"},
	}
	model := newProjectedStaticUIModel(WithUIStatusCollector(collector))
	messages := statusReadMessages(t, model.inputController().startStatusFlowCmd())
	var base tea.Msg
	for _, message := range messages {
		if _, ok := message.(statusBaseRefreshDoneMsg); ok {
			base = message
		} else {
			model.Update(message)
		}
	}
	model.Update(base)
	if model.status.snapshot.Git.Visible || model.status.snapshot.Auth.Visible ||
		len(model.status.snapshot.AgentsPaths) != 0 || model.status.snapshot.CollectorWarning == "" {
		t.Fatal("late base completion replaced completed empty sections or their warning")
	}
	model.inputController().stopStatusFlowCmd()
	model.inputController().startStatusFlowCmd()
	if model.status.snapshot.CollectorWarning == "" {
		t.Fatal("reopening hid the previous inspection failure before a replacement completed")
	}
}
