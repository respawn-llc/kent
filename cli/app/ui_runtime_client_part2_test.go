package app

import (
	"context"
	"core/server/llm"
	"core/server/registry"
	"core/server/runtime"
	"core/server/runtimecontrol"
	"core/server/runtimeview"
	sharedclient "core/shared/client"
	"core/shared/clientui"
	"core/shared/serverapi"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRuntimeClientMainViewDoesNotRefreshCachedSnapshotBehindUIBack(t *testing.T) {
	reads := &countingSessionViewClient{view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}}}
	controls := sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(registry.NewRuntimeRegistry(), nil))
	runtimeClient := newTestSessionRuntimeClient(reads, controls)
	runtimeClient.storeMainView(clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}})
	notified := make(chan error, 1)
	runtimeClient.SetConnectionStateObserver(func(err error) {
		notified <- err
	})

	_ = runtimeClient.MainView()

	if got := reads.count.Load(); got != 0 {
		t.Fatalf("main view read count = %d, want 0", got)
	}
	select {
	case err := <-notified:
		t.Fatalf("did not expect synchronous main-view refresh notification, got %v", err)
	default:
	}
}

type leaseRetryRuntimeControlClient struct {
	mu              sync.Mutex
	firstSubmitErr  error
	appendErr       error
	compactErr      error
	compactCalls    int
	showGoalErr     error
	showGoalCalls   int
	queuedWorkErr   error
	queuedWork      bool
	queuedWorkCalls int
	submitLeaseID   []string
	submitRequestID []string
	queueRequestID  []string
	recordRequestID []string
	goalLeaseID     []string
	localEntries    []serverapi.RuntimeAppendCommittedEntryRequest
	showGoalResp    serverapi.RuntimeGoalShowResponse
	setGoalResp     serverapi.RuntimeGoalShowResponse
	pauseGoalResp   serverapi.RuntimeGoalShowResponse
	resumeGoalResp  serverapi.RuntimeGoalShowResponse
	clearGoalResp   serverapi.RuntimeGoalShowResponse
}

func (c *leaseRetryRuntimeControlClient) submitLeaseIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.submitLeaseID...)
}

func (c *leaseRetryRuntimeControlClient) submitRequestIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.submitRequestID...)
}

func (c *leaseRetryRuntimeControlClient) queueRequestIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.queueRequestID...)
}

func (c *leaseRetryRuntimeControlClient) recordRequestIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.recordRequestID...)
}

func (c *leaseRetryRuntimeControlClient) goalLeaseIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.goalLeaseID...)
}

func (c *leaseRetryRuntimeControlClient) resetGoalLeaseIDs() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.goalLeaseID = nil
}

func (c *leaseRetryRuntimeControlClient) appendedLocalEntries() []serverapi.RuntimeAppendCommittedEntryRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]serverapi.RuntimeAppendCommittedEntryRequest(nil), c.localEntries...)
}

func (c *leaseRetryRuntimeControlClient) SetSessionName(context.Context, serverapi.RuntimeSetSessionNameRequest) error {
	return nil
}

func (c *leaseRetryRuntimeControlClient) SetThinkingLevel(context.Context, serverapi.RuntimeSetThinkingLevelRequest) error {
	return nil
}

func (c *leaseRetryRuntimeControlClient) SetFastModeEnabled(context.Context, serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
	return serverapi.RuntimeSetFastModeEnabledResponse{}, nil
}

func (c *leaseRetryRuntimeControlClient) SetReviewerEnabled(context.Context, serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
	return serverapi.RuntimeSetReviewerEnabledResponse{}, nil
}

func (c *leaseRetryRuntimeControlClient) SetAutoCompactionEnabled(context.Context, serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
	return serverapi.RuntimeSetAutoCompactionEnabledResponse{}, nil
}

func (c *leaseRetryRuntimeControlClient) SetQuestionsEnabled(context.Context, serverapi.RuntimeSetQuestionsEnabledRequest) (serverapi.RuntimeSetQuestionsEnabledResponse, error) {
	return serverapi.RuntimeSetQuestionsEnabledResponse{}, nil
}

