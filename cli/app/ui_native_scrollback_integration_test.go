package app

import (
	"builder/cli/tui"
	"builder/server/llm"
	"builder/server/tools"
	"builder/shared/clientui"
	"builder/shared/protocol"
	"builder/shared/rpcwire"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
	"builder/shared/transcript"
	"builder/shared/transcript/toolcodec"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func (m *observedUIModel) snapshot() observedUISnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latest
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

type singleChunkStreamClient struct {
	delta string
}

type noopFinalStreamClient struct{}

type asyncLateDeltaStreamClient struct {
	initial string
	late    string
	delay   time.Duration
}

type gatedStreamClient struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	lastReq llm.Request
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

type countingRuntimeClient struct {
	inner        clientui.RuntimeClient
	loadCalls    atomic.Int32
	refreshCalls atomic.Int32
}

type localCompactionSummaryClient struct {
	summary string
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

func (c *countingRuntimeClient) MainView() clientui.RuntimeMainView { return c.inner.MainView() }

func (c *countingRuntimeClient) RefreshMainView() (clientui.RuntimeMainView, error) {
	return c.inner.RefreshMainView()
}

func (c *countingRuntimeClient) Transcript() clientui.TranscriptPage { return c.inner.Transcript() }

func (c *countingRuntimeClient) RefreshTranscript() (clientui.TranscriptPage, error) {
	return c.inner.RefreshTranscript()
}

func (c *countingRuntimeClient) RefreshTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	c.refreshCalls.Add(1)
	return c.inner.RefreshTranscriptPage(req)
}

func (c *countingRuntimeClient) LoadTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	c.loadCalls.Add(1)
	return c.inner.LoadTranscriptPage(req)
}

func (c *countingRuntimeClient) Status() clientui.RuntimeStatus { return c.inner.Status() }

func (c *countingRuntimeClient) SessionView() clientui.RuntimeSessionView {
	return c.inner.SessionView()
}

func (c *countingRuntimeClient) SetSessionName(name string) error {
	return c.inner.SetSessionName(name)
}

func (c *countingRuntimeClient) SetThinkingLevel(level string) error {
	return c.inner.SetThinkingLevel(level)
}

func (c *countingRuntimeClient) SetFastModeEnabled(enabled bool) (bool, error) {
	return c.inner.SetFastModeEnabled(enabled)
}

func (c *countingRuntimeClient) SetReviewerEnabled(enabled bool) (bool, string, error) {
	return c.inner.SetReviewerEnabled(enabled)
}

func (c *countingRuntimeClient) SetAutoCompactionEnabled(enabled bool) (bool, bool, error) {
	return c.inner.SetAutoCompactionEnabled(enabled)
}

func (c *countingRuntimeClient) ShowGoal() (*clientui.RuntimeGoal, error) {
	return c.inner.ShowGoal()
}

func (c *countingRuntimeClient) SetGoal(objective string) (*clientui.RuntimeGoal, error) {
	return c.inner.SetGoal(objective)
}

func (c *countingRuntimeClient) PauseGoal() (*clientui.RuntimeGoal, error) {
	return c.inner.PauseGoal()
}

func (c *countingRuntimeClient) ResumeGoal() (*clientui.RuntimeGoal, error) {
	return c.inner.ResumeGoal()
}

func (c *countingRuntimeClient) ClearGoal() (*clientui.RuntimeGoal, error) {
	return c.inner.ClearGoal()
}

func (c *countingRuntimeClient) AppendLocalEntry(role, text string) error {
	return c.inner.AppendLocalEntry(role, text)
}

func (c *countingRuntimeClient) ShouldCompactBeforeUserMessage(ctx context.Context, text string) (bool, error) {
	return c.inner.ShouldCompactBeforeUserMessage(ctx, text)
}

func (c *countingRuntimeClient) SubmitUserMessage(ctx context.Context, text string) (string, error) {
	return c.inner.SubmitUserMessage(ctx, text)
}

