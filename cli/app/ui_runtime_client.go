package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"core/shared/apicontract"
	"core/shared/clientui"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"

	"google.golang.org/protobuf/proto"
)

const uiRuntimeControlTimeout = 3 * time.Second
const uiChatSettingsTimeout = 30 * time.Second
const uiRuntimeHydrationReadTimeout = 10 * time.Second

var errRuntimeTranscriptRefreshUnsupported = errors.New("runtime transcript refresh is not available in the tui-redesign emergency path")

var uiRuntimeReadTimeout = 300 * time.Millisecond

type sessionRuntimeClient struct {
	reads                   apicontract.SessionViewService
	controls                apicontract.RuntimeControlService
	chatMutations           apicontract.ChatMutationService
	goalSet                 goalSetClient
	chatSettings            apicontract.ChatSettingsService
	sessionID               string
	connectionStateObserver func(error)

	mu               sync.RWMutex
	preparation      *sessionlaunchpb.SessionRuntimePreparationOptions
	mainView         *runtimepb.MainView
	hasMainView      bool
	metadataRevision uint64
}

func newUIRuntimeClientWithReads(
	sessionID string,
	reads apicontract.SessionViewService,
	controls apicontract.RuntimeControlService,
	goalSet goalSetClient,
	chatSettings apicontract.ChatSettingsService,
) clientui.RuntimeClient {
	if reads == nil || controls == nil {
		return nil
	}
	return &sessionRuntimeClient{
		sessionID:    sessionID,
		reads:        reads,
		controls:     controls,
		goalSet:      goalSet,
		chatSettings: chatSettings,
		mainView:     &runtimepb.MainView{Session: &runtimepb.SessionView{SessionId: sessionID}, Status: &runtimepb.Status{}},
	}
}

func runtimeControlCall[T any](call func(ctx context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), uiRuntimeControlTimeout)
	defer cancel()
	return call(ctx)
}

func runtimeControlCallNoResult(call func(ctx context.Context) error) error {
	_, err := runtimeControlCall(func(ctx context.Context) (struct{}, error) {
		return struct{}{}, call(ctx)
	})
	return err
}

func (c *sessionRuntimeClient) SetConnectionStateObserver(observer func(error)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectionStateObserver = observer
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
	resp, err := c.reads.GetSessionMainView(ctx, &sessionpb.MainViewRequest{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: c.sessionID}}})
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