func (c *leaseRetryRuntimeControlClient) AppendCommittedEntry(_ context.Context, req serverapi.RuntimeAppendCommittedEntryRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.localEntries = append(c.localEntries, req)
	return c.appendErr
}

func (c *leaseRetryRuntimeControlClient) ShouldCompactBeforeUserMessage(context.Context, serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.compactCalls++
	if c.compactCalls == 1 && c.compactErr != nil {
		return serverapi.RuntimeShouldCompactBeforeUserMessageResponse{}, c.compactErr
	}
	return serverapi.RuntimeShouldCompactBeforeUserMessageResponse{}, nil
}

func (c *leaseRetryRuntimeControlClient) SubmitUserMessage(_ context.Context, req serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.submitLeaseID = append(c.submitLeaseID, req.ControllerLeaseID)
	c.submitRequestID = append(c.submitRequestID, req.ClientRequestID)
	switch req.ControllerLeaseID {
	case "lease-old":
		if c.firstSubmitErr != nil {
			return serverapi.RuntimeSubmitUserMessageResponse{}, c.firstSubmitErr
		}
		return serverapi.RuntimeSubmitUserMessageResponse{}, serverapi.ErrInvalidControllerLease
	case "lease-new":
		return serverapi.RuntimeSubmitUserMessageResponse{Message: "recovered"}, nil
	default:
		return serverapi.RuntimeSubmitUserMessageResponse{}, errors.New("unexpected controller lease")
	}
}

func (c *leaseRetryRuntimeControlClient) SubmitUserTurn(_ context.Context, req serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.submitLeaseID = append(c.submitLeaseID, req.ControllerLeaseID)
	c.submitRequestID = append(c.submitRequestID, req.ClientRequestID)
	switch req.ControllerLeaseID {
	case "lease-old":
		if c.firstSubmitErr != nil {
			return serverapi.RuntimeSubmitUserTurnResponse{}, c.firstSubmitErr
		}
		return serverapi.RuntimeSubmitUserTurnResponse{}, serverapi.ErrInvalidControllerLease
	case "lease-new":
		return serverapi.RuntimeSubmitUserTurnResponse{Message: "recovered"}, nil
	default:
		return serverapi.RuntimeSubmitUserTurnResponse{}, errors.New("unexpected controller lease")
	}
}

func (c *leaseRetryRuntimeControlClient) SubmitUserShellCommand(context.Context, serverapi.RuntimeSubmitUserShellCommandRequest) error {
	return nil
}

func (c *leaseRetryRuntimeControlClient) CompactContext(context.Context, serverapi.RuntimeCompactContextRequest) error {
	return nil
}

func (c *leaseRetryRuntimeControlClient) CompactContextForPreSubmit(context.Context, serverapi.RuntimeCompactContextForPreSubmitRequest) error {
	return nil
}

func (c *leaseRetryRuntimeControlClient) HasQueuedUserWork(context.Context, serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queuedWorkCalls++
	if c.queuedWorkCalls == 1 && c.queuedWorkErr != nil {
		return serverapi.RuntimeHasQueuedUserWorkResponse{}, c.queuedWorkErr
	}
	return serverapi.RuntimeHasQueuedUserWorkResponse{HasQueuedUserWork: c.queuedWork}, nil
}

func (c *leaseRetryRuntimeControlClient) SubmitQueuedUserMessages(context.Context, serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
	return serverapi.RuntimeSubmitQueuedUserMessagesResponse{}, nil
}

func (c *leaseRetryRuntimeControlClient) Interrupt(context.Context, serverapi.RuntimeInterruptRequest) error {
	return nil
}

func (c *leaseRetryRuntimeControlClient) QueueUserMessage(_ context.Context, req serverapi.RuntimeQueueUserMessageRequest) (serverapi.RuntimeQueueUserMessageResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queueRequestID = append(c.queueRequestID, req.ClientRequestID)
	switch req.ControllerLeaseID {
	case "lease-old":
		return serverapi.RuntimeQueueUserMessageResponse{}, serverapi.ErrInvalidControllerLease
	case "lease-new":
		return serverapi.RuntimeQueueUserMessageResponse{QueueItemID: "queue-1", Text: req.Text}, nil
	default:
		return serverapi.RuntimeQueueUserMessageResponse{}, errors.New("unexpected controller lease")
	}
}

