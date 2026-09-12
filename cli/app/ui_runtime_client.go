package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"core/shared/apicontract"
	"core/shared/clientui"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
)

const uiRuntimeControlTimeout = 3 * time.Second
const uiChatSettingsTimeout = 30 * time.Second
const uiRuntimeHydrationReadTimeout = 10 * time.Second
const runtimeReconnectWarningText = "Lost connection to the session runtime; reconnected."

var errRuntimeTranscriptRefreshUnsupported = errors.New("runtime transcript refresh is not available in the tui-redesign emergency path")

var uiRuntimeReadTimeout = 300 * time.Millisecond

type sessionRuntimeClient struct {
	reads                    apicontract.SessionViewService
	controls                 apicontract.RuntimeControlService
	chatSettings             apicontract.ChatSettingsService
	sessionID                string
	reactivator              *runtimeReactivator
	connectionStateObserver  func(error)
	reconnectWarningObserver func(string, clientui.EntryVisibility)

	mu               sync.RWMutex
	mainView         *runtimepb.MainView
	hasMainView      bool
	metadataRevision uint64
}

func newUIRuntimeClientWithReads(sessionID string, reads apicontract.SessionViewService, controls apicontract.RuntimeControlService, chatSettings apicontract.ChatSettingsService) clientui.RuntimeClient {
	if reads == nil || controls == nil {
		return nil
	}
	return &sessionRuntimeClient{
		sessionID:    sessionID,
		reactivator:  newRuntimeReactivator(),
		reads:        reads,
		controls:     controls,
		chatSettings: chatSettings,
		mainView:     &runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: sessionID}, Status: &runtimepb.Status{}},
	}
}

func (c *sessionRuntimeClient) SetRuntimeReactivator(reactivator *runtimeReactivator) {
	if c == nil || reactivator == nil {
		return
	}
	c.mu.Lock()
	c.reactivator = reactivator
	c.mu.Unlock()
}

func (c *sessionRuntimeClient) runtimeReactivator() *runtimeReactivator {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reactivator
}

func (c *sessionRuntimeClient) recoverRuntimeConnectionWithWarning(ctx context.Context, trigger error, appendWarning bool) error {
	return c.recoverRuntimeConnection(ctx, trigger, appendWarning, true)
}

func (c *sessionRuntimeClient) recoverRuntimeConnectionPreservingContext(ctx context.Context, trigger error, appendWarning bool) error {
	return c.recoverRuntimeConnection(ctx, trigger, appendWarning, false)
}

func (c *sessionRuntimeClient) recoverRuntimeConnection(ctx context.Context, trigger error, appendWarning bool, detach bool) error {
	reactivator := c.runtimeReactivator()
	if reactivator == nil {
		return errRuntimeReactivationUnavailable
	}
	reconnectBase := ctx
	if detach {
		reconnectBase = context.WithoutCancel(ctx)
	}
	reconnectCtx, cancel := context.WithTimeout(reconnectBase, uiRuntimeControlTimeout)
	defer cancel()
	if err := reactivator.Reactivate(reconnectCtx); err != nil {
		return err
	}
	if appendWarning && isRecoverableRuntimeControlError(trigger) {
		c.appendRuntimeReconnectWarning()
	}
	return nil
}

func (c *sessionRuntimeClient) appendRuntimeReconnectWarning() {
	if c == nil || c.controls == nil {
		return
	}
	warningCtx, cancel := context.WithTimeout(context.Background(), uiRuntimeControlTimeout)
	defer cancel()
	if err := c.controls.AppendCommittedEntry(warningCtx, &transcriptpb.AppendCommittedEntryRequest{
		SessionId:  c.sessionID,
		Role:       "warning",
		Text:       runtimeReconnectWarningText,
		Visibility: transcriptpb.AppendVisibility_APPEND_VISIBILITY_ONGOING.Enum(),
	}); err != nil {
		c.notifyRuntimeReconnectWarning(runtimeReconnectWarningText, clientui.EntryVisibilityOngoing)
	}
}

func isRecoverableRuntimeControlError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, serverapi.ErrRuntimeUnavailable)
}

