package app

import (
	"bytes"
	"context"
	"core/cli/tui"
	"core/server/llm"
	"core/server/tools"
	"core/shared/clientui"
	"core/shared/protocol"
	"core/shared/rpcwire"
	"core/shared/serverapi"
	"core/shared/toolspec"
	"core/shared/transcript"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

type runtimeDisconnectTestRemote struct {
	server *httptest.Server
	mu     sync.Mutex
	conn   rpcwire.Conn
}

func (r *runtimeDisconnectTestRemote) URL() string {
	if r == nil || r.server == nil {
		return ""
	}
	return r.server.URL
}

func (r *runtimeDisconnectTestRemote) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	conn := r.conn
	r.conn = nil
	r.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	if r.server != nil {
		r.server.Close()
	}
}

func newRuntimeDisconnectTestRemote(t *testing.T) *runtimeDisconnectTestRemote {
	t.Helper()
	remote := &runtimeDisconnectTestRemote{}
	remote.server = httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(func(ctx context.Context, conn rpcwire.Conn) {
		remote.mu.Lock()
		remote.conn = conn
		remote.mu.Unlock()
		handshaken := false
		attached := false
		for event := range conn.Events() {
			if event.Err != nil {
				return
			}
			req := event.Frame.Request()
			if !handshaken {
				if req.Method != protocol.MethodHandshake {
					return
				}
				if err := conn.Send(ctx, rpcwire.FrameFromResponse(protocol.NewSuccessResponse(req.ID, protocol.HandshakeResponse{Identity: protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"}}))); err != nil {
					return
				}
				handshaken = true
				continue
			}
			if !attached {
				if req.Method != protocol.MethodAttachProject {
					return
				}
				if err := conn.Send(ctx, rpcwire.FrameFromResponse(protocol.NewSuccessResponse(req.ID, protocol.AttachResponse{Kind: "project", ProjectID: "project-1", WorkspaceRoot: "/tmp/workspace-a"}))); err != nil {
					return
				}
				attached = true
				continue
			}
			switch req.Method {
			case protocol.MethodSessionGetMainView:
				if err := conn.Send(ctx, rpcwire.FrameFromResponse(protocol.NewSuccessResponse(req.ID, serverapi.SessionMainViewResponse{MainView: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}}}))); err != nil {
					return
				}
			case protocol.MethodSessionGetTranscriptPage:
				if err := conn.Send(ctx, rpcwire.FrameFromResponse(protocol.NewSuccessResponse(req.ID, serverapi.SessionTranscriptPageResponse{Transcript: clientui.TranscriptPage{SessionID: "session-1"}}))); err != nil {
					return
				}
			default:
				return
			}
		}
	}))
	return remote
}

func closedAskEvents() <-chan askEvent {
	ch := make(chan askEvent)
	close(ch)
	return ch
}

func normalizedOutput(v string) string {
	return strings.Join(strings.Fields(xansi.Strip(v)), " ")
}

type lockedBuffer struct {
	mu         sync.Mutex
	buffer     bytes.Buffer
	firstWrite chan struct{}
	once       sync.Once
}