func (c *leaseRetryRuntimeControlClient) DiscardQueuedUserMessage(context.Context, serverapi.RuntimeDiscardQueuedUserMessageRequest) (serverapi.RuntimeDiscardQueuedUserMessageResponse, error) {
	return serverapi.RuntimeDiscardQueuedUserMessageResponse{}, nil
}

func (c *leaseRetryRuntimeControlClient) RecordPromptHistory(_ context.Context, req serverapi.RuntimeRecordPromptHistoryRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recordRequestID = append(c.recordRequestID, req.ClientRequestID)
	switch req.ControllerLeaseID {
	case "lease-old":
		return serverapi.ErrInvalidControllerLease
	case "lease-new":
		return nil
	default:
		return errors.New("unexpected controller lease")
	}
}

func (c *leaseRetryRuntimeControlClient) ShowGoal(context.Context, serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.showGoalCalls++
	if c.showGoalCalls == 1 && c.showGoalErr != nil {
		return serverapi.RuntimeGoalShowResponse{}, c.showGoalErr
	}
	return c.showGoalResp, nil
}

func (c *leaseRetryRuntimeControlClient) SetGoal(_ context.Context, req serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return c.goalWriteResponse(req.ControllerLeaseID, c.setGoalResp)
}

func (c *leaseRetryRuntimeControlClient) PauseGoal(_ context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return c.goalWriteResponse(req.ControllerLeaseID, c.pauseGoalResp)
}

func (c *leaseRetryRuntimeControlClient) ResumeGoal(_ context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return c.goalWriteResponse(req.ControllerLeaseID, c.resumeGoalResp)
}

func (c *leaseRetryRuntimeControlClient) CompleteGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}

func (c *leaseRetryRuntimeControlClient) ClearGoal(_ context.Context, req serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return c.goalWriteResponse(req.ControllerLeaseID, c.clearGoalResp)
}

func (c *leaseRetryRuntimeControlClient) goalWriteResponse(leaseID string, resp serverapi.RuntimeGoalShowResponse) (serverapi.RuntimeGoalShowResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.goalLeaseID = append(c.goalLeaseID, leaseID)
	if leaseID == "lease-old" {
		return serverapi.RuntimeGoalShowResponse{}, serverapi.ErrInvalidControllerLease
	}
	return resp, nil
}

