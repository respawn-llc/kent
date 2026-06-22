package app

import (
	"context"
	"core/cli/tui"
	"core/server/llm"
	"core/server/registry"
	"core/server/runtime"
	"core/server/runtimecontrol"
	"core/server/sessionview"
	"core/server/tools"
	sharedclient "core/shared/client"
	"core/shared/clientui"
	"core/shared/serverapi"
	"core/shared/toolspec"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type countingSessionViewClient struct {
	view              clientui.RuntimeMainView
	page              clientui.TranscriptPage
	suffix            clientui.CommittedTranscriptSuffix
	suffixErr         error
	pageForRequest    func(serverapi.SessionTranscriptPageRequest) clientui.TranscriptPage
	count             atomic.Int32
	mainViewCount     atomic.Int32
	pageCount         atomic.Int32
	suffixCount       atomic.Int32
	lastTranscriptReq serverapi.SessionTranscriptPageRequest
	lastSuffixReq     serverapi.SessionCommittedTranscriptSuffixRequest
}

type runtimeClientWithoutCachedMainView struct {
	clientui.RuntimeClient
	mainView      clientui.RuntimeMainView
	mainViewCalls int
	suffixReq     clientui.CommittedTranscriptSuffixRequest
}

func (c *runtimeClientWithoutCachedMainView) MainView() clientui.RuntimeMainView {
	c.mainViewCalls++
	return c.mainView
}

func (c *runtimeClientWithoutCachedMainView) RefreshCommittedTranscriptSuffix(req clientui.CommittedTranscriptSuffixRequest) (clientui.CommittedTranscriptSuffix, error) {
	c.suffixReq = req
	return clientui.CommittedTranscriptSuffix{
		SessionID:             c.mainView.Session.SessionID,
		Revision:              c.mainView.Session.Transcript.Revision,
		CommittedEntryCount:   c.mainView.Session.Transcript.CommittedEntryCount,
		StartEntryCount:       req.AfterEntryCount,
		NextEntryCount:        c.mainView.Session.Transcript.CommittedEntryCount,
		ConversationFreshness: c.mainView.Session.ConversationFreshness,
	}, nil
}

func (c *countingSessionViewClient) GetSessionMainView(context.Context, serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
	c.count.Add(1)
	c.mainViewCount.Add(1)
	return serverapi.SessionMainViewResponse{MainView: c.view}, nil
}

func (c *countingSessionViewClient) GetSessionTranscriptPage(ctx context.Context, req serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
	_ = ctx
	c.lastTranscriptReq = req
	c.count.Add(1)
	c.pageCount.Add(1)
	if c.pageForRequest != nil {
		return serverapi.SessionTranscriptPageResponse{Transcript: c.pageForRequest(req)}, nil
	}
	return serverapi.SessionTranscriptPageResponse{Transcript: c.page}, nil
}

func (c *countingSessionViewClient) GetSessionCommittedTranscriptSuffix(ctx context.Context, req serverapi.SessionCommittedTranscriptSuffixRequest) (serverapi.SessionCommittedTranscriptSuffixResponse, error) {
	_ = ctx
	c.lastSuffixReq = req
	c.count.Add(1)
	c.suffixCount.Add(1)
	if c.suffixErr != nil {
		return serverapi.SessionCommittedTranscriptSuffixResponse{}, c.suffixErr
	}
	return serverapi.SessionCommittedTranscriptSuffixResponse{Suffix: c.suffix}, nil
}

func (*countingSessionViewClient) GetRun(context.Context, serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
	return serverapi.RunGetResponse{}, nil
}

type blockingSessionViewClient struct{}

func (blockingSessionViewClient) GetSessionMainView(ctx context.Context, _ serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
	<-ctx.Done()
	return serverapi.SessionMainViewResponse{}, ctx.Err()
}

func (blockingSessionViewClient) GetSessionTranscriptPage(ctx context.Context, _ serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
	<-ctx.Done()
	return serverapi.SessionTranscriptPageResponse{}, ctx.Err()
}

func (blockingSessionViewClient) GetRun(context.Context, serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
	return serverapi.RunGetResponse{}, nil
}

type blockingCountingSessionViewClient struct {
	count atomic.Int32
}

func (c *blockingCountingSessionViewClient) GetSessionMainView(ctx context.Context, _ serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
	c.count.Add(1)
	<-ctx.Done()
	return serverapi.SessionMainViewResponse{}, ctx.Err()
}

func (c *blockingCountingSessionViewClient) GetSessionTranscriptPage(ctx context.Context, _ serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
	<-ctx.Done()
	return serverapi.SessionTranscriptPageResponse{}, ctx.Err()
}

func (*blockingCountingSessionViewClient) GetRun(context.Context, serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
	return serverapi.RunGetResponse{}, nil
}

type mutableRuntimeResolver struct {
	mu     sync.Mutex
	engine *runtime.Engine
}

func (r *mutableRuntimeResolver) Set(engine *runtime.Engine) {
	r.mu.Lock()
	r.engine = engine
	r.mu.Unlock()
}

func (r *mutableRuntimeResolver) ResolveRuntime(context.Context, string) (*runtime.Engine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.engine, nil
}

type flakySessionViewClient struct {
	mu        sync.Mutex
	responses []serverapi.SessionMainViewResponse
	pages     []serverapi.SessionTranscriptPageResponse
	errs      []error
	count     int
}

func (c *flakySessionViewClient) GetSessionMainView(context.Context, serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := c.count
	c.count++
	if idx < len(c.errs) && c.errs[idx] != nil {
		return serverapi.SessionMainViewResponse{}, c.errs[idx]
	}
	if idx < len(c.responses) {
		return c.responses[idx], nil
	}
	if len(c.responses) > 0 {
		return c.responses[len(c.responses)-1], nil
	}
	return serverapi.SessionMainViewResponse{}, nil
}

func (c *flakySessionViewClient) GetSessionTranscriptPage(context.Context, serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := c.count
	c.count++
	if idx < len(c.errs) && c.errs[idx] != nil {
		return serverapi.SessionTranscriptPageResponse{}, c.errs[idx]
	}
	if idx < len(c.pages) {
		return c.pages[idx], nil
	}
	if len(c.pages) > 0 {
		return c.pages[len(c.pages)-1], nil
	}
	return serverapi.SessionTranscriptPageResponse{}, nil
}

func (c *flakySessionViewClient) GetRun(context.Context, serverapi.RunGetRequest) (serverapi.RunGetResponse, error) {
	return serverapi.RunGetResponse{}, nil
}

type runtimeClientFakeLLM struct {
	mu        sync.Mutex
	responses []llm.Response
}

func (f *runtimeClientFakeLLM) Generate(_ context.Context, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.responses) == 0 {
		return llm.Response{}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *runtimeClientFakeLLM) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{
		ProviderID:                    "openai",
		SupportsResponsesAPI:          true,
		SupportsResponsesCompact:      true,
		SupportsReasoningEncrypted:    true,
		SupportsServerSideContextEdit: true,
		IsOpenAIFirstParty:            true,
	}, nil
}

type runtimeClientBlockingTool struct {
	started chan struct{}
	release chan struct{}
}

func (t runtimeClientBlockingTool) Call(_ context.Context, c tools.Call) (tools.Result, error) {
	select {
	case <-t.started:
	default:
		close(t.started)
	}
	<-t.release
	out, _ := json.Marshal(map[string]any{"ok": true})
	return tools.Result{CallID: c.ID, Name: c.Name, Output: out}, nil
}

func newRuntimeClientReadTest(reads sharedclient.SessionViewClient) clientui.RuntimeClient {
	return newUIRuntimeClientWithReads(
		"session-1",
		reads,
		sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(registry.NewRuntimeRegistry(), nil)),
	)
}

func newRuntimeClientReadOnlyTest(reads sharedclient.SessionViewClient) clientui.RuntimeClient {
	return newUIRuntimeClientWithReads(
		"session-1",
		reads,
		sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(nil, nil)),
	)
}

func TestRuntimeClientRefreshTranscriptRequestsRecentTail(t *testing.T) {
	reads := &countingSessionViewClient{page: clientui.TranscriptPage{SessionID: "session-1"}}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)

	if _, err := runtimeClient.RefreshTranscript(); err != nil {
		t.Fatalf("refresh transcript: %v", err)
	}
	if reads.lastTranscriptReq.Window != clientui.TranscriptWindowRecentTail {
		t.Fatalf("window = %q, want ongoing tail", reads.lastTranscriptReq.Window)
	}
}

func TestRuntimeClientLoadTranscriptPageLetsServerApplyDefaultWindow(t *testing.T) {
	reads := &countingSessionViewClient{page: clientui.TranscriptPage{SessionID: "session-1"}}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)

	if _, err := runtimeClient.LoadTranscriptPage(clientui.TranscriptPageRequest{}); err != nil {
		t.Fatalf("load transcript page: %v", err)
	}
	if reads.lastTranscriptReq.Window != "" {
		t.Fatalf("window = %q, want empty server-default request", reads.lastTranscriptReq.Window)
	}
}