func (c *countingRuntimeClient) SubmitUserShellCommand(ctx context.Context, command string) error {
	return c.inner.SubmitUserShellCommand(ctx, command)
}

func (c *countingRuntimeClient) CompactContext(ctx context.Context, args string) error {
	return c.inner.CompactContext(ctx, args)
}

func (c *countingRuntimeClient) CompactContextForPreSubmit(ctx context.Context) error {
	return c.inner.CompactContextForPreSubmit(ctx)
}

func (c *countingRuntimeClient) HasQueuedUserWork() (bool, error) { return c.inner.HasQueuedUserWork() }

func (c *countingRuntimeClient) SubmitQueuedUserMessages(ctx context.Context) (string, error) {
	return c.inner.SubmitQueuedUserMessages(ctx)
}

func (c *countingRuntimeClient) Interrupt() error { return c.inner.Interrupt() }

func (c *countingRuntimeClient) QueueUserMessage(text string) { c.inner.QueueUserMessage(text) }

func (c *countingRuntimeClient) DiscardQueuedUserMessagesMatching(text string) int {
	return c.inner.DiscardQueuedUserMessagesMatching(text)
}

func (c *countingRuntimeClient) RecordPromptHistory(text string) error {
	return c.inner.RecordPromptHistory(text)
}

func (c *countingRuntimeClient) LoadCalls() int { return int(c.loadCalls.Load()) }

func (c *countingRuntimeClient) RefreshCalls() int { return int(c.refreshCalls.Load()) }

func (c localCompactionSummaryClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: c.summary, Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200_000},
	}, nil
}

func (c localCompactionSummaryClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "test-local", SupportsResponsesAPI: false}, nil
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

func (c *gatedStreamClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("not implemented")
}

func (c *gatedStreamClient) GenerateStream(_ context.Context, req llm.Request, onDelta func(string)) (llm.Response, error) {
	c.mu.Lock()
	c.lastReq = req
	c.mu.Unlock()
	close(c.started)
	<-c.release
	if onDelta != nil {
		onDelta("assistant")
	}
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "assistant"},
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
				Presentation: toolcodec.EncodeToolCallMeta(transcript.ToolCallMeta{
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

func (t *blockingShellTool) Name() toolspec.ID {
	return toolspec.ToolExecCommand
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

func TestNativeScrollbackProgramOutputContract(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
		WithUIInitialTranscript([]UITranscriptEntry{
			{Role: "user", Text: "first replay line"},
			{Role: "assistant", Text: "second replay line"},
		}),
	)

	program := tea.NewProgram(
		model,
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(out),
		tea.WithoutSignals(),
	)

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	time.Sleep(40 * time.Millisecond)
	program.Send(nativeHistoryFlushMsg{Text: "delta replay line"})
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	time.Sleep(20 * time.Millisecond)
	program.Quit()

	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}

	raw := out.String()
	normalized := normalizedOutput(raw)
	if !strings.Contains(raw, "\x1b[2J") {
		t.Fatalf("expected startup clear-screen sequence in native mode output")
	}
	if strings.Contains(raw, "\x1b[?1049h") || strings.Contains(raw, "\x1b[?1049l") {
		t.Fatalf("did not expect alt-screen enter/leave sequences in native mode output")
	}
	if strings.Contains(raw, "\x1b[?1000h") || strings.Contains(raw, "\x1b[?1002h") || strings.Contains(raw, "\x1b[?1003h") || strings.Contains(raw, "\x1b[?1006h") {
		t.Fatalf("did not expect mouse-capture enable sequences in native mode output")
	}
	if strings.Count(normalized, "first replay line") != 1 {
		t.Fatalf("expected startup replay line exactly once, got %d", strings.Count(normalized, "first replay line"))
	}
	if strings.Count(normalized, "delta replay line") != 1 {
		t.Fatalf("expected delta replay exactly once, got %d", strings.Count(normalized, "delta replay line"))
	}
	if strings.Contains(raw, strings.Repeat(" ", 400)) {
		t.Fatalf("expected native mode to avoid frame-sized whitespace rewrites")
	}
	plain := xansi.Strip(raw)
	if occurrences := strings.Count(plain, statusStateCircleGlyph+statusLineSpinnerSeparator); occurrences > 12 {
		t.Fatalf("expected bounded status redraw output, got %d occurrences", occurrences)
	}
}

func TestNativeScrollbackInitClearsOnEachProgramRun(t *testing.T) {
	run := func() string {
		t.Helper()
		out := &bytes.Buffer{}
		model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())

		program := tea.NewProgram(
			model,
			tea.WithInput(strings.NewReader("")),
			tea.WithOutput(out),
			tea.WithoutSignals(),
		)

		done := make(chan error, 1)
		go func() {
			_, err := program.Run()
			done <- err
		}()

		time.Sleep(40 * time.Millisecond)
		program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
		time.Sleep(20 * time.Millisecond)
		program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("program run failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("program did not terminate")
		}

		return out.String()
	}

	first := run()
	second := run()
	if !strings.Contains(first, "\x1b[2J") {
		t.Fatalf("expected first startup to clear screen, output=%q", first)
	}
	if !strings.Contains(second, "\x1b[2J") {
		t.Fatalf("expected second startup to clear screen, output=%q", second)
	}
}

