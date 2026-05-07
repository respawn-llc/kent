package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"builder/server/runtime"
	"builder/server/runtimecontrol"
	"builder/server/runtimeview"
	"builder/server/sessionview"
	"builder/shared/client"
	"builder/shared/clientui"
	"builder/shared/serverapi"
	"builder/shared/transcriptdiag"
	"github.com/google/uuid"
)

const uiRuntimeControlTimeout = 3 * time.Second
const uiRuntimeHydrationReadTimeout = 10 * time.Second
const runtimeLeaseRecoveryWarningText = "Lost connection to the session runtime; reconnected."

var uiRuntimeReadTimeout = 300 * time.Millisecond

type sessionRuntimeClient struct {
	reads                   client.SessionViewClient
	controls                client.RuntimeControlClient
	sessionID               string
	controllerLease         *controllerLeaseManager
	diagLogf                func(string)
	transcriptDiagnostics   bool
	connectionStateObserver func(error)
	leaseRecoveryWarning    func(string, clientui.EntryVisibility)

	mu                   sync.RWMutex
	mainView             clientui.RuntimeMainView
	hasMainView          bool
	suffixRPCUnsupported bool
}

func newRuntimeClient(sessionID string, reads client.SessionViewClient, controls client.RuntimeControlClient) clientui.RuntimeClient {
	return newUIRuntimeClientWithReads(sessionID, reads, controls)
}

func newUIRuntimeClientFromEngine(engine *runtime.Engine) clientui.RuntimeClient {
	if engine == nil {
		return nil
	}
	resolver := sessionview.NewStaticRuntimeResolver(engine)
	reads := client.NewLoopbackSessionViewClient(sessionview.NewService(nil, resolver, nil))
	controls := client.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(resolver, nil))
	runtimeClient := newUIRuntimeClientWithReads(engine.SessionID(), reads, controls).(*sessionRuntimeClient)
	runtimeClient.storeMainView(runtimeview.MainViewFromRuntime(engine))
	return runtimeClient
}

func newUIRuntimeClient(engine *runtime.Engine) clientui.RuntimeClient {
	return newUIRuntimeClientFromEngine(engine)
}

func newUIRuntimeClientWithReads(sessionID string, reads client.SessionViewClient, controls client.RuntimeControlClient) clientui.RuntimeClient {
	if reads == nil || controls == nil {
		return nil
	}
	return &sessionRuntimeClient{
		sessionID:       sessionID,
		controllerLease: newControllerLeaseManager("local-ui-controller"),
		reads:           reads,
		controls:        controls,
		mainView:        clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: sessionID}},
	}
}

func (c *sessionRuntimeClient) SetControllerLeaseManager(manager *controllerLeaseManager) {
	if c == nil || manager == nil {
		return
	}
	c.mu.Lock()
	c.controllerLease = manager
	c.mu.Unlock()
}

func (c *sessionRuntimeClient) SetControllerLeaseID(leaseID string) {
	if c == nil {
		return
	}
	if manager := c.controllerLeaseManager(); manager != nil {
		manager.Set(leaseID)
	}
}

func (c *sessionRuntimeClient) controllerLeaseIDValue() string {
	if manager := c.controllerLeaseManager(); manager != nil {
		return manager.Value()
	}
	return ""
}

func (c *sessionRuntimeClient) controllerLeaseManager() *controllerLeaseManager {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.controllerLease
}

func (c *sessionRuntimeClient) recoverControllerLease(ctx context.Context, trigger error) error {
	return c.recoverControllerLeaseWithWarning(ctx, trigger, true)
}

func (c *sessionRuntimeClient) recoverControllerLeaseSilently(ctx context.Context, trigger error) error {
	return c.recoverControllerLeaseWithWarning(ctx, trigger, false)
}