func TestRuntimeClientGoalMethodsPatchCachedMainView(t *testing.T) {
	showGoal := &serverapi.RuntimeGoal{ID: "goal-show", Objective: "show goal", Status: "paused", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	setGoal := &serverapi.RuntimeGoal{ID: "goal-set", Objective: "set goal", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	pauseGoal := &serverapi.RuntimeGoal{ID: "goal-pause", Objective: "pause goal", Status: "paused", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	resumeGoal := &serverapi.RuntimeGoal{ID: "goal-resume", Objective: "resume goal", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	controls := &leaseRetryRuntimeControlClient{
		showGoalResp:   serverapi.RuntimeGoalShowResponse{Goal: showGoal},
		setGoalResp:    serverapi.RuntimeGoalShowResponse{Goal: setGoal},
		pauseGoalResp:  serverapi.RuntimeGoalShowResponse{Goal: pauseGoal},
		resumeGoalResp: serverapi.RuntimeGoalShowResponse{Goal: resumeGoal},
		clearGoalResp:  serverapi.RuntimeGoalShowResponse{},
	}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	leaseManager := newControllerLeaseManager("lease-old")
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) { return "lease-new", nil })
	runtimeClient.SetControllerLeaseManager(leaseManager)

	goal, err := runtimeClient.ShowGoal()
	if err != nil {
		t.Fatalf("ShowGoal: %v", err)
	}
	assertRuntimeClientGoalCached(t, runtimeClient, goal, runtimeGoalFromAPI(showGoal))
	assertRuntimeGoalConversionDropsAPITimestamps(t, goal, showGoal)

	for _, tt := range []struct {
		name string
		call func() (*clientui.RuntimeGoal, error)
		want *serverapi.RuntimeGoal
	}{
		{name: "set", call: func() (*clientui.RuntimeGoal, error) { return runtimeClient.SetGoal("set goal") }, want: setGoal},
		{name: "pause", call: runtimeClient.PauseGoal, want: pauseGoal},
		{name: "resume", call: runtimeClient.ResumeGoal, want: resumeGoal},
		{name: "clear", call: runtimeClient.ClearGoal, want: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			leaseManager.Set("lease-old")
			controls.resetGoalLeaseIDs()
			goal, err := tt.call()
			if err != nil {
				t.Fatalf("%s goal: %v", tt.name, err)
			}
			if got := controls.goalLeaseIDs(); !reflect.DeepEqual(got, []string{"lease-old", "lease-new"}) {
				t.Fatalf("%s goal lease ids = %+v, want [lease-old lease-new]", tt.name, got)
			}
			assertRuntimeClientGoalCached(t, runtimeClient, goal, runtimeGoalFromAPI(tt.want))
		})
	}
}

func TestCloneRuntimeGoalReturnsIndependentCopy(t *testing.T) {
	original := &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship", Status: clientui.RuntimeGoalStatusActive, Suspended: true}
	cloned := cloneRuntimeGoal(original)
	original.ID = "goal-2"
	original.Objective = "mutated"
	original.Status = clientui.RuntimeGoalStatusPaused
	original.Suspended = false

	want := &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship", Status: clientui.RuntimeGoalStatusActive, Suspended: true}
	if !reflect.DeepEqual(cloned, want) {
		t.Fatalf("clone = %+v, want %+v", cloned, want)
	}
}

func assertRuntimeClientGoalCached(t *testing.T, runtimeClient *sessionRuntimeClient, got *clientui.RuntimeGoal, want *clientui.RuntimeGoal) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("goal = %+v, want %+v", got, want)
	}
	view, ok := runtimeClient.CachedMainView()
	if !ok {
		t.Fatal("expected cached main view")
	}
	if !reflect.DeepEqual(view.Status.Goal, want) {
		t.Fatalf("cached goal = %+v, want %+v", view.Status.Goal, want)
	}
}

func assertRuntimeGoalConversionDropsAPITimestamps(t *testing.T, got *clientui.RuntimeGoal, source *serverapi.RuntimeGoal) {
	t.Helper()
	if source == nil || source.CreatedAt.IsZero() || source.UpdatedAt.IsZero() {
		t.Fatal("test source goal must include timestamps")
	}
	if got == nil || got.ID != source.ID || got.Objective != source.Objective || string(got.Status) != source.Status || got.Suspended != source.Suspended {
		t.Fatalf("converted goal = %+v, source = %+v", got, source)
	}
}

func TestRuntimeClientSubmitUserMessageRecoversInvalidControllerLease(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	leaseManager := newControllerLeaseManager("lease-old")
	recoveryCalls := 0
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		recoveryCalls++
		return "lease-new", nil
	})
	runtimeClient.SetControllerLeaseManager(leaseManager)

	message, err := runtimeClient.SubmitUserMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	if message != "recovered" {
		t.Fatalf("SubmitUserMessage message = %q, want recovered", message)
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	if got := runtimeClient.controllerLeaseIDValue(); got != "lease-new" {
		t.Fatalf("controller lease id = %q, want lease-new", got)
	}
	if got := controls.submitLeaseIDs(); !reflect.DeepEqual(got, []string{"lease-old", "lease-new"}) {
		t.Fatalf("submit lease ids = %+v, want [lease-old lease-new]", got)
	}
	if got := controls.submitRequestIDs(); len(got) != 2 || got[0] == "" || got[0] != got[1] {
		t.Fatalf("submit request ids = %+v, want same non-empty id across retry", got)
	}
}