func TestNativeResizeReplaysOngoingScreenAfterRealResize(t *testing.T) {
	previousDebounce := nativeResizeReplayDebounce
	nativeResizeReplayDebounce = 20 * time.Millisecond
	t.Cleanup(func() {
		nativeResizeReplayDebounce = previousDebounce
	})

	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed replay line"}}),
	)
	model.input = "line one\nline two"

	program := tea.NewProgram(
		model,
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(out),
		tea.WithoutSignals(),
	)

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	time.Sleep(40 * time.Millisecond)
	for _, size := range []tea.WindowSizeMsg{
		{Width: 120, Height: 30},
		{Width: 96, Height: 30},
		{Width: 110, Height: 30},
		{Width: 84, Height: 30},
	} {
		program.Send(size)
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	program.Quit()

	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}

	raw := out.String()
	if count := strings.Count(raw, "\x1b[2J"); count < 2 || count > 3 {
		t.Fatalf("expected startup clear plus 1-2 width-resize replay clears, got %d occurrences in %q", count, raw)
	}
	plain := xansi.Strip(raw)
	if count := strings.Count(normalizedOutput(raw), "seed replay line"); count < 2 || count > 3 {
		t.Fatalf("expected committed history to replay at least once after debounced width resize burst, got %q", normalizedOutput(raw))
	}
	for _, line := range strings.Split(plain, "\n") {
		if strings.Count(line, statusStateCircleGlyph+statusLineSpinnerSeparator) > 1 {
			t.Fatalf("expected no duplicated status segment in a single rendered line, got %q", line)
		}
	}
	borderLines := 0
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, strings.Repeat("─", 12)) {
			borderLines++
		}
	}
	if borderLines > 24 {
		t.Fatalf("expected bounded border redraw count during resize, got %d", borderLines)
	}
	if strings.Count(plain, statusStateCircleGlyph+statusLineSpinnerSeparator) > 16 {
		t.Fatalf("expected bounded status redraw count during resize, got %d", strings.Count(plain, statusStateCircleGlyph+statusLineSpinnerSeparator))
	}
}