func newLockedBuffer() *lockedBuffer {
	return &lockedBuffer{firstWrite: make(chan struct{})}
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.once.Do(func() { close(b.firstWrite) })
	return b.buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func (b *lockedBuffer) Started() <-chan struct{} {
	return b.firstWrite
}

type observedUISnapshot struct {
	Mode                 tui.Mode
	OngoingSnapshot      string
	OngoingStreamingText string
	SawAssistantDelta    bool
}

type observedUIWaiter struct {
	check func(observedUISnapshot) bool
	ready chan struct{}
}

type observedUIModel struct {
	model   *uiModel
	mu      sync.Mutex
	latest  observedUISnapshot
	waiters []observedUIWaiter
}

func newObservedUIModel(model *uiModel) *observedUIModel {
	observed := &observedUIModel{model: model}
	observed.captureLocked()
	return observed
}

func (m *observedUIModel) Init() tea.Cmd {
	return m.model.Init()
}

func (m *observedUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(msg)
	if updated, ok := next.(*uiModel); ok {
		m.model = updated
	}
	m.mu.Lock()
	m.captureLocked()
	m.notifyWaitersLocked()
	m.mu.Unlock()
	return m, cmd
}

func (m *observedUIModel) View() string {
	return m.model.View()
}

func (m *observedUIModel) waitFor(t *testing.T, timeout time.Duration, description string, check func(observedUISnapshot) bool) {
	t.Helper()
	waitForSignal(t, timeout, description, m.readyWhen(check))
}

func (m *observedUIModel) readyWhen(check func(observedUISnapshot) bool) <-chan struct{} {
	ready := make(chan struct{})
	m.mu.Lock()
	defer m.mu.Unlock()
	if check(m.latest) {
		close(ready)
		return ready
	}
	m.waiters = append(m.waiters, observedUIWaiter{check: check, ready: ready})
	return ready
}

func (m *observedUIModel) captureLocked() {
	m.latest = observedUISnapshot{
		Mode:                 m.model.view.Mode(),
		OngoingSnapshot:      stripANSIAndTrimRight(m.model.view.OngoingSnapshot()),
		OngoingStreamingText: m.model.view.OngoingStreamingText(),
		SawAssistantDelta:    m.model.sawAssistantDelta,
	}
}

func (m *observedUIModel) notifyWaitersLocked() {
	remaining := m.waiters[:0]
	for _, waiter := range m.waiters {
		if waiter.check(m.latest) {
			close(waiter.ready)
			continue
		}
		remaining = append(remaining, waiter)
	}
	m.waiters = remaining
}

func waitForTestCondition(t *testing.T, timeout time.Duration, description string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForSignal(t *testing.T, timeout time.Duration, description string, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForSubmitResult(t *testing.T, timeout time.Duration, submitDone <-chan error) {
	t.Helper()
	select {
	case err := <-submitDone:
		if err != nil {
			t.Fatalf("submit user message: %v", err)
		}
	case <-time.After(timeout):
		t.Fatal("timed out waiting for submit user message completion")
	}
}

type nativeProgramHarness struct {
	t       *testing.T
	program *tea.Program
	done    chan error
}

func startNativeProgram(t *testing.T, model tea.Model, output io.Writer, options ...tea.ProgramOption) *nativeProgramHarness {
	t.Helper()
	programOptions := append([]tea.ProgramOption{
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(output),
		tea.WithoutSignals(),
	}, options...)
	program := tea.NewProgram(model, programOptions...)
	harness := &nativeProgramHarness{
		t:       t,
		program: program,
		done:    make(chan error, 1),
	}
	go func() {
		_, err := program.Run()
		harness.done <- err
	}()
	return harness
}

func (h *nativeProgramHarness) Send(msg tea.Msg) {
	h.program.Send(msg)
}

func (h *nativeProgramHarness) Quit() {
	h.program.Quit()
}

func (h *nativeProgramHarness) Wait(timeout time.Duration) {
	h.t.Helper()
	h.wait(timeout, false)
}

func (h *nativeProgramHarness) WaitAllowContextCanceled(timeout time.Duration) {
	h.t.Helper()
	h.wait(timeout, true)
}

func (h *nativeProgramHarness) QuitAndWait(timeout time.Duration) {
	h.t.Helper()
	h.Quit()
	h.Wait(timeout)
}

func (h *nativeProgramHarness) QuitAndWaitAllowContextCanceled(timeout time.Duration) {
	h.t.Helper()
	h.Quit()
	h.WaitAllowContextCanceled(timeout)
}

func (h *nativeProgramHarness) wait(timeout time.Duration, allowContextCanceled bool) {
	h.t.Helper()
	select {
	case err := <-h.done:
		if err != nil && !(allowContextCanceled && errors.Is(err, context.Canceled)) {
			h.t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(timeout):
		h.t.Fatal("program did not terminate")
	}
}

type singleChunkStreamClient struct {
	delta string
}

type noopFinalStreamClient struct{}

type asyncLateDeltaStreamClient struct {
	initial string
	late    string
	delay   time.Duration
}

type deferredFinalQueuedInjectionStreamClient struct {
	mu           sync.Mutex
	calls        int
	releaseFirst <-chan struct{}
	firstDelta   chan<- struct{}
}

type queuedSteerDuringBlockingToolClient struct {
	mu    sync.Mutex
	calls int
}

type blockingShellTool struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type reviewerNoSuggestionsClient struct{}

type staleTranscriptRuntimeClient struct {
	runtimeControlFakeClient
	loadCalls atomic.Int32
	page      clientui.TranscriptPage
}

type gatedRefreshRuntimeClient struct {
	runtimeControlFakeClient
	page           clientui.TranscriptPage
	refreshStarted chan struct{}
	releaseRefresh chan struct{}
	refreshOnce    sync.Once
}

func (c *staleTranscriptRuntimeClient) MainView() clientui.RuntimeMainView {
	if c.sessionView.SessionID == "" {
		c.sessionView.SessionID = "session-1"
	}
	return clientui.RuntimeMainView{Session: c.sessionView}
}

func (c *staleTranscriptRuntimeClient) RefreshMainView() (clientui.RuntimeMainView, error) {
	return c.MainView(), nil
}

func (c *staleTranscriptRuntimeClient) LoadTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	_ = req
	c.loadCalls.Add(1)
	page := c.page
	if page.SessionID == "" {
		page.SessionID = "session-1"
	}
	return page, nil
}

func (c *staleTranscriptRuntimeClient) RefreshTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return c.LoadTranscriptPage(req)
}

func (c *staleTranscriptRuntimeClient) LoadCalls() int {
	if c == nil {
		return 0
	}
	return int(c.loadCalls.Load())
}

func (c *gatedRefreshRuntimeClient) LoadTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	_ = req
	page := c.page
	if page.SessionID == "" {
		page.SessionID = "session-1"
	}
	return page, nil
}

func (c *gatedRefreshRuntimeClient) RefreshTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	c.refreshOnce.Do(func() {
		close(c.refreshStarted)
	})
	<-c.releaseRefresh
	return c.LoadTranscriptPage(req)
}