func TestRuntimeClientQueueUserMessageReusesRequestIDAcrossLeaseRecovery(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	leaseManager := newControllerLeaseManager("lease-old")
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) { return "lease-new", nil })
	runtimeClient.SetControllerLeaseManager(leaseManager)

	item, err := runtimeClient.QueueUserMessage("queued")
	if err != nil {
		t.Fatalf("QueueUserMessage: %v", err)
	}
	if item.ID != "queue-1" || item.Text != "queued" {
		t.Fatalf("queued item = %+v, want server response", item)
	}
	if got := controls.queueRequestIDs(); len(got) != 2 || got[0] == "" || got[0] != got[1] {
		t.Fatalf("queue request ids = %+v, want same non-empty id across retry", got)
	}
}

func TestRuntimeClientRecordPromptHistoryReusesRequestIDAcrossLeaseRecovery(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	leaseManager := newControllerLeaseManager("lease-old")
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) { return "lease-new", nil })
	runtimeClient.SetControllerLeaseManager(leaseManager)

	if err := runtimeClient.RecordPromptHistory("/status"); err != nil {
		t.Fatalf("RecordPromptHistory: %v", err)
	}
	if got := controls.recordRequestIDs(); len(got) != 2 || got[0] == "" || got[0] != got[1] {
		t.Fatalf("record request ids = %+v, want same non-empty id across retry", got)
	}
}

func TestRuntimeClientSubmitUserMessageRecoversRuntimeUnavailable(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{firstSubmitErr: serverapi.ErrRuntimeUnavailable}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	leaseManager := newControllerLeaseManager("lease-old")
	recoveryCalls := 0
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		recoveryCalls++
		return "lease-new", nil
	})
	runtimeClient.SetControllerLeaseManager(leaseManager)

	message, err := runtimeClient.SubmitUserMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	if message != "recovered" {
		t.Fatalf("SubmitUserMessage message = %q, want recovered", message)
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	if got := runtimeClient.controllerLeaseIDValue(); got != "lease-new" {
		t.Fatalf("controller lease id = %q, want lease-new", got)
	}
	if got := controls.submitLeaseIDs(); !reflect.DeepEqual(got, []string{"lease-old", "lease-new"}) {
		t.Fatalf("submit lease ids = %+v, want [lease-old lease-new]", got)
	}
	entries := controls.appendedLocalEntries()
	if len(entries) != 1 {
		t.Fatalf("warning entry count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.ControllerLeaseID != "lease-new" || entry.Role != "warning" || entry.Text != runtimeLeaseRecoveryWarningText || entry.Visibility != string(clientui.EntryVisibilityAll) {
		t.Fatalf("warning entry = %+v, want new lease warning", entry)
	}
}

func TestRuntimeClientSubmitTurnRecoveryContinuesFirstPrompt(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{firstSubmitErr: serverapi.ErrRuntimeUnavailable}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	leaseManager := newControllerLeaseManager("lease-old")
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		return "lease-new", nil
	})
	runtimeClient.SetControllerLeaseManager(leaseManager)
	model := newProjectedClosedUIModel(runtimeClient)
	model.startupCmds = nil

	submitCmd := model.inputController().startSubmissionWithPromptHistoryAndQueuePositionAndID("hello after restart", preSubmitQueueBack, "")
	if submitCmd == nil {
		t.Fatal("expected submit command")
	}
	next := tea.Model(model)
	updated := next.(*uiModel)
	submitMsgs := collectCmdMessages(t, submitCmd)
	var done submitDoneMsg
	foundDone := false
	for _, msg := range submitMsgs {
		if typed, ok := msg.(submitDoneMsg); ok {
			done = typed
			foundDone = true
		}
	}
	if !foundDone {
		t.Fatalf("expected submit result, got %+v", submitMsgs)
	}
	if done.err != nil || done.message != "recovered" {
		t.Fatalf("submit result = %+v, want recovered first prompt", done)
	}
	next, _ = updated.Update(done)
	updated = next.(*uiModel)
	if updated.activity == uiActivityError {
		t.Fatal("did not expect pre-submit recovery to surface operator error")
	}
	plain := stripANSIAndTrimRight(updated.view.OngoingSnapshot())
	if strings.Contains(plain, serverapi.ErrRuntimeUnavailable.Error()) || strings.Contains(plain, "runtime for session") || strings.Contains(plain, runtimeLeaseRecoveryWarningText) {
		t.Fatalf("did not expect recovery diagnostics in ongoing transcript, got %q", plain)
	}
}