func (c *sessionRuntimeClient) recoverControllerLeaseWithWarning(ctx context.Context, trigger error, appendWarning bool) error {
	manager := c.controllerLeaseManager()
	if manager == nil {
		return errControllerLeaseRecoveryUnavailable
	}
	leaseID, err := manager.Recover(ctx)
	if err != nil {
		return err
	}
	if appendWarning && isRecoverableRuntimeControlError(trigger) {
		c.appendLeaseRecoveryWarning(leaseID)
	}
	return nil
}

func (c *sessionRuntimeClient) appendLeaseRecoveryWarning(controllerLeaseID string) {
	if c == nil || c.controls == nil {
		return
	}
	warningCtx, cancel := c.controlContext()
	defer cancel()
	if err := c.controls.AppendLocalEntry(warningCtx, serverapi.RuntimeAppendLocalEntryRequest{
		ClientRequestID:   uuid.NewString(),
		SessionID:         c.sessionID,
		ControllerLeaseID: controllerLeaseID,
		Role:              "warning",
		Text:              runtimeLeaseRecoveryWarningText,
		Visibility:        string(clientui.EntryVisibilityAll),
	}); err != nil {
		c.notifyLeaseRecoveryWarning(runtimeLeaseRecoveryWarningText, clientui.EntryVisibilityAll)
	}
}

func isRecoverableRuntimeControlError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, serverapi.ErrInvalidControllerLease) || errors.Is(err, serverapi.ErrRuntimeUnavailable)
}

func (c *sessionRuntimeClient) retryControlCallNoResult(ctx context.Context, call func(controllerLeaseID string) error) error {
	_, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (struct{}, error) {
		return struct{}{}, call(controllerLeaseID)
	})
	return err
}

func retryRuntimeControlCall[T any](ctx context.Context, currentLeaseID func() string, recoverLease func(context.Context, error) error, call func(controllerLeaseID string) (T, error)) (T, error) {
	value, err := call(currentLeaseID())
	if !isRecoverableRuntimeControlError(err) {
		return value, err
	}
	var zero T
	if recoverErr := recoverLease(ctx, err); recoverErr != nil {
		return zero, recoverErr
	}
	return call(currentLeaseID())
}

func retryRuntimeUnavailableCall[T any](ctx context.Context, recoverLease func(context.Context, error) error, call func() (T, error)) (T, error) {
	value, err := call()
	if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return value, err
	}
	var zero T
	if recoverErr := recoverLease(ctx, err); recoverErr != nil {
		return zero, recoverErr
	}
	return call()
}

func (c *sessionRuntimeClient) SetTranscriptDiagnosticLogger(logf func(string)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.diagLogf = logf
}

func (c *sessionRuntimeClient) SetTranscriptDiagnosticsEnabled(enabled bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transcriptDiagnostics = enabled
	if enabled {
		return
	}
}

func (c *sessionRuntimeClient) SetConnectionStateObserver(observer func(error)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectionStateObserver = observer
}

func (c *sessionRuntimeClient) SetLeaseRecoveryWarningObserver(observer func(string, clientui.EntryVisibility)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.leaseRecoveryWarning = observer
}

func (c *sessionRuntimeClient) MainView() clientui.RuntimeMainView {
	view, hasView := c.cachedMainView()
	if !hasView {
		refreshed, err := c.refreshMainViewSync(uiRuntimeReadTimeout)
		if err == nil {
			return refreshed
		}
		return view
	}
	return view
}

func (c *sessionRuntimeClient) RefreshMainView() (clientui.RuntimeMainView, error) {
	return c.refreshMainViewSync(uiRuntimeHydrationReadTimeout)
}

func (c *sessionRuntimeClient) Transcript() clientui.TranscriptPage {
	return clientui.TranscriptPage{SessionID: c.sessionID}
}

func (c *sessionRuntimeClient) RefreshTranscript() (clientui.TranscriptPage, error) {
	return c.refreshTranscriptPageSync(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, uiRuntimeHydrationReadTimeout)
}

func (c *sessionRuntimeClient) RefreshTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return c.refreshTranscriptPageSync(req, uiRuntimeHydrationReadTimeout)
}