func TestRuntimeClientRefreshCommittedTranscriptSuffixUsesSessionViewSuffixAPI(t *testing.T) {
	reads := &countingSessionViewClient{
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:             "session-1",
			Revision:              12,
			CommittedEntryCount:   5,
			StartEntryCount:       2,
			NextEntryCount:        4,
			HasMore:               true,
			Entries:               []clientui.ChatEntry{{Role: "assistant", Text: "reply-002"}, {Role: "assistant", Text: "reply-003"}},
			ConversationFreshness: clientui.ConversationFreshnessEstablished,
		},
	}
	runtimeClient := newRuntimeClientReadOnlyTest(reads).(*sessionRuntimeClient)

	suffix, err := runtimeClient.RefreshCommittedTranscriptSuffix(clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 2, Limit: 2})
	if err != nil {
		t.Fatalf("refresh committed transcript suffix: %v", err)
	}
	if reads.lastSuffixReq.SessionID != "session-1" || reads.lastSuffixReq.AfterEntryCount != 2 || reads.lastSuffixReq.Limit != 2 {
		t.Fatalf("unexpected suffix request: %+v", reads.lastSuffixReq)
	}
	if reads.lastTranscriptReq != (serverapi.SessionTranscriptPageRequest{}) {
		t.Fatalf("did not expect transcript page request, got %+v", reads.lastTranscriptReq)
	}
	if suffix.StartEntryCount != 2 || suffix.NextEntryCount != 4 || len(suffix.Entries) != 2 {
		t.Fatalf("unexpected suffix response: %+v", suffix)
	}
	cached := runtimeClient.SessionView()
	if cached.Transcript.Revision != 12 || cached.Transcript.CommittedEntryCount != 5 {
		t.Fatalf("cached transcript metadata = %+v, want revision 12 count 5", cached.Transcript)
	}
}

func TestRuntimeClientCommittedSuffixDisablesUnsupportedRPC(t *testing.T) {
	reads := &countingSessionViewClient{
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			Revision:     7,
			TotalEntries: 4,
			Offset:       2,
			Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "page fallback"}},
		},
		suffixErr: serverapi.ErrMethodNotFound,
	}
	runtimeClient := newRuntimeClientReadOnlyTest(reads).(*sessionRuntimeClient)
	req := clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 2, Limit: 1}

	suffix, err := runtimeClient.RefreshCommittedTranscriptSuffix(req)
	if err != nil {
		t.Fatalf("refresh committed transcript suffix fallback: %v", err)
	}
	if suffix.StartEntryCount != 2 || suffix.NextEntryCount != 3 || suffix.Entries[0].Text != "page fallback" {
		t.Fatalf("unexpected fallback suffix: %+v", suffix)
	}
	if reads.suffixCount.Load() != 1 || reads.pageCount.Load() != 1 {
		t.Fatalf("first refresh counts suffix=%d page=%d, want 1/1", reads.suffixCount.Load(), reads.pageCount.Load())
	}

	reads.suffixErr = nil
	reads.suffix = clientui.CommittedTranscriptSuffix{
		SessionID:           "session-1",
		CommittedEntryCount: 4,
		StartEntryCount:     2,
		NextEntryCount:      3,
		Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "rpc should stay disabled"}},
	}
	suffix, err = runtimeClient.RefreshCommittedTranscriptSuffix(req)
	if err != nil {
		t.Fatalf("second refresh committed transcript suffix fallback: %v", err)
	}
	if suffix.Entries[0].Text != "page fallback" {
		t.Fatalf("expected cached unsupported capability to keep page fallback, got %+v", suffix)
	}
	if reads.suffixCount.Load() != 1 || reads.pageCount.Load() != 2 {
		t.Fatalf("second refresh counts suffix=%d page=%d, want 1/2", reads.suffixCount.Load(), reads.pageCount.Load())
	}
}

func TestStartupRuntimeTranscriptUsesCommittedSuffixBounding(t *testing.T) {
	reads := &countingSessionViewClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID: "session-1",
			Transcript: clientui.TranscriptMetadata{
				Revision:            10,
				CommittedEntryCount: 600,
			},
		}},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            10,
			CommittedEntryCount: 600,
			StartEntryCount:     100,
			NextEntryCount:      101,
			HasMore:             true,
			Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "reply-100"}},
		},
	}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)

	model := NewProjectedUIModel(runtimeClient, closedProjectedRuntimeEvents(), closedAskEvents()).(*uiModel)

	if reads.lastSuffixReq.AfterEntryCount != 100 {
		t.Fatalf("startup suffix after_entry_count = %d, want 100", reads.lastSuffixReq.AfterEntryCount)
	}
	if reads.lastSuffixReq.Limit != clientui.MaxCommittedTranscriptSuffixLimit {
		t.Fatalf("startup suffix limit = %d, want %d", reads.lastSuffixReq.Limit, clientui.MaxCommittedTranscriptSuffixLimit)
	}
	if reads.lastTranscriptReq != (serverapi.SessionTranscriptPageRequest{}) {
		t.Fatalf("did not expect startup seed to use transcript page request, got %+v", reads.lastTranscriptReq)
	}
	if model.transcriptBaseOffset != 100 || model.transcriptTotalEntries != 600 {
		t.Fatalf("startup transcript metadata base=%d total=%d, want base 100 total 600", model.transcriptBaseOffset, model.transcriptTotalEntries)
	}
	if got := len(model.transcriptEntries); got != 1 {
		t.Fatalf("startup transcript entries = %d, want 1", got)
	}
	if model.transcriptEntries[0].Text != "reply-100" {
		t.Fatalf("startup transcript entry = %+v, want reply-100", model.transcriptEntries[0])
	}
}

func TestWidthResizeReplayFetchesCommittedSuffixBeforeFullReplay(t *testing.T) {
	reads := &countingSessionViewClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID: "session-1",
			Transcript: clientui.TranscriptMetadata{
				Revision:            10,
				CommittedEntryCount: 3,
			},
		}},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            10,
			CommittedEntryCount: 3,
			StartEntryCount:     0,
			NextEntryCount:      1,
			HasMore:             true,
			Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "startup"}},
		},
	}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)
	model := NewProjectedUIModel(runtimeClient, closedProjectedRuntimeEvents(), closedAskEvents()).(*uiModel)

	next, startupCmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = next.(*uiModel)
	if startupCmd != nil {
		if flush, ok := startupCmd().(nativeHistoryFlushMsg); ok {
			next, _ = model.Update(flush)
			model = next.(*uiModel)
		}
	}
	reads.suffix = clientui.CommittedTranscriptSuffix{
		SessionID:           "session-1",
		Revision:            11,
		CommittedEntryCount: 3,
		StartEntryCount:     0,
		NextEntryCount:      3,
		Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "fresh-0"}, {Role: "assistant", Text: "fresh-1"}, {Role: "assistant", Text: "fresh-2"}},
	}
	reads.mainViewCount.Store(0)

	next, resizeCmd := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = next.(*uiModel)
	if resizeCmd == nil {
		t.Fatal("expected resize debounce command")
	}
	model.nativeResizeReplayAt = time.Time{}
	next, fetchCmd := model.Update(nativeResizeReplayMsg{token: model.nativeResizeReplayToken})
	model = next.(*uiModel)
	if fetchCmd == nil {
		t.Fatal("expected resize replay to fetch committed suffix")
	}
	refresh, ok := fetchCmd().(nativeResizeTranscriptSuffixRefreshedMsg)
	if !ok {
		t.Fatalf("expected nativeResizeTranscriptSuffixRefreshedMsg, got %T", fetchCmd())
	}
	if reads.lastSuffixReq.AfterEntryCount != 0 || reads.lastSuffixReq.Limit != clientui.MaxCommittedTranscriptSuffixLimit {
		t.Fatalf("unexpected resize suffix request: %+v", reads.lastSuffixReq)
	}
	if got := reads.mainViewCount.Load(); got != 0 {
		t.Fatalf("native resize requested main view %d time(s), want 0", got)
	}
	next, replayCmd := model.Update(refresh)
	model = next.(*uiModel)
	msgs := collectCmdMessages(t, replayCmd)
	foundFreshReplay := false
	for _, msg := range msgs {
		flush, ok := msg.(nativeHistoryFlushMsg)
		if ok && strings.Contains(flush.Text, "fresh-0") && strings.Contains(flush.Text, "fresh-2") {
			foundFreshReplay = true
		}
	}
	if !foundFreshReplay {
		t.Fatalf("expected full replay from refreshed suffix, got msgs=%+v", msgs)
	}
	if model.transcriptRevision != 11 || model.transcriptTotalEntries != 3 {
		t.Fatalf("model transcript metadata revision=%d total=%d, want revision 11 total 3", model.transcriptRevision, model.transcriptTotalEntries)
	}
}