func TestNativeResizeClearWithoutHistoryRedrawsSingleLiveRegion(t *testing.T) {
	previousDebounce := nativeResizeReplayDebounce
	nativeResizeReplayDebounce = 20 * time.Millisecond
	t.Cleanup(func() {
		nativeResizeReplayDebounce = previousDebounce
	})

	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(nil, closedProjectedRuntimeEvents(), closedAskEvents())
	model.input = "top\ncurrent\nbottom"

	program := tea.NewProgram(
		model,
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(out),
		tea.WithoutSignals(),
	)

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	time.Sleep(40 * time.Millisecond)
	for _, size := range []tea.WindowSizeMsg{
		{Width: 120, Height: 30},
		{Width: 96, Height: 24},
		{Width: 110, Height: 28},
		{Width: 84, Height: 22},
	} {
		program.Send(size)
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(40 * time.Millisecond)
	program.Quit()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}

	raw := out.String()
	if count := strings.Count(raw, "\x1b[2J"); count < 1 {
		t.Fatalf("expected startup clear-screen sequence in no-history path, got %d occurrences in %q", count, raw)
	}
	plain := xansi.Strip(raw)
	if !strings.Contains(plain, "top") || !strings.Contains(plain, "current") || !strings.Contains(plain, "bottom") {
		t.Fatalf("expected multiline input to remain visible after repeated resizes, got %q", plain)
	}
	for _, line := range strings.Split(plain, "\n") {
		if strings.Count(line, statusStateCircleGlyph+statusLineSpinnerSeparator) > 1 {
			t.Fatalf("expected no duplicated status segment in a single rendered line, got %q", line)
		}
		if strings.Count(line, "› ") > 1 {
			t.Fatalf("expected no duplicated input prompt in a single rendered line, got %q", line)
		}
	}
	borderLines := 0
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, strings.Repeat("─", 12)) {
			borderLines++
		}
	}
	if borderLines > 16 {
		t.Fatalf("expected bounded border redraw count in no-history resize path, got %d", borderLines)
	}
	if strings.Count(plain, statusStateCircleGlyph+statusLineSpinnerSeparator) > 12 {
		t.Fatalf("expected bounded status redraw count in no-history resize path, got %d", strings.Count(plain, statusStateCircleGlyph+statusLineSpinnerSeparator))
	}
}

func TestNativeRollbackOverlayCtrlCBalancesAltScreenAndAlternateScroll(t *testing.T) {
	var terminalSequences []string
	originalWriteTerminalSequence := writeTerminalSequence
	writeTerminalSequence = func(sequence string) {
		terminalSequences = append(terminalSequences, sequence)
	}
	defer func() {
		writeTerminalSequence = originalWriteTerminalSequence
	}()

	out := newLockedBuffer()
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
		WithUIInitialTranscript([]UITranscriptEntry{
			{Role: "user", Text: "u1"},
			{Role: "assistant", Text: "a1"},
			{Role: "user", Text: "u2"},
		}),
	)

	program := tea.NewProgram(
		model,
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(out),
		tea.WithoutSignals(),
	)

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	time.Sleep(40 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	time.Sleep(20 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForTestCondition(t, 2*time.Second, "rollback overlay to open", func() bool {
		return model.rollback.isSelecting() && model.surface() == uiSurfaceRollbackSelection && model.view.Mode() == tui.ModeDetail
	})
	waitForTestCondition(t, 2*time.Second, "rollback overlay alt-screen enter to render", func() bool {
		return strings.Contains(out.String(), "\x1b[?1049h")
	})
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("program did not terminate")
	}

	raw := out.String()
	enterAlt := strings.Count(raw, "\x1b[?1049h")
	exitAlt := strings.Count(raw, "\x1b[?1049l")
	if enterAlt != exitAlt {
		t.Fatalf("expected balanced alt-screen enter/exit sequences, enter=%d exit=%d", enterAlt, exitAlt)
	}
	if enterAlt == 0 {
		t.Fatal("expected rollback overlay in native mode to enter alt-screen under auto policy")
	}
	sequenceLog := strings.Join(terminalSequences, "")
	enableAltScroll := strings.Count(sequenceLog, "\x1b[?1007h")
	if enableAltScroll != 0 {
		t.Fatalf("did not expect rollback picker to enable alternate-scroll, enable=%d log=%q", enableAltScroll, sequenceLog)
	}
}