func (c *sessionRuntimeClient) LoadTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return c.refreshTranscriptPageSync(req, uiRuntimeHydrationReadTimeout)
}

func (c *sessionRuntimeClient) RefreshCommittedTranscriptSuffix(req clientui.CommittedTranscriptSuffixRequest) (clientui.CommittedTranscriptSuffix, error) {
	return c.refreshCommittedTranscriptSuffixSync(req, uiRuntimeHydrationReadTimeout)
}

func (c *sessionRuntimeClient) Status() clientui.RuntimeStatus {
	return c.MainView().Status
}

func (c *sessionRuntimeClient) SessionView() clientui.RuntimeSessionView {
	return c.MainView().Session
}

func (c *sessionRuntimeClient) controlContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), uiRuntimeControlTimeout)
}

func (c *sessionRuntimeClient) readContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = uiRuntimeReadTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (c *sessionRuntimeClient) cachedMainView() (clientui.RuntimeMainView, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	view := c.mainView
	if !c.hasMainView {
		return view, false
	}
	return view, true
}

func (c *sessionRuntimeClient) CachedMainView() (clientui.RuntimeMainView, bool) {
	if c == nil {
		return clientui.RuntimeMainView{}, false
	}
	return c.cachedMainView()
}

func (c *sessionRuntimeClient) storeMainView(view clientui.RuntimeMainView) clientui.RuntimeMainView {
	if view.Session.SessionID == "" {
		view.Session.SessionID = c.sessionID
	}
	c.mu.Lock()
	c.mainView = view
	c.hasMainView = true
	c.mu.Unlock()
	return view
}

func (c *sessionRuntimeClient) patchMainView(apply func(view *clientui.RuntimeMainView)) {
	c.mu.Lock()
	apply(&c.mainView)
	if c.mainView.Session.SessionID == "" {
		c.mainView.Session.SessionID = c.sessionID
	}
	c.hasMainView = true
	c.mu.Unlock()
}

func (c *sessionRuntimeClient) observeRuntimeEventStatus(evt clientui.Event) {
	if c == nil || evt.ContextUsage == nil {
		return
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.ContextUsage = *evt.ContextUsage
	})
}

func (c *sessionRuntimeClient) refreshMainViewSync(timeout time.Duration) (clientui.RuntimeMainView, error) {
	ctx, cancel := c.readContext(timeout)
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseSilently, func() (serverapi.SessionMainViewResponse, error) {
		return c.reads.GetSessionMainView(ctx, serverapi.SessionMainViewRequest{SessionID: c.sessionID})
	})
	c.notifyConnectionState(err)
	if err != nil {
		c.mu.Lock()
		view := c.mainView
		if view.Session.SessionID == "" {
			view.Session.SessionID = c.sessionID
		}
		c.mainView = view
		c.hasMainView = true
		c.mu.Unlock()
		return view, err
	}
	return c.storeMainView(resp.MainView), nil
}

func (c *sessionRuntimeClient) refreshTranscriptSync(timeout time.Duration) (clientui.TranscriptPage, error) {
	return c.refreshTranscriptPageSync(clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}, timeout)
}