func TestCommittedRuntimeEventPermanentOutputUsesCommittedSuffix(t *testing.T) {
	reads := &countingSessionViewClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID: "session-1",
			Transcript: clientui.TranscriptMetadata{
				Revision:            1,
				CommittedEntryCount: 1,
			},
		}},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            1,
			CommittedEntryCount: 1,
			StartEntryCount:     0,
			NextEntryCount:      1,
			Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
		},
	}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)
	model := NewProjectedUIModel(runtimeClient, closedProjectedRuntimeEvents(), closedAskEvents()).(*uiModel)
	model.termWidth = 100
	model.termHeight = 20
	model.windowSizeKnown = true
	reads.suffix = clientui.CommittedTranscriptSuffix{
		SessionID:           "session-1",
		Revision:            2,
		CommittedEntryCount: 2,
		StartEntryCount:     1,
		NextEntryCount:      2,
		Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "authoritative suffix"}},
	}

	cmd := model.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         2,
		CommittedEntryCount:        2,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "stale event payload", Phase: string(llm.MessagePhaseFinal)}},
	}, true).cmd
	msgs := collectCmdMessages(t, cmd)
	var refresh runtimeCommittedTranscriptSuffixRefreshedMsg
	for _, msg := range msgs {
		if typed, ok := msg.(runtimeCommittedTranscriptSuffixRefreshedMsg); ok {
			refresh = typed
		}
	}
	if refresh.token == 0 {
		t.Fatalf("expected committed suffix refresh, got %+v", msgs)
	}
	if reads.lastSuffixReq.AfterEntryCount != 1 || reads.lastSuffixReq.Limit != 1 {
		t.Fatalf("unexpected committed suffix request: %+v", reads.lastSuffixReq)
	}
	if strings.Contains(stripANSIAndTrimRight(model.view.OngoingSnapshot()), "stale event payload") {
		t.Fatalf("event payload rendered before suffix response: %q", stripANSIAndTrimRight(model.view.OngoingSnapshot()))
	}

	next, applyCmd := model.Update(refresh)
	model = next.(*uiModel)
	_ = collectCmdMessages(t, applyCmd)
	ongoing := stripANSIAndTrimRight(model.view.OngoingSnapshot())
	if !strings.Contains(ongoing, "authoritative suffix") {
		t.Fatalf("expected authoritative suffix rendered, got %q", ongoing)
	}
	if strings.Contains(ongoing, "stale event payload") {
		t.Fatalf("event payload leaked into permanent output: %q", ongoing)
	}
}

func TestCommittedSuffixRequestUsesDeliveryCursorNotLocalProjection(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(2, 10)
	for i := 0; i < 8; i++ {
		m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: tui.TranscriptRoleAssistant, Text: "stale"})
	}

	req := committedTranscriptSuffixRequestForEvent(m, clientui.Event{
		CommittedTranscriptChanged: true,
		CommittedEntryCount:        5,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "committed"}},
	})

	if req.AfterEntryCount != 2 {
		t.Fatalf("suffix request after_entry_count = %d, want delivery cursor 2", req.AfterEntryCount)
	}
	if req.Limit != 3 {
		t.Fatalf("suffix request limit = %d, want committed gap 3", req.Limit)
	}
}

func TestUserMessageFlushedAdvancesDeliveryCursorBeforeFollowingSuffix(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(0, 1)

	cmd := m.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventUserMessageFlushed,
		CommittedTranscriptChanged: true,
		CommittedEntryCount:        1,
		TranscriptRevision:         2,
		CommittedEntryStart:        0,
		CommittedEntryStartSet:     true,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "user", Text: "prompt"}},
	}, true).cmd
	_ = collectCmdMessages(t, cmd)

	if m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount != 1 {
		t.Fatalf("user echo did not advance delivery cursor: %+v", m.ongoingCommittedDelivery)
	}
	req := committedTranscriptSuffixRequestForEvent(m, clientui.Event{
		CommittedTranscriptChanged: true,
		CommittedEntryCount:        2,
		TranscriptRevision:         3,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "answer"}},
	})
	if req.AfterEntryCount != 1 {
		t.Fatalf("following suffix after_entry_count = %d, want user echo cursor 1", req.AfterEntryCount)
	}
	if req.Limit != 1 {
		t.Fatalf("following suffix limit = %d, want 1", req.Limit)
	}
}

func TestCommittedSuffixResponsesAcceptOlderRangeAfterNewerRequestIssued(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.runtimeCommittedSuffixToken = 2

	firstCmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 1,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 1},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            2,
			CommittedEntryCount: 2,
			StartEntryCount:     0,
			NextEntryCount:      1,
			Entries:             []clientui.ChatEntry{{Role: "user", Text: "message1"}},
		},
	})
	secondCmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 2,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 1, Limit: 1},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            3,
			CommittedEntryCount: 2,
			StartEntryCount:     1,
			NextEntryCount:      2,
			Entries:             []clientui.ChatEntry{{Role: "system", Text: "Fast mode enabled"}},
		},
	})

	flushText := normalizedOutput(collectNativeHistoryFlushText(append(collectCmdMessages(t, firstCmd), collectCmdMessages(t, secondCmd)...)))
	if !containsInOrder(flushText, "message1", "Fast mode enabled") {
		t.Fatalf("expected stale older suffix and newer suffix in order, got %q", flushText)
	}
	if strings.Count(flushText, "message1") != 1 {
		t.Fatalf("expected message1 once in permanent output, got %q", flushText)
	}
	loaded := m.view.LoadedTranscriptEntries()
	if len(loaded) != 2 || loaded[0].Text != "message1" || loaded[1].Text != "Fast mode enabled" {
		t.Fatalf("loaded transcript = %+v, want message then toggle", loaded)
	}
}

func TestCommittedSuffixStaleErrorDoesNotRequestFullTranscriptSync(t *testing.T) {
	reads := &countingSessionViewClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}},
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			Revision:     3,
			Offset:       0,
			TotalEntries: 2,
			Entries: []clientui.ChatEntry{
				{Role: "user", Text: "message1"},
				{Role: "system", Text: "Fast mode enabled"},
			},
		},
	}
	client := newUIRuntimeClientWithReads("session-1", reads, sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(nil, nil)))
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.runtimeCommittedSuffixToken = 2

	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 1,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 1},
		err:   errors.New("stale timeout"),
	})
	if cmd != nil {
		t.Fatalf("stale suffix error produced command: %+v", collectCmdMessages(t, cmd))
	}
	if reads.pageCount.Load() != 0 {
		t.Fatalf("stale suffix error requested full transcript sync")
	}
}

func TestCommittedSuffixStaleHasMoreDoesNotRequestFollowUp(t *testing.T) {
	reads := &countingSessionViewClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}},
	}
	client := newUIRuntimeClientWithReads("session-1", reads, sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(nil, nil)))
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.runtimeCommittedSuffixToken = 2
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(2, 3)

	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 1,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: 1},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            2,
			CommittedEntryCount: 3,
			StartEntryCount:     0,
			NextEntryCount:      1,
			HasMore:             true,
			Entries:             []clientui.ChatEntry{{Role: "user", Text: "stale message"}},
		},
	})
	for _, msg := range collectCmdMessages(t, cmd) {
		if _, ok := msg.(runtimeCommittedTranscriptSuffixRefreshedMsg); ok {
			t.Fatalf("stale no-op suffix requested follow-up page: %+v", msg)
		}
	}
}

func TestFutureCommittedSuffixDoesNotReplaceMissingPrefix(t *testing.T) {
	reads := &countingSessionViewClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}},
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			Revision:     3,
			Offset:       0,
			TotalEntries: 2,
			Entries: []clientui.ChatEntry{
				{Role: "user", Text: "message1"},
				{Role: "system", Text: "Fast mode enabled"},
			},
		},
	}
	client := newUIRuntimeClientWithReads("session-1", reads, sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(nil, nil)))
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.runtimeCommittedSuffixToken = 2

	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 2,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 1, Limit: 1},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            3,
			CommittedEntryCount: 2,
			StartEntryCount:     1,
			NextEntryCount:      2,
			Entries:             []clientui.ChatEntry{{Role: "system", Text: "Fast mode enabled"}},
		},
	})

	msgs := collectCmdMessages(t, cmd)
	for _, msg := range msgs {
		if flush, ok := msg.(nativeHistoryFlushMsg); ok && strings.Contains(stripANSIText(flush.Text), "Fast mode enabled") {
			t.Fatalf("future suffix was emitted before missing prefix recovery: %q", stripANSIText(flush.Text))
		}
	}
	foundHydration := false
	for _, msg := range msgs {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			foundHydration = true
		}
	}
	if !foundHydration {
		t.Fatalf("expected future suffix gap to request transcript recovery, got %+v", msgs)
	}
	if loaded := m.view.LoadedTranscriptEntries(); len(loaded) != 0 {
		t.Fatalf("future suffix replaced missing prefix in loaded transcript: %+v", loaded)
	}
}

func TestCommittedSuffixResponseFromPreviousSessionIsIgnored(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.sessionID = "session-current"
	m.runtimeCommittedSuffixToken = 1

	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 1,
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-previous",
			Revision:            2,
			CommittedEntryCount: 1,
			StartEntryCount:     0,
			NextEntryCount:      1,
			Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "previous session answer"}},
		},
	})

	if cmd != nil {
		t.Fatalf("previous-session suffix produced command: %+v", collectCmdMessages(t, cmd))
	}
	if loaded := m.view.LoadedTranscriptEntries(); len(loaded) != 0 {
		t.Fatalf("previous-session suffix mutated transcript: %+v", loaded)
	}
}

func TestCommittedSuffixAppendUsesLoadedTailWhenDeliveryCursorLags(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt", Committed: true}}
	m.transcriptTotalEntries = 1
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(0, 1)
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, TotalEntries: 1})

	cmd := m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            2,
		CommittedEntryCount: 2,
		StartEntryCount:     1,
		NextEntryCount:      2,
		Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "answer"}},
	})

	flushText := normalizedOutput(collectNativeHistoryFlushText(collectCmdMessages(t, cmd)))
	if !containsInOrder(flushText, "prompt", "answer") {
		t.Fatalf("expected loaded prefix and suffix in permanent output, got %q", flushText)
	}
	loaded := m.view.LoadedTranscriptEntries()
	if len(loaded) != 2 || loaded[0].Text != "prompt" || loaded[1].Text != "answer" {
		t.Fatalf("loaded transcript = %+v, want prompt then answer", loaded)
	}
	if got := m.ongoingCommittedDelivery.lastAppliedCommittedEntryCount; got != 2 {
		t.Fatalf("delivery cursor applied count = %d, want 2", got)
	}
}