func TestRuntimeClientHydrationRecoversRuntimeUnavailableSilently(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{}
	authoritativePage := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     4,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "authoritative"}},
	}
	reads := &flakySessionViewClient{
		errs:  []error{serverapi.ErrRuntimeUnavailable, nil},
		pages: []serverapi.SessionTranscriptPageResponse{{}, {Transcript: authoritativePage}},
	}
	runtimeClient := newTestSessionRuntimeClient(reads, controls)
	leaseManager := newControllerLeaseManager("lease-old")
	recoveryCalls := 0
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		recoveryCalls++
		return "lease-new", nil
	})
	runtimeClient.SetControllerLeaseManager(leaseManager)

	page, err := runtimeClient.RefreshTranscriptPage(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail})
	if err != nil {
		t.Fatalf("RefreshTranscriptPage: %v", err)
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	if reads.count != 2 {
		t.Fatalf("transcript read count = %d, want 2", reads.count)
	}
	if page.Revision != authoritativePage.Revision || len(page.Entries) != 1 || page.Entries[0].Text != "authoritative" {
		t.Fatalf("hydrated page = %+v, want %+v", page, authoritativePage)
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 0 {
		t.Fatalf("did not expect visible recovery warning during hydration, got %+v", entries)
	}
}

func TestRuntimeClientMainViewRefreshRecoversRuntimeUnavailableSilently(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{}
	authoritativeView := clientui.RuntimeMainView{
		Session: clientui.RuntimeSessionView{SessionID: "session-1", SessionName: "restored"},
		Status:  clientui.RuntimeStatus{ThinkingLevel: "high"},
	}
	reads := &flakySessionViewClient{
		errs:      []error{serverapi.ErrRuntimeUnavailable, nil},
		responses: []serverapi.SessionMainViewResponse{{}, {MainView: authoritativeView}},
	}
	runtimeClient := newTestSessionRuntimeClient(reads, controls)
	leaseManager := newControllerLeaseManager("lease-old")
	recoveryCalls := 0
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		recoveryCalls++
		return "lease-new", nil
	})
	runtimeClient.SetControllerLeaseManager(leaseManager)

	view, err := runtimeClient.RefreshMainView()
	if err != nil {
		t.Fatalf("RefreshMainView: %v", err)
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	if reads.count != 2 {
		t.Fatalf("main-view read count = %d, want 2", reads.count)
	}
	if view.Session.SessionName != "restored" || view.Status.ThinkingLevel != "high" {
		t.Fatalf("main view = %+v, want %+v", view, authoritativeView)
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 0 {
		t.Fatalf("did not expect visible recovery warning during main-view refresh, got %+v", entries)
	}
}

func TestRuntimeUnavailableHydrationRecoveryResumesOngoingEventFence(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{}
	authoritativePage := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     5,
		TotalEntries: 1,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "hydrated"}},
	}
	reads := &flakySessionViewClient{
		errs:  []error{serverapi.ErrRuntimeUnavailable, nil},
		pages: []serverapi.SessionTranscriptPageResponse{{}, {Transcript: authoritativePage}},
	}
	runtimeClient := newTestSessionRuntimeClient(reads, controls)
	leaseManager := newControllerLeaseManager("lease-old")
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		return "lease-new", nil
	})
	runtimeClient.SetControllerLeaseManager(leaseManager)
	runtimeEvents := make(chan clientui.Event, 1)
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "after hydrate"}
	model := newProjectedRuntimeEventsUIModel(runtimeClient, runtimeEvents)
	model.startupCmds = nil
	model.waitRuntimeEventAfterHydration = true

	cmd := model.startRuntimeTranscriptSyncRequest(runtimeTranscriptSyncRequestForPage(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, false, runtimeTranscriptSyncCauseContinuityRecovery, clientui.TranscriptRecoveryCauseStreamGap)).cmd
	if cmd == nil {
		t.Fatal("expected hydration command")
	}
	rawMsg := cmd()
	msg, ok := rawMsg.(runtimeTranscriptRefreshedMsg)
	if !ok {
		t.Fatalf("expected runtimeTranscriptRefreshedMsg, got %T", rawMsg)
	}
	if msg.err != nil {
		t.Fatalf("hydration err = %v, want recovered nil", msg.err)
	}

	next, resumeCmd := model.Update(msg)
	updated := next.(*uiModel)
	if updated.waitRuntimeEventAfterHydration {
		t.Fatal("expected recovered hydration to release runtime event fence")
	}
	if updated.runtimeTranscriptBusy {
		t.Fatal("expected recovered hydration to clear in-flight busy flag")
	}
	msgs := collectCmdMessages(t, resumeCmd)
	resumed := false
	for _, collected := range msgs {
		if typed, ok := collected.(runtimeEventBatchMsg); ok && len(typed.events) == 1 && typed.events[0].AssistantDelta == "after hydrate" {
			resumed = true
		}
		if _, ok := collected.(runtimeTranscriptRetryMsg); ok {
			t.Fatalf("did not expect retry after successful runtime-unavailable recovery, got %+v", msgs)
		}
	}
	if !resumed {
		t.Fatalf("expected runtime event consumption to resume after recovered hydration, got %+v", msgs)
	}
	if len(runtimeEvents) != 0 {
		t.Fatalf("expected resumed runtime wait to consume pending event, remaining=%d", len(runtimeEvents))
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 0 {
		t.Fatalf("did not expect visible recovery warning during UI hydration, got %+v", entries)
	}
}