func (c *sessionRuntimeClient) refreshTranscriptPageSync(req clientui.TranscriptPageRequest, timeout time.Duration) (clientui.TranscriptPage, error) {
	ctx, cancel := c.readContext(timeout)
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseSilently, func() (serverapi.SessionTranscriptPageResponse, error) {
		return c.reads.GetSessionTranscriptPage(ctx, serverapi.SessionTranscriptPageRequest{
			SessionID:                c.sessionID,
			Offset:                   req.Offset,
			Limit:                    req.Limit,
			Page:                     req.Page,
			PageSize:                 req.PageSize,
			Window:                   req.Window,
			KnownRevision:            req.KnownRevision,
			KnownCommittedEntryCount: req.KnownCommittedEntryCount,
		})
	})
	c.notifyConnectionState(err)
	if c.transcriptDiagnosticsEnabled() {
		fields := map[string]string{"session_id": c.sessionID, "path": "hydrate"}
		for key, value := range transcriptdiag.RequestFields(req) {
			fields[key] = value
		}
		if err != nil {
			fields["err"] = err.Error()
			c.logTranscriptDiag(transcriptdiag.FormatLine("transcript.diag.client.hydrate_fetch", fields))
		} else {
			c.logTranscriptDiag(transcriptdiag.FormatLine("transcript.diag.client.hydrate_fetch", transcriptdiag.AddPageFields(fields, resp.Transcript)))
		}
	}
	if err != nil {
		return clientui.TranscriptPage{SessionID: c.sessionID}, err
	}
	page := resp.Transcript
	if page.SessionID == "" {
		page.SessionID = c.sessionID
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Session.Transcript = clientui.TranscriptMetadata{
			Revision:            page.Revision,
			CommittedEntryCount: page.TotalEntries,
		}
		if isOngoingTailTranscriptRequest(req) {
			view.Session.Chat = clientui.ChatSnapshot{
				Entries:      cloneTranscriptEntries(page.Entries),
				Ongoing:      page.Ongoing,
				OngoingError: page.OngoingError,
			}
		}
	})
	return page, nil
}

func (c *sessionRuntimeClient) refreshCommittedTranscriptSuffixSync(req clientui.CommittedTranscriptSuffixRequest, timeout time.Duration) (clientui.CommittedTranscriptSuffix, error) {
	req = clientui.NormalizeCommittedTranscriptSuffixRequest(req)
	fallbackToPage := func() (clientui.CommittedTranscriptSuffix, error) {
		page, err := c.refreshTranscriptPageSync(clientui.TranscriptPageRequest{Offset: req.AfterEntryCount, Limit: req.Limit}, timeout)
		if err != nil {
			return clientui.CommittedTranscriptSuffix{SessionID: c.sessionID}, err
		}
		return committedTranscriptSuffixFromPage(page), nil
	}
	suffixClient, ok := c.reads.(client.SessionCommittedTranscriptSuffixClient)
	if !ok {
		return fallbackToPage()
	}
	if c.committedSuffixRPCUnsupported() {
		return fallbackToPage()
	}
	ctx, cancel := c.readContext(timeout)
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseSilently, func() (serverapi.SessionCommittedTranscriptSuffixResponse, error) {
		return suffixClient.GetSessionCommittedTranscriptSuffix(ctx, serverapi.SessionCommittedTranscriptSuffixRequest{
			SessionID:       c.sessionID,
			AfterEntryCount: req.AfterEntryCount,
			Limit:           req.Limit,
		})
	})
	c.notifyConnectionState(err)
	if err != nil {
		if errors.Is(err, serverapi.ErrMethodNotFound) {
			c.setCommittedSuffixRPCUnsupported()
			return fallbackToPage()
		}
		return clientui.CommittedTranscriptSuffix{SessionID: c.sessionID}, err
	}
	suffix := resp.Suffix
	if suffix.SessionID == "" {
		suffix.SessionID = c.sessionID
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Session.Transcript = clientui.TranscriptMetadata{
			Revision:            suffix.Revision,
			CommittedEntryCount: suffix.CommittedEntryCount,
		}
	})
	return suffix, nil
}

func (c *sessionRuntimeClient) committedSuffixRPCUnsupported() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.suffixRPCUnsupported
}

func (c *sessionRuntimeClient) setCommittedSuffixRPCUnsupported() {
	c.mu.Lock()
	c.suffixRPCUnsupported = true
	c.mu.Unlock()
}

func (c *sessionRuntimeClient) transcriptDiagnosticsEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.transcriptDiagnostics || transcriptdiag.EnabledForProcess(false)
}

func (c *sessionRuntimeClient) notifyConnectionState(err error) {
	if c == nil {
		return
	}
	c.mu.RLock()
	observer := c.connectionStateObserver
	c.mu.RUnlock()
	if observer == nil {
		return
	}
	observer(err)
}