func TestCommittedSuffixAppendTrimsOverlappedRowsAlreadyDelivered(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(0, 1)

	firstCmd := m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            2,
		CommittedEntryCount: 1,
		StartEntryCount:     0,
		NextEntryCount:      1,
		Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "original final", Phase: string(llm.MessagePhaseFinal)}},
	})
	if firstCmd == nil {
		t.Fatal("expected first suffix to schedule native flush")
	}
	if m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount != 0 {
		t.Fatalf("expected first suffix to wait for native flush ack before advancing emitted cursor, got %+v", m.ongoingCommittedDelivery)
	}
	if m.ongoingCommittedDelivery.lastAppliedCommittedEntryCount != 1 {
		t.Fatalf("expected first suffix to advance applied cursor immediately, got %+v", m.ongoingCommittedDelivery)
	}

	overlappedCmd := m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            4,
		CommittedEntryCount: 3,
		StartEntryCount:     0,
		NextEntryCount:      3,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "original final", Phase: string(llm.MessagePhaseFinal)},
			{Role: "reviewer_suggestions", Text: "Supervisor suggested:\n1. Tighten final answer.", CondensedText: "Supervisor made 1 suggestion."},
			{Role: "assistant", Text: "updated final after review", Phase: string(llm.MessagePhaseFinal)},
		},
	})
	overlappedFlush := collectNativeHistoryFlushText(collectCmdMessages(t, overlappedCmd))
	if strings.Contains(overlappedFlush, "original final") {
		t.Fatalf("expected overlapped suffix append to skip already delivered final answer, got %q", overlappedFlush)
	}
	if !strings.Contains(overlappedFlush, "Supervisor made 1 suggestion.") || !strings.Contains(overlappedFlush, "updated final after review") {
		t.Fatalf("expected overlapped suffix append to emit only new supervisor/final rows, got %q", overlappedFlush)
	}
	counts := map[string]int{}
	for _, entry := range m.transcriptEntries {
		counts[string(entry.Role)+":"+entry.Text]++
	}
	if counts["assistant:original final"] != 1 {
		t.Fatalf("expected original final once in transcript, got %+v", m.transcriptEntries)
	}
	if counts["assistant:updated final after review"] != 1 {
		t.Fatalf("expected updated final once in transcript, got %+v", m.transcriptEntries)
	}
}

func TestCommittedSuffixAppendIncludesServerCommittedEntryWithoutLocalEcho(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedStaticUIModel()
	m.engine = client
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(0, 1)

	if cmd := m.appendLocalEntryWithNoticeID("system", "Fast mode enabled", "notice-1"); cmd == nil {
		t.Fatal("expected local entry persistence command")
	}
	if len(m.transcriptEntries) != 0 {
		t.Fatalf("did not expect runtime-backed append to create a local echo, got %+v", m.transcriptEntries)
	}

	cmd := m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            2,
		CommittedEntryCount: 1,
		StartEntryCount:     0,
		NextEntryCount:      1,
		Entries:             []clientui.ChatEntry{{Role: "system", Text: "Fast mode enabled", NoticeID: "notice-1"}},
	})
	if cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	loaded := m.view.LoadedTranscriptEntries()
	if len(loaded) != 1 {
		t.Fatalf("expected committed entry exactly once, got %+v", loaded)
	}
	if loaded[0].NoticeID != "notice-1" || loaded[0].Text != "Fast mode enabled" {
		t.Fatalf("unexpected loaded transcript entry: %+v", loaded[0])
	}
	if got := m.ongoingCommittedDelivery.lastAppliedCommittedEntryCount; got != 1 {
		t.Fatalf("delivery cursor applied count = %d, want 1", got)
	}
}

func TestCommittedSuffixAppendKeepsCommittedPrefixWithLaterEntries(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedStaticUIModel()
	m.engine = client
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(0, 1)

	_ = m.appendLocalEntryWithNoticeID("system", "Fast mode enabled", "notice-1")
	if len(m.transcriptEntries) != 0 {
		t.Fatalf("did not expect runtime-backed append to create a local echo, got %+v", m.transcriptEntries)
	}
	cmd := m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            2,
		CommittedEntryCount: 2,
		StartEntryCount:     0,
		NextEntryCount:      2,
		Entries: []clientui.ChatEntry{
			{Role: "system", Text: "Fast mode enabled", NoticeID: "notice-1"},
			{Role: "assistant", Text: "authoritative answer", Phase: string(llm.MessagePhaseFinal)},
		},
	})
	if cmd != nil {
		_ = collectCmdMessages(t, cmd)
	}

	loaded := m.view.LoadedTranscriptEntries()
	if len(loaded) != 2 {
		t.Fatalf("expected committed prefix and assistant exactly once, got %+v", loaded)
	}
	if loaded[0].NoticeID != "notice-1" || loaded[0].Text != "Fast mode enabled" {
		t.Fatalf("unexpected committed prefix entry: %+v", loaded[0])
	}
	if loaded[1].Role != "assistant" || loaded[1].Text != "authoritative answer" {
		t.Fatalf("unexpected assistant suffix entry: %+v", loaded[1])
	}
	if got := m.ongoingCommittedDelivery.lastAppliedCommittedEntryCount; got != 2 {
		t.Fatalf("delivery cursor applied count = %d, want 2", got)
	}
	if got := committedTranscriptTailEnd(m); got != 2 {
		t.Fatalf("committed transcript tail = %d, want 2", got)
	}
}

func TestCommittedSuffixAppendClearsStreamBeforeFinalAnswerAppend(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.sawAssistantDelta = true
	m.forwardToView(tui.SetConversationMsg{Ongoing: "final answer"})
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(0, 1)

	cmd := m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            2,
		CommittedEntryCount: 1,
		StartEntryCount:     0,
		NextEntryCount:      1,
		Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "final answer", Phase: string(llm.MessagePhaseFinal)}},
	})

	if got := m.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected committed suffix final to clear live stream, got %q", got)
	}
	if m.sawAssistantDelta {
		t.Fatal("expected committed suffix final to clear assistant delta flag")
	}
	view := stripANSIPreserve(m.view.OngoingSnapshot())
	if got := strings.Count(view, "final answer"); got != 1 {
		t.Fatalf("expected suffix final rendered once, got %d in %q", got, view)
	}
	flush := collectNativeHistoryFlushText(collectCmdMessages(t, cmd))
	if got := strings.Count(normalizedOutput(flush), "final answer"); got != 1 {
		t.Fatalf("expected suffix final native flush once, got %d in %q", got, normalizedOutput(flush))
	}
}

func TestCommittedSuffixAppendCursorAdvancesAfterNativeFlushAck(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(0, 1)

	cmd := m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            2,
		CommittedEntryCount: 1,
		StartEntryCount:     0,
		NextEntryCount:      1,
		Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "answer"}},
	})
	if cmd == nil {
		t.Fatal("expected native flush command")
	}
	if m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount != 0 {
		t.Fatalf("cursor advanced before native flush ack: %+v", m.ongoingCommittedDelivery)
	}
	msg, ok := cmd().(nativeHistoryFlushMsg)
	if !ok {
		t.Fatalf("expected nativeHistoryFlushMsg, got %T", cmd())
	}
	_ = m.handleNativeHistoryFlush(msg)
	if m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount != 1 {
		t.Fatalf("cursor did not advance after native flush ack: %+v", m.ongoingCommittedDelivery)
	}
}