func (c singleChunkStreamClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("not implemented")
}

func (c singleChunkStreamClient) GenerateStream(_ context.Context, _ llm.Request, onDelta func(string)) (llm.Response, error) {
	if onDelta != nil {
		onDelta(c.delta)
	}
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: c.delta},
		Usage:     llm.Usage{WindowTokens: 200_000},
	}, nil
}

func (noopFinalStreamClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("not implemented")
}

func (noopFinalStreamClient) GenerateStream(_ context.Context, _ llm.Request, onDelta func(string)) (llm.Response, error) {
	if onDelta != nil {
		onDelta("NO_OP")
	}
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "NO_OP", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200_000},
	}, nil
}

func (c asyncLateDeltaStreamClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("not implemented")
}

func (c asyncLateDeltaStreamClient) GenerateStream(_ context.Context, _ llm.Request, onDelta func(string)) (llm.Response, error) {
	if onDelta != nil {
		onDelta(c.initial)
	}
	if onDelta != nil && strings.TrimSpace(c.late) != "" {
		go func() {
			time.Sleep(c.delay)
			onDelta(c.late)
		}()
	}
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: c.initial},
		Usage:     llm.Usage{WindowTokens: 200_000},
	}, nil
}

func (c *deferredFinalQueuedInjectionStreamClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("not implemented")
}

func (c *queuedSteerDuringBlockingToolClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("not implemented")
}

func (c *deferredFinalQueuedInjectionStreamClient) GenerateStream(_ context.Context, _ llm.Request, onDelta func(string)) (llm.Response, error) {
	c.mu.Lock()
	call := c.calls
	c.calls++
	releaseFirst := c.releaseFirst
	c.mu.Unlock()
	if call == 0 {
		if onDelta != nil {
			onDelta("foreground done")
		}
		if c.firstDelta != nil {
			close(c.firstDelta)
		}
		if releaseFirst != nil {
			<-releaseFirst
		}
		return llm.Response{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "foreground done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200_000},
		}, nil
	}
	if onDelta != nil {
		onDelta("NO_OP")
	}
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "NO_OP", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200_000},
	}, nil
}

func (c *queuedSteerDuringBlockingToolClient) GenerateStream(_ context.Context, _ llm.Request, onDelta func(string)) (llm.Response, error) {
	c.mu.Lock()
	call := c.calls
	c.calls++
	c.mu.Unlock()
	if call == 0 {
		if onDelta != nil {
			onDelta("working")
		}
		return llm.Response{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "working", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{
				ID:    "call-1",
				Name:  string(toolspec.ToolExecCommand),
				Input: json.RawMessage(`{"command":"sleep 1"}`),
				Presentation: transcript.EncodeToolCallMeta(transcript.ToolCallMeta{
					ToolName:    "shell",
					IsShell:     true,
					Command:     "sleep 1",
					CompactText: "sleep 1",
				}),
			}},
			Usage: llm.Usage{WindowTokens: 200_000},
		}, nil
	}
	if onDelta != nil {
		onDelta("after steer")
	}
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "after steer", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200_000},
	}, nil
}

func (t *blockingShellTool) Call(ctx context.Context, c tools.Call) (tools.Result, error) {
	t.once.Do(func() {
		close(t.started)
	})
	select {
	case <-t.release:
	case <-ctx.Done():
		return tools.Result{CallID: c.ID, Name: toolspec.ToolExecCommand, IsError: true, Output: []byte(`{"error":"context canceled"}`)}, ctx.Err()
	}
	return tools.Result{CallID: c.ID, Name: toolspec.ToolExecCommand, Output: []byte(`"/tmp"`)}, nil
}

func (reviewerNoSuggestionsClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"suggestions":[]}`},
		Usage:     llm.Usage{WindowTokens: 200_000},
	}, nil
}