func (c *sessionRuntimeClient) notifyLeaseRecoveryWarning(text string, visibility clientui.EntryVisibility) {
	if c == nil || strings.TrimSpace(text) == "" {
		return
	}
	c.mu.RLock()
	observer := c.leaseRecoveryWarning
	c.mu.RUnlock()
	if observer == nil {
		return
	}
	observer(text, visibility)
}

func (c *sessionRuntimeClient) logTranscriptDiag(line string) {
	if c == nil {
		return
	}
	c.mu.RLock()
	logf := c.diagLogf
	c.mu.RUnlock()
	if logf == nil {
		return
	}
	logf(strings.TrimSpace(line))
}

func isOngoingTailTranscriptRequest(req clientui.TranscriptPageRequest) bool {
	return req == (clientui.TranscriptPageRequest{}) || req.Window == clientui.TranscriptWindowOngoingTail
}

func transcriptPageFromSessionView(view clientui.RuntimeSessionView) clientui.TranscriptPage {
	total := view.Transcript.CommittedEntryCount
	if total == 0 {
		total = len(view.Chat.Entries)
	}
	hasMore := total > len(view.Chat.Entries)
	nextOffset := 0
	if hasMore {
		nextOffset = len(view.Chat.Entries)
	}
	return clientui.TranscriptPage{
		SessionID:             view.SessionID,
		SessionName:           view.SessionName,
		ConversationFreshness: view.ConversationFreshness,
		Revision:              view.Transcript.Revision,
		TotalEntries:          total,
		Offset:                0,
		NextOffset:            nextOffset,
		HasMore:               hasMore,
		Entries:               cloneTranscriptEntries(view.Chat.Entries),
	}
}

func transcriptPageFromCommittedTranscriptSuffix(suffix clientui.CommittedTranscriptSuffix) clientui.TranscriptPage {
	nextOffset := 0
	if suffix.HasMore {
		nextOffset = suffix.NextEntryCount
	}
	return clientui.TranscriptPage{
		SessionID:             suffix.SessionID,
		SessionName:           suffix.SessionName,
		ConversationFreshness: suffix.ConversationFreshness,
		Revision:              suffix.Revision,
		TotalEntries:          suffix.CommittedEntryCount,
		Offset:                suffix.StartEntryCount,
		NextOffset:            nextOffset,
		HasMore:               suffix.HasMore,
		Entries:               cloneTranscriptEntries(suffix.Entries),
	}
}

func committedTranscriptSuffixFromPage(page clientui.TranscriptPage) clientui.CommittedTranscriptSuffix {
	return clientui.CommittedTranscriptSuffix{
		SessionID:             page.SessionID,
		SessionName:           page.SessionName,
		ConversationFreshness: page.ConversationFreshness,
		Revision:              page.Revision,
		CommittedEntryCount:   page.TotalEntries,
		StartEntryCount:       page.Offset,
		NextEntryCount:        page.Offset + len(page.Entries),
		HasMore:               page.HasMore,
		Entries:               cloneTranscriptEntries(page.Entries),
	}
}

func cloneTranscriptEntries(entries []clientui.ChatEntry) []clientui.ChatEntry {
	if len(entries) == 0 {
		return nil
	}
	cloned := make([]clientui.ChatEntry, 0, len(entries))
	for _, entry := range entries {
		copyEntry := entry
		if entry.ToolCall != nil {
			copyMeta := *entry.ToolCall
			if len(entry.ToolCall.Suggestions) > 0 {
				copyMeta.Suggestions = append([]string(nil), entry.ToolCall.Suggestions...)
			}
			if entry.ToolCall.RenderHint != nil {
				renderHint := *entry.ToolCall.RenderHint
				copyMeta.RenderHint = &renderHint
			}
			copyEntry.ToolCall = &copyMeta
		}
		cloned = append(cloned, copyEntry)
	}
	return cloned
}