func TestCommittedSuffixDeliveryDoesNotAdvancePastUnresolvedToolTail(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt"}}
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, TotalEntries: 1})
	m.rebaseNativeProjection(m.nativeCommittedProjection(m.transcriptEntries), 0, 1)
	m.acceptNativeProjectionWithoutReplay(m.nativeProjection)
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(1, 1)

	cmd := m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            2,
		CommittedEntryCount: 3,
		StartEntryCount:     1,
		NextEntryCount:      3,
		Entries: []clientui.ChatEntry{
			{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
			{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
		},
	})
	if cmd != nil {
		for _, msg := range collectCmdMessages(t, cmd) {
			if _, ok := msg.(nativeHistoryFlushMsg); ok {
				t.Fatalf("did not expect unresolved tool calls to flush committed history, got %+v", msg)
			}
		}
	}
	if got := m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount; got != 1 {
		t.Fatalf("cursor advanced past unresolved tools: got %d want 1", got)
	}
	if got := len(m.transcriptEntries); got != 3 {
		t.Fatalf("transcript entries after unresolved suffix = %d, want 3", got)
	}

	cmd = m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            3,
		CommittedEntryCount: 4,
		StartEntryCount:     1,
		NextEntryCount:      4,
		Entries: []clientui.ChatEntry{
			{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
			{Role: "tool_result_ok", Text: "out-a", ToolCallID: "call_a"},
			{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
		},
	})
	if cmd == nil {
		t.Fatal("expected completed tool prefix to flush")
	}
	flushes := collectCmdMessages(t, cmd)
	foundFlush := false
	for _, msg := range flushes {
		flush, ok := msg.(nativeHistoryFlushMsg)
		if !ok {
			continue
		}
		foundFlush = true
		if strings.Contains(stripANSIText(flush.Text), "echo b") {
			t.Fatalf("unresolved sibling flushed as committed history: %q", stripANSIText(flush.Text))
		}
		_ = m.handleNativeHistoryFlush(flush)
	}
	if !foundFlush {
		t.Fatalf("expected native flush for resolved tool prefix, got %+v", flushes)
	}
	if got := m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount; got != 3 {
		t.Fatalf("cursor after resolved prefix flush = %d, want 3", got)
	}
	if got := len(m.transcriptEntries); got != 4 {
		t.Fatalf("transcript entries after replacing pending tail = %d, want 4 (%+v)", got, m.transcriptEntries)
	}
	if m.transcriptEntries[1].ToolCallID != "call_a" || m.transcriptEntries[2].ToolCallID != "call_a" || m.transcriptEntries[3].ToolCallID != "call_b" {
		t.Fatalf("pending tail replacement duplicated or reordered entries: %+v", m.transcriptEntries)
	}

	cmd = m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            4,
		CommittedEntryCount: 5,
		StartEntryCount:     3,
		NextEntryCount:      5,
		Entries: []clientui.ChatEntry{
			{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
			{Role: "tool_result_ok", Text: "out-b", ToolCallID: "call_b"},
		},
	})
	for _, msg := range collectCmdMessages(t, cmd) {
		if flush, ok := msg.(nativeHistoryFlushMsg); ok {
			_ = m.handleNativeHistoryFlush(flush)
		}
	}
	if got := m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount; got != 5 {
		t.Fatalf("cursor after final tool flush = %d, want 5", got)
	}
	if pending := tui.PendingOngoingEntries(m.transcriptEntries); len(pending) != 0 {
		t.Fatalf("expected no pending live tool entries after all results, got %+v", pending)
	}
}

func TestCommittedSuffixMultiToolDetailRoundTripLeavesNoLiveSpinnerAfterFinalResults(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.windowSizeKnown = true
	m.termWidth = 100
	m.termHeight = 20
	m.transcriptEntries = []tui.TranscriptEntry{{Role: tui.TranscriptRoleUser, Text: "prompt"}}
	m.transcriptTotalEntries = 1
	m.forwardToView(tui.SetConversationMsg{Entries: m.transcriptEntries, TotalEntries: 1})
	m.rebaseNativeProjection(m.nativeCommittedProjection(m.transcriptEntries), 0, 1)
	m.acceptNativeProjectionWithoutReplay(m.nativeProjection)
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(1, 1)

	_ = collectCmdMessages(t, m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            2,
		CommittedEntryCount: 3,
		StartEntryCount:     1,
		NextEntryCount:      3,
		Entries: []clientui.ChatEntry{
			{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
			{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
		},
	}))
	if pending := tui.PendingOngoingEntries(m.transcriptEntries); len(pending) != 2 {
		t.Fatalf("expected two pending tool calls after starts, got %+v", pending)
	}

	_ = collectCmdMessages(t, m.toggleTranscriptModeWithNativeReplay(false))
	if m.view.Mode() != tui.ModeDetail {
		t.Fatalf("mode = %q, want detail", m.view.Mode())
	}

	_ = collectCmdMessages(t, m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            3,
		CommittedEntryCount: 4,
		StartEntryCount:     1,
		NextEntryCount:      4,
		Entries: []clientui.ChatEntry{
			{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
			{Role: "tool_result_ok", Text: "out-a", ToolCallID: "call_a"},
			{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
		},
	}))

	for _, msg := range collectCmdMessages(t, m.toggleTranscriptModeWithNativeReplay(true)) {
		if flush, ok := msg.(nativeHistoryFlushMsg); ok {
			_ = m.handleNativeHistoryFlush(flush)
		}
	}
	if m.view.Mode() != tui.ModeOngoing {
		t.Fatalf("mode = %q, want ongoing", m.view.Mode())
	}

	for _, msg := range collectCmdMessages(t, m.applyCommittedTranscriptSuffixAppend(clientui.CommittedTranscriptSuffix{
		Revision:            4,
		CommittedEntryCount: 5,
		StartEntryCount:     committedTranscriptTailEnd(m),
		NextEntryCount:      5,
		Entries: []clientui.ChatEntry{
			{Role: "tool_call", Text: "echo a", ToolCallID: "call_a", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo a"}},
			{Role: "tool_result_ok", Text: "out-a", ToolCallID: "call_a"},
			{Role: "tool_call", Text: "echo b", ToolCallID: "call_b", ToolCall: &clientui.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "echo b"}},
			{Role: "tool_result_ok", Text: "out-b", ToolCallID: "call_b"},
		},
	})) {
		if flush, ok := msg.(nativeHistoryFlushMsg); ok {
			_ = m.handleNativeHistoryFlush(flush)
		}
	}

	if pending := tui.PendingOngoingEntries(m.transcriptEntries); len(pending) != 0 {
		t.Fatalf("expected no pending live tool entries after all results, got %+v", pending)
	}
	if view := stripANSIPreserve(m.View()); strings.Contains(view, pendingSpinnerFrameText(0)+" echo") || strings.Contains(view, pendingSpinnerFrameText(1)+" echo") {
		t.Fatalf("did not expect live spinner after final tool results, got %q", view)
	}
	counts := map[string]int{}
	for _, entry := range committedTranscriptEntriesForApp(m.transcriptEntries) {
		if strings.TrimSpace(entry.ToolCallID) != "" {
			counts[string(entry.Role)+":"+entry.ToolCallID]++
		}
	}
	for _, key := range []string{"tool_call:call_a", "tool_result_ok:call_a", "tool_call:call_b", "tool_result_ok:call_b"} {
		if counts[key] != 1 {
			t.Fatalf("committed transcript row count %s = %d, want 1; entries=%+v", key, counts[key], m.transcriptEntries)
		}
	}
}

func TestNativeResizeCommittedTranscriptSuffixFallsBackToMainViewWhenCacheUnavailable(t *testing.T) {
	client := &runtimeClientWithoutCachedMainView{
		RuntimeClient: &runtimeControlFakeClient{},
		mainView: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID: "session-1",
			Transcript: clientui.TranscriptMetadata{
				Revision:            9,
				CommittedEntryCount: 900,
			},
		}},
	}
	m := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	client.mainViewCalls = 0

	cmd := m.requestNativeResizeCommittedTranscriptSuffix(3)
	if cmd == nil {
		t.Fatal("expected native resize suffix request command")
	}
	if client.mainViewCalls != 0 {
		t.Fatalf("main view fallback happened during Update: %d calls", client.mainViewCalls)
	}
	raw := cmd()
	msg, ok := raw.(nativeResizeTranscriptSuffixRefreshedMsg)
	if !ok {
		t.Fatalf("unexpected command message type %T", raw)
	}
	if msg.token != 3 || msg.err != nil {
		t.Fatalf("unexpected resize suffix response: %+v", msg)
	}
	if client.mainViewCalls != 1 {
		t.Fatalf("expected main view fallback in command, got %d calls", client.mainViewCalls)
	}
	wantAfter := 900 - clientui.MaxCommittedTranscriptSuffixLimit
	if client.suffixReq.AfterEntryCount != wantAfter || client.suffixReq.Limit != clientui.MaxCommittedTranscriptSuffixLimit {
		t.Fatalf("resize suffix request = %+v, want after=%d limit=%d", client.suffixReq, wantAfter, clientui.MaxCommittedTranscriptSuffixLimit)
	}
}

func TestCommittedSuffixRefreshedRequestsNextPageWhenCapped(t *testing.T) {
	reads := &countingSessionViewClient{
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            3,
			CommittedEntryCount: 900,
			StartEntryCount:     500,
			NextEntryCount:      750,
			HasMore:             false,
			Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "next capped page"}},
		},
	}
	runtimeClient := newUIRuntimeClientWithReads(
		"session-1",
		reads,
		sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(nil, nil)),
	)
	m := newProjectedTestUIModel(runtimeClient, closedProjectedRuntimeEvents(), closedAskEvents())
	m.runtimeCommittedSuffixToken = 7

	cmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(runtimeCommittedTranscriptSuffixRefreshedMsg{
		token: 7,
		req:   clientui.CommittedTranscriptSuffixRequest{AfterEntryCount: 0, Limit: clientui.MaxCommittedTranscriptSuffixLimit},
		suffix: clientui.CommittedTranscriptSuffix{
			SessionID:           "session-1",
			Revision:            2,
			CommittedEntryCount: 900,
			StartEntryCount:     0,
			NextEntryCount:      500,
			HasMore:             true,
			Entries:             []clientui.ChatEntry{{Role: "assistant", Text: "first capped page"}},
		},
	})
	if cmd == nil {
		t.Fatal("expected follow-up suffix request command")
	}

	var followUp runtimeCommittedTranscriptSuffixRefreshedMsg
	found := false
	for _, msg := range collectCmdMessages(t, cmd) {
		typed, ok := msg.(runtimeCommittedTranscriptSuffixRefreshedMsg)
		if !ok {
			continue
		}
		followUp = typed
		found = true
	}
	if !found {
		t.Fatal("expected capped suffix to schedule a follow-up committed suffix request")
	}
	if followUp.req.AfterEntryCount != 500 {
		t.Fatalf("follow-up after_entry_count = %d, want 500", followUp.req.AfterEntryCount)
	}
	if followUp.req.Limit != clientui.MaxCommittedTranscriptSuffixLimit {
		t.Fatalf("follow-up limit = %d, want max %d", followUp.req.Limit, clientui.MaxCommittedTranscriptSuffixLimit)
	}
	if reads.lastSuffixReq.AfterEntryCount != 500 {
		t.Fatalf("server follow-up after_entry_count = %d, want 500", reads.lastSuffixReq.AfterEntryCount)
	}

	nextCount := reads.count.Load()
	stopCmd := m.handleRuntimeCommittedTranscriptSuffixRefreshed(followUp)
	for _, msg := range collectCmdMessages(t, stopCmd) {
		if _, ok := msg.(runtimeCommittedTranscriptSuffixRefreshedMsg); ok {
			t.Fatalf("did not expect another suffix request after HasMore=false follow-up, got %+v", msg)
		}
	}
	if got := reads.count.Load(); got != nextCount {
		t.Fatalf("unexpected extra suffix request after HasMore=false follow-up: count=%d want %d", got, nextCount)
	}
}

