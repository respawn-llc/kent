package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"core/shared/client"
	"core/shared/clientui"
	"core/shared/serverapi"
	"core/shared/transcriptdiag"

	"github.com/google/uuid"
)

const uiRuntimeControlTimeout = 3 * time.Second
const uiRuntimeHydrationReadTimeout = 10 * time.Second
const collaborativeFallbackRefreshCooldown = 750 * time.Millisecond
const runtimeLeaseRecoveryWarningText = "Lost connection to the session runtime; reconnected."

var uiRuntimeReadTimeout = 300 * time.Millisecond
var errReadOnlyRuntime = errors.New("session is unavailable for runtime control attachment")
var errCollaborativeOperationBlocked = errors.New("operation is unavailable for collaborative runtime attach")

type sessionRuntimeClient struct {
	reads                   client.SessionViewClient
	controls                client.RuntimeControlClient
	sessionID               string
	controllerLease         *controllerLeaseManager
	diagLogf                func(string)
	transcriptDiagnostics   bool
	connectionStateObserver func(error)
	leaseRecoveryWarning    func(string, clientui.EntryVisibility)
	readOnly                bool
	accessMode              serverapi.SessionRuntimeAttachMode
	allowedOperations       map[serverapi.SessionRuntimeOperation]bool

	mu                   sync.RWMutex
	mainView             clientui.RuntimeMainView
	hasMainView          bool
	mainViewFallback     bool
	mainViewRetryAfter   time.Time
	suffixRPCUnsupported bool
}

func (c *sessionRuntimeClient) SetReadOnly(readOnly bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.readOnly = readOnly
	c.mu.Unlock()
}

func (c *sessionRuntimeClient) SetAccessMode(mode serverapi.SessionRuntimeAttachMode, operations []serverapi.SessionRuntimeOperation) {
	if c == nil {
		return
	}
	allowed := make(map[serverapi.SessionRuntimeOperation]bool, len(operations))
	for _, op := range operations {
		allowed[op] = true
	}
	if mode == serverapi.SessionRuntimeAttachModeCollaborative && len(allowed) == 0 {
		for _, op := range defaultCollaborativeRuntimeOperations() {
			allowed[op] = true
		}
	}
	c.mu.Lock()
	c.accessMode = mode
	c.readOnly = mode == serverapi.SessionRuntimeAttachModeNoControl
	c.allowedOperations = allowed
	if mode == serverapi.SessionRuntimeAttachModeCollaborative {
		c.controllerLease = nil
		if c.applyCollaborativeMainViewFallbackLocked(&c.mainView) {
			c.mainViewFallback = true
			c.mainViewRetryAfter = time.Time{}
		}
		c.hasMainView = true
	} else {
		c.mainViewFallback = false
		c.mainViewRetryAfter = time.Time{}
	}
	c.mu.Unlock()
}

func (c *sessionRuntimeClient) applyCollaborativeMainViewFallbackLocked(view *clientui.RuntimeMainView) bool {
	if c == nil || view == nil || c.accessMode != serverapi.SessionRuntimeAttachModeCollaborative {
		return false
	}
	if view.Session.SessionID == "" {
		view.Session.SessionID = c.sessionID
	}
	if view.ExternalRuntime == nil || view.ExternalRuntime.State == "" {
		status := clientui.ExternalRuntimeStatus{State: clientui.ExternalRuntimeStateOwnerRunning, QueueAccepting: true}
		view.ExternalRuntime = &status
		return true
	}
	return false
}

func defaultCollaborativeRuntimeOperations() []serverapi.SessionRuntimeOperation {
	return []serverapi.SessionRuntimeOperation{
		serverapi.SessionRuntimeOperationSubmitUserTurn,
		serverapi.SessionRuntimeOperationQueueUserMessage,
		serverapi.SessionRuntimeOperationPromptAnswer,
	}
}

func (c *sessionRuntimeClient) ensureOperation(op serverapi.SessionRuntimeOperation) error {
	if err := c.ensureWritable(); err != nil {
		return err
	}
	c.mu.RLock()
	mode := c.accessMode
	allowed := c.allowedOperations[op]
	c.mu.RUnlock()
	if mode == serverapi.SessionRuntimeAttachModeCollaborative && !allowed {
		return errCollaborativeOperationBlocked
	}
	return nil
}