func (c *sessionRuntimeClient) SetSessionName(name string) error {
	ctx, cancel := c.controlContext()
	defer cancel()
	if err := c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.SetSessionName(ctx, serverapi.RuntimeSetSessionNameRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Name: name})
	}); err != nil {
		return err
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Session.SessionName = name
	})
	return nil
}
func (c *sessionRuntimeClient) SetThinkingLevel(level string) error {
	ctx, cancel := c.controlContext()
	defer cancel()
	if err := c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.SetThinkingLevel(ctx, serverapi.RuntimeSetThinkingLevelRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Level: level})
	}); err != nil {
		return err
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.ThinkingLevel = level
	})
	return nil
}
func (c *sessionRuntimeClient) SetFastModeEnabled(enabled bool) (bool, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
		return c.controls.SetFastModeEnabled(ctx, serverapi.RuntimeSetFastModeEnabledRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Enabled: enabled})
	})
	if err == nil {
		c.patchMainView(func(view *clientui.RuntimeMainView) {
			view.Status.FastModeEnabled = enabled
		})
	}
	return resp.Changed, err
}
func (c *sessionRuntimeClient) SetReviewerEnabled(enabled bool) (bool, string, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
		return c.controls.SetReviewerEnabled(ctx, serverapi.RuntimeSetReviewerEnabledRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Enabled: enabled})
	})
	if err == nil {
		c.patchMainView(func(view *clientui.RuntimeMainView) {
			view.Status.ReviewerFrequency = resp.Mode
			view.Status.ReviewerEnabled = resp.Mode != "" && resp.Mode != "off"
		})
	}
	return resp.Changed, resp.Mode, err
}
func (c *sessionRuntimeClient) SetAutoCompactionEnabled(enabled bool) (bool, bool, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
		return c.controls.SetAutoCompactionEnabled(ctx, serverapi.RuntimeSetAutoCompactionEnabledRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Enabled: enabled})
	})
	if err != nil {
		return false, false, err
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.AutoCompactionEnabled = resp.Enabled
	})
	return resp.Changed, resp.Enabled, nil
}

func (c *sessionRuntimeClient) ShowGoal() (*clientui.RuntimeGoal, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseSilently, func() (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.ShowGoal(ctx, serverapi.RuntimeGoalShowRequest{SessionID: c.sessionID})
	})
	if err != nil {
		return nil, err
	}
	goal := runtimeGoalFromAPI(resp.Goal)
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.Goal = cloneRuntimeGoal(goal)
	})
	return goal, nil
}

func (c *sessionRuntimeClient) SetGoal(objective string) (*clientui.RuntimeGoal, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.SetGoal(ctx, serverapi.RuntimeGoalSetRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Objective: objective, Actor: "user"})
	})
	if err != nil {
		return nil, err
	}
	goal := runtimeGoalFromAPI(resp.Goal)
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.Goal = cloneRuntimeGoal(goal)
	})
	return goal, nil
}

func (c *sessionRuntimeClient) PauseGoal() (*clientui.RuntimeGoal, error) {
	return c.setGoalStatus(func(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.PauseGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) ResumeGoal() (*clientui.RuntimeGoal, error) {
	return c.setGoalStatus(func(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.ResumeGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) ClearGoal() (*clientui.RuntimeGoal, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.ClearGoal(ctx, serverapi.RuntimeGoalClearRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Actor: "user"})
	})
	if err != nil {
		return nil, err
	}
	goal := runtimeGoalFromAPI(resp.Goal)
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.Goal = cloneRuntimeGoal(goal)
	})
	return goal, nil
}

func (c *sessionRuntimeClient) setGoalStatus(call func(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error)) (*clientui.RuntimeGoal, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeGoalShowResponse, error) {
		return call(ctx, serverapi.RuntimeGoalStatusRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Actor: "user"})
	})
	if err != nil {
		return nil, err
	}
	goal := runtimeGoalFromAPI(resp.Goal)
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.Goal = cloneRuntimeGoal(goal)
	})
	return goal, nil
}