func TestRuntimeClientLoadTranscriptPageAlwaysReadsFromServerAuthority(t *testing.T) {
	reads := &countingSessionViewClient{page: clientui.TranscriptPage{SessionID: "session-1", Offset: 300, TotalEntries: 500}}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)
	req := clientui.TranscriptPageRequest{Offset: 300, Limit: 200}

	if _, err := runtimeClient.LoadTranscriptPage(req); err != nil {
		t.Fatalf("first load transcript page: %v", err)
	}
	if _, err := runtimeClient.LoadTranscriptPage(req); err != nil {
		t.Fatalf("second load transcript page: %v", err)
	}
	if got := reads.count.Load(); got != 2 {
		t.Fatalf("session view call count = %d, want 2", got)
	}
}

func TestRuntimeClientLoadTranscriptPageCachesByRequestKey(t *testing.T) {
	reads := &countingSessionViewClient{page: clientui.TranscriptPage{SessionID: "session-1", TotalEntries: 500}}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)

	if _, err := runtimeClient.LoadTranscriptPage(clientui.TranscriptPageRequest{Offset: 300, Limit: 200}); err != nil {
		t.Fatalf("first load transcript page: %v", err)
	}
	if _, err := runtimeClient.LoadTranscriptPage(clientui.TranscriptPageRequest{Offset: 0, Limit: 250}); err != nil {
		t.Fatalf("second load transcript page: %v", err)
	}
	if got := reads.count.Load(); got != 2 {
		t.Fatalf("session view call count = %d, want 2", got)
	}
}

func TestRuntimeClientRefreshTranscriptBypassesFreshCachedPage(t *testing.T) {
	reads := &countingSessionViewClient{page: clientui.TranscriptPage{SessionID: "session-1"}}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)

	if _, err := runtimeClient.LoadTranscriptPage(clientui.TranscriptPageRequest{}); err != nil {
		t.Fatalf("load transcript page: %v", err)
	}
	if _, err := runtimeClient.RefreshTranscript(); err != nil {
		t.Fatalf("refresh transcript: %v", err)
	}
	if got := reads.count.Load(); got != 2 {
		t.Fatalf("session view call count = %d, want 2", got)
	}
}

func TestRuntimeClientLoadTranscriptPageDoesNotPopulateTranscriptAccessor(t *testing.T) {
	reads := &countingSessionViewClient{
		pageForRequest: func(req serverapi.SessionTranscriptPageRequest) clientui.TranscriptPage {
			if req.Window == clientui.TranscriptWindowRecentTail {
				return clientui.TranscriptPage{
					SessionID:    "session-1",
					Offset:       0,
					TotalEntries: 500,
					Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "tail"}},
				}
			}
			return clientui.TranscriptPage{
				SessionID:    "session-1",
				Offset:       req.Offset,
				TotalEntries: 500,
				Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "paged"}},
			}
		},
	}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)

	if _, err := runtimeClient.RefreshTranscript(); err != nil {
		t.Fatalf("refresh transcript: %v", err)
	}
	if _, err := runtimeClient.LoadTranscriptPage(clientui.TranscriptPageRequest{Offset: 300, Limit: 100}); err != nil {
		t.Fatalf("load transcript page: %v", err)
	}
	page := runtimeClient.Transcript()
	if page.SessionID != "session-1" || len(page.Entries) != 0 {
		t.Fatalf("transcript accessor page = %+v, want empty session page", page)
	}
	if got := reads.count.Load(); got != 2 {
		t.Fatalf("session view call count = %d, want 2", got)
	}
}

func TestRuntimeClientTranscriptDoesNotReadFromServer(t *testing.T) {
	reads := &countingSessionViewClient{
		pageForRequest: func(req serverapi.SessionTranscriptPageRequest) clientui.TranscriptPage {
			if req.Window == clientui.TranscriptWindowRecentTail {
				return clientui.TranscriptPage{
					SessionID:    "session-1",
					Offset:       490,
					TotalEntries: 500,
					Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "tail"}},
				}
			}
			return clientui.TranscriptPage{
				SessionID:    "session-1",
				Offset:       req.Offset,
				TotalEntries: 500,
				Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "paged"}},
			}
		},
	}
	runtimeClient := newRuntimeClientReadOnlyTest(reads)

	if _, err := runtimeClient.RefreshTranscript(); err != nil {
		t.Fatalf("refresh transcript: %v", err)
	}
	if _, err := runtimeClient.LoadTranscriptPage(clientui.TranscriptPageRequest{Offset: 0, Limit: 10}); err != nil {
		t.Fatalf("load transcript page: %v", err)
	}

	page := runtimeClient.Transcript()
	if page.SessionID != "session-1" || len(page.Entries) != 0 {
		t.Fatalf("transcript accessor page = %+v, want empty session page", page)
	}
	if got, want := reads.count.Load(), int32(2); got != want {
		t.Fatalf("session view call count = %d, want %d", got, want)
	}
}

func TestRuntimeClientFromEngineDoesNotSeedTranscriptAccessor(t *testing.T) {
	store := createAppRuntimeSession(t)
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleUser, Content: "u1"}); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: "a1", Phase: llm.MessagePhaseFinal}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}
	eng := newAppRuntimeEngineWithStore(t, store, &runtimeClientFakeLLM{}, runtime.Config{})

	runtimeClient := newUIRuntimeClientFromEngine(eng)
	page := runtimeClient.Transcript()

	if page.SessionID != store.Meta().SessionID || len(page.Entries) != 0 {
		t.Fatalf("transcript accessor page = %+v, want empty session page", page)
	}
	authoritative, err := runtimeClient.RefreshTranscript()
	if err != nil {
		t.Fatalf("refresh transcript: %v", err)
	}
	if got, want := authoritative.TotalEntries, 2; got != want {
		t.Fatalf("total entries = %d, want %d", got, want)
	}
	if got, want := len(authoritative.Entries), 2; got != want {
		t.Fatalf("entry count = %d, want %d", got, want)
	}
	if authoritative.Entries[1].Text != "a1" {
		t.Fatalf("expected authoritative transcript tail entry, got %+v", authoritative.Entries)
	}
}

func TestRuntimeClientMainViewIncludesActiveRunFromRealEngine(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	fakeLLM := &runtimeClientFakeLLM{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "working", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{ID: "call_shell_1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)}},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}
	store, eng := newAppRuntimeEngine(t, fakeLLM, runtime.Config{}, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: runtimeClientBlockingTool{started: started, release: release}})
	runtimeRegistry := registry.NewRuntimeRegistry()
	runtimeRegistry.Register(store.Meta().SessionID, eng)

	runtimeClient := newUIRuntimeClientWithReads(
		store.Meta().SessionID,
		sharedclient.NewLoopbackSessionViewClient(sessionview.NewService(nil, runtimeRegistry, nil)),
		sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(runtimeRegistry, runtimeRegistry)),
	)
	result := make(chan error, 1)
	go func() {
		_, submitErr := eng.SubmitUserMessage(context.Background(), "run tools")
		result <- submitErr
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active run")
	}

	view := runtimeClient.MainView()
	if view.Session.SessionID != store.Meta().SessionID {
		t.Fatalf("session id = %q, want %q", view.Session.SessionID, store.Meta().SessionID)
	}
	if view.ActiveRun == nil {
		t.Fatal("expected active run in main view")
	}
	if view.ActiveRun.RunID == "" || view.ActiveRun.StepID == "" {
		t.Fatalf("expected run identifiers, got %+v", view.ActiveRun)
	}
	if view.ActiveRun.SessionID != store.Meta().SessionID {
		t.Fatalf("run session id = %q, want %q", view.ActiveRun.SessionID, store.Meta().SessionID)
	}
	if view.ActiveRun.Status != "running" || view.ActiveRun.StartedAt.IsZero() || !view.ActiveRun.FinishedAt.IsZero() {
		t.Fatalf("unexpected active run payload: %+v", view.ActiveRun)
	}

	close(release)
	if err := <-result; err != nil {
		t.Fatalf("submit user message: %v", err)
	}
}