func runtimeControlCall[T any](c *sessionRuntimeClient, appendWarning bool, call func(ctx context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), uiRuntimeControlTimeout)
	defer cancel()
	return runtimeRequestCall(ctx, c, appendWarning, call)
}

func runtimeRequestCall[T any](ctx context.Context, c *sessionRuntimeClient, appendWarning bool, call func(ctx context.Context) (T, error)) (T, error) {
	return retryRuntimeUnavailableCall(ctx, c.recoverRuntimeConnectionWithWarning, appendWarning, func() (T, error) {
		return call(ctx)
	})
}

func runtimeControlCallNoResult(c *sessionRuntimeClient, call func(ctx context.Context) error) error {
	_, err := runtimeControlCall(c, true, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, call(ctx)
	})
	return err
}

func retryRuntimeUnavailableCall[T any](ctx context.Context, recoverRuntimeConnection func(context.Context, error, bool) error, appendRecoveryWarning bool, call func() (T, error)) (T, error) {
	value, err := call()
	if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return value, err
	}
	var zero T
	if recoverErr := recoverRuntimeConnection(ctx, err, appendRecoveryWarning); recoverErr != nil {
		return zero, recoverErr
	}
	return call()
}

func (c *sessionRuntimeClient) SetConnectionStateObserver(observer func(error)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectionStateObserver = observer
}

func (c *sessionRuntimeClient) SetRuntimeReconnectWarningObserver(observer func(string, clientui.EntryVisibility)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconnectWarningObserver = observer
}

func (c *sessionRuntimeClient) MainView() *runtimepb.MainView {
	view, _ := c.cachedMainView()
	if view.Session.SessionId == "" {
		view.Session.SessionId = c.sessionID
	}
	return view
}

func (c *sessionRuntimeClient) RefreshMainView() (*runtimepb.MainView, error) {
	return c.refreshMainViewSync(uiRuntimeHydrationReadTimeout)
}

func (c *sessionRuntimeClient) Status() *runtimepb.Status {
	return c.MainView().Status
}

func (c *sessionRuntimeClient) SessionView() *runtimepb.SessionView {
	return c.MainView().Session
}

func (c *sessionRuntimeClient) readContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = uiRuntimeReadTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (c *sessionRuntimeClient) refreshMainViewSync(timeout time.Duration) (*runtimepb.MainView, error) {
	view, err := c.fetchMainViewSync(timeout)
	if err != nil {
		c.mu.Lock()
		if c.mainView.Session.SessionId == "" {
			c.mainView.Session.SessionId = c.sessionID
		}
		c.hasMainView = true
		view = proto.Clone(c.mainView).(*runtimepb.MainView)
		c.mu.Unlock()
		return view, err
	}
	return c.storeMainView(view), nil
}

func (c *sessionRuntimeClient) fetchMainView() (*runtimepb.MainView, error) {
	return c.fetchMainViewSync(uiRuntimeHydrationReadTimeout)
}

func (c *sessionRuntimeClient) fetchMainViewSync(timeout time.Duration) (*runtimepb.MainView, error) {
	ctx, cancel := c.readContext(timeout)
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverRuntimeConnectionPreservingContext, false, func() (*sessionpb.MainViewSuccess, error) {
		return c.reads.GetSessionMainView(ctx, &sessionpb.MainViewRequest{SessionId: c.sessionID})
	})
	c.notifyConnectionState(err)
	if err != nil {
		view, _ := c.cachedMainView()
		if view.Session.SessionId == "" {
			view.Session.SessionId = c.sessionID
		}
		return view, err
	}
	view := proto.Clone(resp.MainView).(*runtimepb.MainView)
	if view.Session.SessionId == "" {
		view.Session.SessionId = c.sessionID
	}
	return view, nil
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

func (c *sessionRuntimeClient) notifyRuntimeReconnectWarning(text string, visibility clientui.EntryVisibility) {
	if c == nil || strings.TrimSpace(text) == "" {
		return
	}
	c.mu.RLock()
	observer := c.reconnectWarningObserver
	c.mu.RUnlock()
	if observer == nil {
		return
	}
	observer(text, visibility)
}