func (c *sessionRuntimeClient) ensureWritable() error {
	if c == nil {
		return errReadOnlyRuntime
	}
	if c.isReadOnly() {
		return errReadOnlyRuntime
	}
	return nil
}

func (c *sessionRuntimeClient) isReadOnly() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	readOnly := c.readOnly
	c.mu.RUnlock()
	return readOnly
}

func (c *sessionRuntimeClient) isCollaborative() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accessMode == serverapi.SessionRuntimeAttachModeCollaborative
}

func (c *sessionRuntimeClient) IsCollaborativeRuntime() bool {
	return c.isCollaborative()
}

func (c *sessionRuntimeClient) CanAppendCommittedEntries() bool {
	return c != nil && !c.isReadOnly() && !c.isCollaborative()
}

func newUIRuntimeClientWithReads(sessionID string, reads client.SessionViewClient, controls client.RuntimeControlClient) clientui.RuntimeClient {
	if reads == nil || controls == nil {
		return nil
	}
	return &sessionRuntimeClient{
		sessionID:       sessionID,
		accessMode:      serverapi.SessionRuntimeAttachModeController,
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
	warningCtx, cancel := context.WithTimeout(context.Background(), uiRuntimeControlTimeout)
	defer cancel()
	if err := c.controls.AppendCommittedEntry(warningCtx, serverapi.RuntimeAppendCommittedEntryRequest{
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
	_, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLeaseWithWarning, true, func(controllerLeaseID string) (struct{}, error) {
		return struct{}{}, call(controllerLeaseID)
	})
	return err
}

func retryRuntimeControlCall[T any](ctx context.Context, currentLeaseID func() string, recoverLease func(context.Context, error, bool) error, appendRecoveryWarning bool, call func(controllerLeaseID string) (T, error)) (T, error) {
	leaseID := strings.TrimSpace(currentLeaseID())
	value, err := call(leaseID)
	if !isRecoverableRuntimeControlError(err) {
		return value, err
	}
	if leaseID == "" {
		return value, err
	}
	var zero T
	if recoverErr := recoverLease(ctx, err, appendRecoveryWarning); recoverErr != nil {
		return zero, recoverErr
	}
	return call(currentLeaseID())
}

func retryRuntimeUnavailableCall[T any](ctx context.Context, recoverLease func(context.Context, error, bool) error, appendRecoveryWarning bool, call func() (T, error)) (T, error) {
	value, err := call()
	if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return value, err
	}
	var zero T
	if recoverErr := recoverLease(ctx, err, appendRecoveryWarning); recoverErr != nil {
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
	view, hasView, fallback := c.cachedMainViewState()
	if !hasView || (fallback && c.claimMainViewFallbackRefresh()) {
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
	return c.refreshTranscriptPageSync(clientui.TranscriptPageRequest{}, uiRuntimeHydrationReadTimeout)
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

func (c *sessionRuntimeClient) readContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = uiRuntimeReadTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (c *sessionRuntimeClient) cachedMainView() (clientui.RuntimeMainView, bool) {
	view, hasView, _ := c.cachedMainViewState()
	return view, hasView
}

func (c *sessionRuntimeClient) cachedMainViewState() (clientui.RuntimeMainView, bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	view := c.mainView
	if !c.hasMainView {
		return view, false, false
	}
	return view, true, c.mainViewFallback
}

func (c *sessionRuntimeClient) claimMainViewFallbackRefresh() bool {
	if c == nil {
		return false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.mainViewFallback {
		return false
	}
	if !c.mainViewRetryAfter.IsZero() && now.Before(c.mainViewRetryAfter) {
		return false
	}
	c.mainViewRetryAfter = now.Add(collaborativeFallbackRefreshCooldown)
	return true
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
	c.mainViewFallback = false
	c.mainViewRetryAfter = time.Time{}
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
	if c == nil || (evt.ContextUsage == nil && evt.GoalStatus == nil) {
		return
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		if evt.ContextUsage != nil {
			view.Status.ContextUsage = *evt.ContextUsage
		}
		if evt.Kind == clientui.EventGoalStatusUpdated && evt.GoalStatus != nil {
			view.Status.Goal = runtimeGoalFromStatusUpdate(view.Status.Goal, *evt.GoalStatus)
		}
	})
}

func runtimeGoalFromStatusUpdate(existing *clientui.RuntimeGoal, update clientui.RuntimeGoalStatusUpdate) *clientui.RuntimeGoal {
	if update.Cleared {
		return nil
	}
	goal := &clientui.RuntimeGoal{
		ID:        strings.TrimSpace(update.ID),
		Objective: update.Objective,
		Status:    update.Status,
	}
	if existing != nil &&
		strings.TrimSpace(existing.ID) == goal.ID &&
		existing.Status == clientui.RuntimeGoalStatusActive &&
		goal.Status == clientui.RuntimeGoalStatusActive {
		goal.Suspended = existing.Suspended
	}
	return goal
}

func (c *sessionRuntimeClient) refreshMainViewSync(timeout time.Duration) (clientui.RuntimeMainView, error) {
	ctx, cancel := c.readContext(timeout)
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseWithWarning, false, func() (serverapi.SessionMainViewResponse, error) {
		return c.reads.GetSessionMainView(ctx, serverapi.SessionMainViewRequest{SessionID: c.sessionID})
	})
	c.notifyConnectionState(err)
	if err != nil {
		c.mu.Lock()
		view := c.mainView
		if view.Session.SessionID == "" {
			view.Session.SessionID = c.sessionID
		}
		if c.applyCollaborativeMainViewFallbackLocked(&view) {
			c.mainViewFallback = true
			c.mainViewRetryAfter = time.Now().Add(collaborativeFallbackRefreshCooldown)
		}
		c.mainView = view
		c.hasMainView = true
		c.mu.Unlock()
		return view, err
	}
	return c.storeMainView(resp.MainView), nil
}

func (c *sessionRuntimeClient) refreshTranscriptPageSync(req clientui.TranscriptPageRequest, timeout time.Duration) (clientui.TranscriptPage, error) {
	ctx, cancel := c.readContext(timeout)
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseWithWarning, false, func() (serverapi.SessionTranscriptPageResponse, error) {
		return c.reads.GetSessionTranscriptPage(ctx, serverapi.SessionTranscriptPageRequest{
			SessionID:   c.sessionID,
			Cursor:      req.Cursor,
			NewerCursor: req.NewerCursor,
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
		view.Status.ConversationFreshness = page.ConversationFreshness
		view.Session.ConversationFreshness = page.ConversationFreshness
		committedEntryCount := view.Session.Transcript.CommittedEntryCount
		if isRecentTailTranscriptRequest(req) {
			committedEntryCount = page.TotalEntries
		}
		view.Session.Transcript = clientui.TranscriptMetadata{
			Revision:            page.Revision,
			CommittedEntryCount: committedEntryCount,
		}
		if isRecentTailTranscriptRequest(req) {
			view.Session.Chat = clientui.ChatSnapshot{
				Entries:        cloneTranscriptEntries(page.Entries),
				Streaming:      page.Streaming,
				StreamingError: page.StreamingError,
			}
		}
	})
	return page, nil
}

func (c *sessionRuntimeClient) refreshCommittedTranscriptSuffixSync(_ clientui.CommittedTranscriptSuffixRequest, timeout time.Duration) (clientui.CommittedTranscriptSuffix, error) {
	fallbackToPage := func() (clientui.CommittedTranscriptSuffix, error) {
		page, err := c.refreshTranscriptPageSync(clientui.TranscriptPageRequest{}, timeout)
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
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseWithWarning, false, func() (serverapi.SessionCommittedTranscriptSuffixResponse, error) {
		return suffixClient.GetSessionCommittedTranscriptSuffix(ctx, serverapi.SessionCommittedTranscriptSuffixRequest{
			SessionID: c.sessionID,
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
		view.Status.ConversationFreshness = suffix.ConversationFreshness
		view.Session.ConversationFreshness = suffix.ConversationFreshness
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
	return c.transcriptDiagnostics || transcriptdiag.Enabled(false, os.Getenv)
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

func isRecentTailTranscriptRequest(req clientui.TranscriptPageRequest) bool {
	return req.Cursor <= 0 && req.NewerCursor <= 0
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