func TestRuntimeClientShowGoalRecoversRuntimeUnavailableSilently(t *testing.T) {
	goal := &serverapi.RuntimeGoal{ID: "goal-1", Objective: "ship", Status: "active"}
	controls := &leaseRetryRuntimeControlClient{
		showGoalErr:  serverapi.ErrRuntimeUnavailable,
		showGoalResp: serverapi.RuntimeGoalShowResponse{Goal: goal},
	}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	leaseManager := newControllerLeaseManager("lease-old")
	recoveryCalls := 0
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		recoveryCalls++
		return "lease-new", nil
	})
	runtimeClient.SetControllerLeaseManager(leaseManager)

	got, err := runtimeClient.ShowGoal()
	if err != nil {
		t.Fatalf("ShowGoal: %v", err)
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	if controls.showGoalCalls != 2 {
		t.Fatalf("show goal call count = %d, want 2", controls.showGoalCalls)
	}
	if got == nil || got.ID != "goal-1" || got.Objective != "ship" || got.Status != clientui.RuntimeGoalStatusActive {
		t.Fatalf("goal = %+v, want recovered active goal", got)
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 0 {
		t.Fatalf("did not expect visible recovery warning during goal read, got %+v", entries)
	}
}

func TestRuntimeClientHasQueuedUserWorkRecoversRuntimeUnavailableSilently(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{
		queuedWorkErr: serverapi.ErrRuntimeUnavailable,
		queuedWork:    true,
	}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	leaseManager := newControllerLeaseManager("lease-old")
	recoveryCalls := 0
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		recoveryCalls++
		return "lease-new", nil
	})
	runtimeClient.SetControllerLeaseManager(leaseManager)

	hasWork, err := runtimeClient.HasQueuedUserWork()
	if err != nil {
		t.Fatalf("HasQueuedUserWork: %v", err)
	}
	if !hasWork {
		t.Fatal("HasQueuedUserWork = false, want true")
	}
	if recoveryCalls != 1 {
		t.Fatalf("recovery call count = %d, want 1", recoveryCalls)
	}
	if controls.queuedWorkCalls != 2 {
		t.Fatalf("queued-work call count = %d, want 2", controls.queuedWorkCalls)
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 0 {
		t.Fatalf("did not expect visible recovery warning during queued-work read, got %+v", entries)
	}
}