func runtimeGoalFromAPI(goal *serverapi.RuntimeGoal) *clientui.RuntimeGoal {
	if goal == nil {
		return nil
	}
	return &clientui.RuntimeGoal{
		ID:        goal.ID,
		Objective: goal.Objective,
		Status:    clientui.RuntimeGoalStatus(strings.TrimSpace(goal.Status)),
		Suspended: goal.Suspended,
	}
}

func cloneRuntimeGoal(goal *clientui.RuntimeGoal) *clientui.RuntimeGoal {
	if goal == nil {
		return nil
	}
	cloned := *goal
	return &cloned
}

func (c *sessionRuntimeClient) AppendLocalEntry(role, text string) error {
	ctx, cancel := c.controlContext()
	defer cancel()
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.AppendLocalEntry(ctx, serverapi.RuntimeAppendLocalEntryRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Role: role, Text: text})
	})
}
func (c *sessionRuntimeClient) ShouldCompactBeforeUserMessage(ctx context.Context, text string) (bool, error) {
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseSilently, func() (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error) {
		return c.controls.ShouldCompactBeforeUserMessage(ctx, serverapi.RuntimeShouldCompactBeforeUserMessageRequest{SessionID: c.sessionID, Text: text})
	})
	return resp.ShouldCompact, err
}
func (c *sessionRuntimeClient) SubmitUserMessage(ctx context.Context, text string) (string, error) {
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSubmitUserMessageResponse, error) {
		return c.controls.SubmitUserMessage(ctx, serverapi.RuntimeSubmitUserMessageRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Text: text})
	})
	return resp.Message, err
}
func (c *sessionRuntimeClient) SubmitUserShellCommand(ctx context.Context, command string) error {
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.SubmitUserShellCommand(ctx, serverapi.RuntimeSubmitUserShellCommandRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Command: command})
	})
}
func (c *sessionRuntimeClient) CompactContext(ctx context.Context, args string) error {
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.CompactContext(ctx, serverapi.RuntimeCompactContextRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Args: args})
	})
}
func (c *sessionRuntimeClient) CompactContextForPreSubmit(ctx context.Context) error {
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.CompactContextForPreSubmit(ctx, serverapi.RuntimeCompactContextForPreSubmitRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID})
	})
}
func (c *sessionRuntimeClient) HasQueuedUserWork() (bool, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseSilently, func() (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
		return c.controls.HasQueuedUserWork(ctx, serverapi.RuntimeHasQueuedUserWorkRequest{SessionID: c.sessionID})
	})
	if err != nil {
		return false, err
	}
	return resp.HasQueuedUserWork, nil
}
func (c *sessionRuntimeClient) SubmitQueuedUserMessages(ctx context.Context) (string, error) {
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
		return c.controls.SubmitQueuedUserMessages(ctx, serverapi.RuntimeSubmitQueuedUserMessagesRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID})
	})
	return resp.Message, err
}
func (c *sessionRuntimeClient) Interrupt() error {
	ctx, cancel := c.controlContext()
	defer cancel()
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.Interrupt(ctx, serverapi.RuntimeInterruptRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID})
	})
}

func (c *sessionRuntimeClient) QueueUserMessage(text string) {
	ctx, cancel := c.controlContext()
	defer cancel()
	if err := c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.QueueUserMessage(ctx, serverapi.RuntimeQueueUserMessageRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Text: text})
	}); err != nil {
		c.notifyConnectionState(err)
	}
}

func (c *sessionRuntimeClient) DiscardQueuedUserMessagesMatching(text string) int {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeDiscardQueuedUserMessagesMatchingResponse, error) {
		return c.controls.DiscardQueuedUserMessagesMatching(ctx, serverapi.RuntimeDiscardQueuedUserMessagesMatchingRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Text: text})
	})
	if err != nil {
		return 0
	}
	return resp.Discarded
}

func (c *sessionRuntimeClient) RecordPromptHistory(text string) error {
	ctx, cancel := c.controlContext()
	defer cancel()
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.RecordPromptHistory(ctx, serverapi.RuntimeRecordPromptHistoryRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Text: text})
	})
}