func TestRuntimeClientMainViewFallsBackToLocalRuntimeProjectionOnReadError(t *testing.T) {
	store := createAppRuntimeSession(t)
	if err := store.SetParentSessionID("parent-123"); err != nil {
		t.Fatalf("set parent session id: %v", err)
	}
	eng := newAppRuntimeEngineWithStore(t, store, &runtimeClientFakeLLM{}, runtime.Config{})
	if err := eng.SetThinkingLevel("high"); err != nil {
		t.Fatalf("set thinking level: %v", err)
	}
	runtimeRegistry := registry.NewRuntimeRegistry()
	runtimeRegistry.Register(store.Meta().SessionID, eng)

	runtimeClient := newUIRuntimeClientWithReads(
		store.Meta().SessionID,
		sharedclient.NewLoopbackSessionViewClient(sessionview.NewService(nil, runtimeRegistry, nil)),
		sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(runtimeRegistry, runtimeRegistry)),
	)
	view := runtimeClient.MainView()
	if view.Session.SessionID != store.Meta().SessionID {
		t.Fatalf("session id = %q, want %q", view.Session.SessionID, store.Meta().SessionID)
	}
	if view.Status.ParentSessionID != "parent-123" {
		t.Fatalf("parent session id = %q, want parent-123", view.Status.ParentSessionID)
	}
	if view.Status.ThinkingLevel != "high" {
		t.Fatalf("thinking level = %q, want high", view.Status.ThinkingLevel)
	}
}

func TestRuntimeClientMainViewSnapshotDoesNotPopulateTranscriptEndpoint(t *testing.T) {
	store := createAppRuntimeSession(t)
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: "seeded from main view", Phase: llm.MessagePhaseFinal}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}
	eng := newAppRuntimeEngineWithStore(t, store, &runtimeClientFakeLLM{}, runtime.Config{})
	runtimeRegistry := registry.NewRuntimeRegistry()
	runtimeRegistry.Register(store.Meta().SessionID, eng)

	runtimeClient := newUIRuntimeClientWithReads(
		store.Meta().SessionID,
		sharedclient.NewLoopbackSessionViewClient(sessionview.NewService(nil, runtimeRegistry, nil)),
		sharedclient.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(runtimeRegistry, runtimeRegistry)),
	)
	view := runtimeClient.MainView()
	if got := len(view.Session.Chat.Entries); got != 0 {
		t.Fatalf("main view chat entry count = %d, want 0", got)
	}
	if page := runtimeClient.Transcript(); len(page.Entries) != 0 {
		t.Fatalf("expected transcript accessor to stay empty before explicit hydration, got %+v", page)
	}

	page, err := runtimeClient.RefreshTranscript()
	if err != nil {
		t.Fatalf("refresh transcript: %v", err)
	}
	if got := len(page.Entries); got != 1 {
		t.Fatalf("transcript entry count = %d, want 1", got)
	}
	if got := page.Entries[0].Text; got != "seeded from main view" {
		t.Fatalf("transcript entry text = %q, want seeded from main view", got)
	}
}

func TestRuntimeClientWithoutClientsIsNil(t *testing.T) {
	if client := newUIRuntimeClientWithReads("session-1", nil, nil); client != nil {
		t.Fatalf("expected nil runtime client, got %#v", client)
	}
	_ = clientui.RuntimeMainView{}
}

func TestRuntimeClientMainViewCachesSuccessfulRead(t *testing.T) {
	reads := &countingSessionViewClient{view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}, Status: clientui.RuntimeStatus{ThinkingLevel: "high"}}}
	runtimeClient := newRuntimeClientReadTest(reads)

	first := runtimeClient.MainView()
	second := runtimeClient.MainView()
	third := runtimeClient.MainView()
	if first.Status.ThinkingLevel != "high" || second.Status.ThinkingLevel != "high" || third.Status.ThinkingLevel != "high" {
		t.Fatalf("expected cached main view to preserve projected status, got %+v / %+v / %+v", first, second, third)
	}
	if got := reads.count.Load(); got != 1 {
		t.Fatalf("main view read count = %d, want 1", got)
	}
}

func TestRuntimeClientRefreshMainViewBypassesCache(t *testing.T) {
	reads := &countingSessionViewClient{view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: "session-1"}, Status: clientui.RuntimeStatus{ThinkingLevel: "high"}}}
	runtimeClient := newRuntimeClientReadTest(reads)
	if _, err := runtimeClient.RefreshMainView(); err != nil {
		t.Fatalf("RefreshMainView: %v", err)
	}
	reads.view.Status.ThinkingLevel = "low"
	refreshed, err := runtimeClient.RefreshMainView()
	if err != nil {
		t.Fatalf("RefreshMainView second call: %v", err)
	}
	if refreshed.Status.ThinkingLevel != "low" {
		t.Fatalf("expected refreshed main view to bypass cache, got %+v", refreshed)
	}
	if got := reads.count.Load(); got != 2 {
		t.Fatalf("refresh main view read count = %d, want 2", got)
	}
}

func TestRuntimeClientMainViewLeavesTranscriptHydrationToTranscriptEndpoint(t *testing.T) {
	reads := &countingSessionViewClient{view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
		SessionID: "session-1",
		Transcript: clientui.TranscriptMetadata{
			Revision:            3,
			CommittedEntryCount: 1,
		},
		Chat: clientui.ChatSnapshot{
			Entries: []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
		},
	}}}
	runtimeClient := newRuntimeClientReadTest(reads)

	view := runtimeClient.MainView()
	if view.Session.SessionID != "session-1" {
		t.Fatalf("session id = %q, want session-1", view.Session.SessionID)
	}
	page := runtimeClient.Transcript()
	if got := len(page.Entries); got != 0 {
		t.Fatalf("transcript entry count = %d, want 0", got)
	}
	if got := reads.count.Load(); got != 1 {
		t.Fatalf("session view call count = %d, want 1", got)
	}
}

func TestRuntimeClientMainViewBootstrapDoesNotSeedStreamingOngoingState(t *testing.T) {
	reads := &countingSessionViewClient{view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
		SessionID: "session-1",
		Transcript: clientui.TranscriptMetadata{
			Revision:            3,
			CommittedEntryCount: 1,
		},
		Chat: clientui.ChatSnapshot{
			Entries: []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
			Ongoing: "NO_OP",
		},
	}}}
	runtimeClient := newRuntimeClientReadTest(reads)

	_ = runtimeClient.MainView()
	page := runtimeClient.Transcript()
	if got := page.Ongoing; got != "" {
		t.Fatalf("bootstrap ongoing text = %q, want empty", got)
	}
}

func TestRuntimeClientRefreshMainViewDoesNotDowngradeCachedTranscriptTail(t *testing.T) {
	reads := &countingSessionViewClient{
		view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{
			SessionID: "session-1",
			Transcript: clientui.TranscriptMetadata{
				Revision:            3,
				CommittedEntryCount: 2,
			},
			Chat: clientui.ChatSnapshot{
				Entries: []clientui.ChatEntry{{Role: "assistant", Text: "seed"}},
			},
		}},
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			Revision:     3,
			TotalEntries: 2,
			Entries: []clientui.ChatEntry{
				{Role: "assistant", Text: "seed"},
				{Role: "reviewer_status", Text: "Supervisor ran and applied 2 suggestions."},
			},
		},
	}
	runtimeClient := newRuntimeClientReadTest(reads)
	if _, err := runtimeClient.RefreshTranscript(); err != nil {
		t.Fatalf("RefreshTranscript: %v", err)
	}
	if _, err := runtimeClient.RefreshMainView(); err != nil {
		t.Fatalf("RefreshMainView: %v", err)
	}
	page, err := runtimeClient.RefreshTranscript()
	if err != nil {
		t.Fatalf("refresh transcript after main view refresh: %v", err)
	}
	if got := len(page.Entries); got != 2 {
		t.Fatalf("transcript entry count = %d, want 2", got)
	}
	if got := page.Entries[1].Role; got != "reviewer_status" {
		t.Fatalf("second transcript role = %q, want reviewer_status", got)
	}
	if got := page.Entries[1].Text; got != "Supervisor ran and applied 2 suggestions." {
		t.Fatalf("second transcript text = %q", got)
	}
	if got := reads.count.Load(); got != 3 {
		t.Fatalf("session view call count = %d, want 3", got)
	}
}

func TestRuntimeClientRefreshTranscriptUpdatesMainViewChatForWindowedRecentTail(t *testing.T) {
	reads := &countingSessionViewClient{
		page: clientui.TranscriptPage{
			SessionID:    "session-1",
			Revision:     3,
			Offset:       490,
			TotalEntries: 500,
			HasMore:      true,
			Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "windowed tail"}},
			Ongoing:      "streaming",
		},
	}
	runtimeClient := newRuntimeClientReadTest(reads)

	if _, err := runtimeClient.RefreshTranscript(); err != nil {
		t.Fatalf("RefreshTranscript: %v", err)
	}
	view := runtimeClient.MainView()
	if got := len(view.Session.Chat.Entries); got != 1 {
		t.Fatalf("main view chat entry count = %d, want 1", got)
	}
	if got := view.Session.Chat.Entries[0].Text; got != "windowed tail" {
		t.Fatalf("main view chat text = %q, want windowed tail", got)
	}
	if got := view.Session.Chat.Ongoing; got != "streaming" {
		t.Fatalf("main view ongoing = %q, want streaming", got)
	}
}