func TestRuntimeClientLeaseRecoveryWarningFailureDoesNotBlockSubmit(t *testing.T) {
	controls := &leaseRetryRuntimeControlClient{firstSubmitErr: serverapi.ErrRuntimeUnavailable, appendErr: serverapi.ErrRuntimeUnavailable}
	runtimeClient := newTestSessionRuntimeClientWithControls(controls)
	warnings := make(chan runtimeLeaseRecoveryWarningMsg, 1)
	runtimeClient.SetLeaseRecoveryWarningObserver(func(text string, visibility clientui.EntryVisibility) {
		warnings <- runtimeLeaseRecoveryWarningMsg{text: text, visibility: visibility}
	})
	leaseManager := newControllerLeaseManager("lease-old")
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) { return "lease-new", nil })
	runtimeClient.SetControllerLeaseManager(leaseManager)

	message, err := runtimeClient.SubmitUserMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	if message != "recovered" {
		t.Fatalf("SubmitUserMessage message = %q, want recovered", message)
	}
	if got := controls.submitLeaseIDs(); !reflect.DeepEqual(got, []string{"lease-old", "lease-new"}) {
		t.Fatalf("submit lease ids = %+v, want [lease-old lease-new]", got)
	}
	if entries := controls.appendedLocalEntries(); len(entries) != 1 {
		t.Fatalf("warning append attempts = %d, want 1", len(entries))
	}
	select {
	case warning := <-warnings:
		if warning.text != runtimeLeaseRecoveryWarningText || warning.visibility != clientui.EntryVisibilityAll {
			t.Fatalf("warning = %+v, want lease recovery warning", warning)
		}
	default:
		t.Fatal("expected warning fallback notification")
	}
}

func TestRuntimeClientServerRestartFirstPromptRecoversAndWarnsOngoing(t *testing.T) {
	runtimeEvents := make(chan clientui.Event, 128)
	store := createAppRuntimeSessionAt(t, t.TempDir(), "workspace-x", t.TempDir())
	client := &runtimeClientFakeLLM{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine := newAppRuntimeEngineWithStore(t, store, client, runtime.Config{
		OnEvent: func(evt runtime.Event) {
			runtimeEvents <- runtimeview.EventFromRuntime(evt)
		},
	})
	resolver := &mutableRuntimeResolver{}
	controls := sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(resolver, nil))
	runtimeClient := newUIRuntimeClientWithReads(store.Meta().SessionID, &countingSessionViewClient{}, controls).(*sessionRuntimeClient)
	leaseManager := newControllerLeaseManager("lease-old")
	leaseManager.SetRecoverFunc(func(context.Context) (string, error) {
		resolver.Set(engine)
		return "lease-new", nil
	})
	runtimeClient.SetControllerLeaseManager(leaseManager)
	model := newProjectedClosedUIModel(nil)
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = sized.(*uiModel)

	message, err := runtimeClient.SubmitUserMessage(context.Background(), "hello after restart")
	if err != nil {
		t.Fatalf("submitRuntimeUserMessage: %v", err)
	}
	if message != "done" {
		t.Fatalf("submitRuntimeUserMessage message = %q, want done", message)
	}

	updated := model
	eventCount := 0
	flushText := ""
	for len(runtimeEvents) > 0 {
		msg := <-runtimeEvents
		eventCount++
		next, cmd := updated.Update(runtimeEventMsg{event: msg})
		updated = next.(*uiModel)
		flushText += collectNativeHistoryFlushText(collectCmdMessages(t, cmd))
	}
	if !strings.Contains(flushText, runtimeLeaseRecoveryWarningText) {
		t.Fatalf("expected ongoing warning flush, events=%d entries=%+v flush=%q", eventCount, updated.transcriptEntries, flushText)
	}
	if strings.Contains(flushText, "runtime for session") {
		t.Fatalf("did not expect runtime unavailable error in ongoing flush, got %q", flushText)
	}
}