func TestRuntimeClientMainViewFailsFastWhenReadStalls(t *testing.T) {
	withUIRuntimeReadTimeout(t, time.Millisecond)

	runtimeClient := newRuntimeClientReadTest(blockingSessionViewClient{})
	start := time.Now()
	view := runtimeClient.MainView()
	elapsed := time.Since(start)
	if elapsed >= time.Second {
		t.Fatalf("expected stalled main-view read to fail fast, took %v", elapsed)
	}
	if view.Session.SessionID != "session-1" {
		t.Fatalf("expected fallback main view to preserve session id, got %+v", view)
	}
}

func TestRuntimeClientCollaborativeMainViewSeedsBusyFallbackBeforeHydration(t *testing.T) {
	withUIRuntimeReadTimeout(t, time.Millisecond)

	reads := &blockingCountingSessionViewClient{}
	runtimeClient := newRuntimeClientReadTest(reads).(*sessionRuntimeClient)
	runtimeClient.SetAccessMode(serverapi.SessionRuntimeAttachModeCollaborative, []serverapi.SessionRuntimeOperation{
		serverapi.SessionRuntimeOperationQueueUserMessage,
	})

	view := runtimeClient.MainView()
	if view.Session.SessionID != "session-1" {
		t.Fatalf("fallback session id = %q, want session-1", view.Session.SessionID)
	}
	if view.ExternalRuntime == nil || view.ExternalRuntime.State != clientui.ExternalRuntimeStateOwnerRunning || !view.ExternalRuntime.QueueAccepting {
		t.Fatalf("external runtime fallback = %+v, want owner-running accepting", view.ExternalRuntime)
	}
	if got := reads.count.Load(); got != 1 {
		t.Fatalf("main view read count = %d, want one hydration attempt before fallback", got)
	}
}

func TestRuntimeClientCollaborativeMainViewThrottlesFallbackHydrationRetry(t *testing.T) {
	withUIRuntimeReadTimeout(t, time.Millisecond)

	reads := &blockingCountingSessionViewClient{}
	runtimeClient := newRuntimeClientReadTest(reads).(*sessionRuntimeClient)
	runtimeClient.SetAccessMode(serverapi.SessionRuntimeAttachModeCollaborative, []serverapi.SessionRuntimeOperation{
		serverapi.SessionRuntimeOperationQueueUserMessage,
	})

	_ = runtimeClient.MainView()
	_ = runtimeClient.MainView()
	if got := reads.count.Load(); got != 1 {
		t.Fatalf("main view read count before fallback retry cooldown = %d, want 1", got)
	}
}

func TestRuntimeClientCollaborativeRefreshMainViewKeepsBusyFallbackOnReadError(t *testing.T) {
	runtimeClient := newRuntimeClientReadTest(&blockingCountingSessionViewClient{}).(*sessionRuntimeClient)
	runtimeClient.SetAccessMode(serverapi.SessionRuntimeAttachModeCollaborative, nil)

	view, err := runtimeClient.refreshMainViewSync(time.Millisecond)
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if view.ExternalRuntime == nil || view.ExternalRuntime.State != clientui.ExternalRuntimeStateOwnerRunning || !view.ExternalRuntime.QueueAccepting {
		t.Fatalf("external runtime fallback = %+v, want owner-running accepting", view.ExternalRuntime)
	}
}

func TestRuntimeClientCollaborativeRefreshMainViewAllowsAuthoritativeDowngrade(t *testing.T) {
	reads := &countingSessionViewClient{view: clientui.RuntimeMainView{
		Session:         clientui.RuntimeSessionView{SessionID: "session-1"},
		ExternalRuntime: &clientui.ExternalRuntimeStatus{State: clientui.ExternalRuntimeStateRegisteredIdle, QueueAccepting: true},
	}}
	runtimeClient := newRuntimeClientReadTest(reads).(*sessionRuntimeClient)
	runtimeClient.SetAccessMode(serverapi.SessionRuntimeAttachModeCollaborative, nil)

	view, err := runtimeClient.RefreshMainView()
	if err != nil {
		t.Fatalf("RefreshMainView: %v", err)
	}
	if view.ExternalRuntime == nil || view.ExternalRuntime.State != clientui.ExternalRuntimeStateRegisteredIdle || !view.ExternalRuntime.QueueAccepting {
		t.Fatalf("external runtime state = %+v, want authoritative registered-idle accepting", view.ExternalRuntime)
	}
}

func withUIRuntimeReadTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	original := uiRuntimeReadTimeout
	uiRuntimeReadTimeout = timeout
	t.Cleanup(func() { uiRuntimeReadTimeout = original })
}

func TestRuntimeClientMainViewCachesFallbackAfterReadError(t *testing.T) {
	withUIRuntimeReadTimeout(t, time.Millisecond)

	reads := &blockingCountingSessionViewClient{}
	runtimeClient := newRuntimeClientReadTest(reads)

	first := runtimeClient.MainView()
	if first.Session.SessionID != "session-1" {
		t.Fatalf("fallback session id = %q, want session-1", first.Session.SessionID)
	}
	second := runtimeClient.MainView()
	if second.Session.SessionID != "session-1" {
		t.Fatalf("expected cached fallback session id preserved, got %+v", second)
	}
	if got := reads.count.Load(); got != 1 {
		t.Fatalf("main view read count after cached fallback = %d, want 1", got)
	}
}

func TestRuntimeClientRefreshTranscriptPageDoesNotUseHiddenPageCacheOnReadError(t *testing.T) {
	reads := &countingSessionViewClient{}
	runtimeClient := newRuntimeClientReadTest(reads)
	seedReq := clientui.TranscriptPageRequest{Page: 2, PageSize: 25}
	concrete := runtimeClient.(*sessionRuntimeClient)

	var observedErr error
	concrete.SetConnectionStateObserver(func(err error) { observedErr = err })
	concrete.reads = &flakySessionViewClient{errs: []error{context.DeadlineExceeded}}

	page, err := concrete.refreshTranscriptPageSync(seedReq, time.Millisecond)
	if err != context.DeadlineExceeded {
		t.Fatalf("refresh transcript page error = %v, want %v", err, context.DeadlineExceeded)
	}
	if observedErr != context.DeadlineExceeded {
		t.Fatalf("observed connection state error = %v, want %v", observedErr, context.DeadlineExceeded)
	}
	if page.SessionID != "session-1" || len(page.Entries) != 0 {
		t.Fatalf("refresh transcript page fallback = %+v, want empty session page", page)
	}
}

func TestRuntimeClientQueueUserMessageNotifiesConnectionObserverOnFailure(t *testing.T) {
	runtimeClient := newUIRuntimeClientWithReads(
		"session-1",
		&countingSessionViewClient{},
		sharedclient.NewLoopbackRuntimeControlClient(nil),
	)
	concrete, ok := runtimeClient.(*sessionRuntimeClient)
	if !ok {
		t.Fatalf("runtime client type = %T, want *sessionRuntimeClient", runtimeClient)
	}
	var observedErr error
	concrete.SetConnectionStateObserver(func(err error) { observedErr = err })

	concrete.QueueUserMessage("queued input")

	if observedErr == nil || !errors.Is(observedErr, sharedclient.ErrLoopbackServiceUnavailable) {
		t.Fatalf("observed connection state error = %v, want runtime control service unavailable", observedErr)
	}
}

func TestRuntimeClientRefreshTranscriptPageRecoveryReturnsAuthoritativePage(t *testing.T) {
	reads := &countingSessionViewClient{}
	runtimeClient := newRuntimeClientReadTest(reads)
	concrete, ok := runtimeClient.(*sessionRuntimeClient)
	if !ok {
		t.Fatalf("runtime client type = %T, want *sessionRuntimeClient", runtimeClient)
	}
	seedReq := clientui.TranscriptPageRequest{Page: 2, PageSize: 25}
	authoritativePage := clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     8,
		Offset:       25,
		TotalEntries: 41,
		Entries:      []clientui.ChatEntry{{Role: "assistant", Text: "authoritative page"}},
	}

	var observed []error
	concrete.SetConnectionStateObserver(func(err error) {
		observed = append(observed, err)
	})
	concrete.reads = &flakySessionViewClient{
		errs:  []error{context.DeadlineExceeded, nil},
		pages: []serverapi.SessionTranscriptPageResponse{{}, {Transcript: authoritativePage}},
	}

	page, err := concrete.refreshTranscriptPageSync(seedReq, time.Millisecond)
	if err != context.DeadlineExceeded {
		t.Fatalf("refresh transcript page error = %v, want %v", err, context.DeadlineExceeded)
	}
	if page.SessionID != "session-1" || len(page.Entries) != 0 {
		t.Fatalf("refresh transcript page fallback = %+v, want empty session page", page)
	}

	page, err = concrete.refreshTranscriptPageSync(seedReq, time.Millisecond)
	if err != nil {
		t.Fatalf("refresh transcript page recovery error = %v", err)
	}
	if page.SessionID != authoritativePage.SessionID || page.Revision != authoritativePage.Revision || len(page.Entries) != 1 || page.Entries[0].Text != "authoritative page" {
		t.Fatalf("refresh transcript page recovery = %+v, want %+v", page, authoritativePage)
	}
	if len(observed) != 2 || observed[0] != context.DeadlineExceeded || observed[1] != nil {
		t.Fatalf("connection observer sequence = %+v, want [%v <nil>]", observed, context.DeadlineExceeded)
	}
}
