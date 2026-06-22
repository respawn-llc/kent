package runtimecontrol

import (
	"context"
	"testing"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/tools"
	"core/shared/serverapi"
	"core/shared/toolspec"
	"core/shared/transcript"
)

var runtimeControlOpenAICapabilities = llm.ProviderCapabilities{
	ProviderID:               "openai",
	SupportsResponsesAPI:     true,
	SupportsResponsesCompact: true,
	IsOpenAIFirstParty:       true,
}

func TestServiceSetThinkingLevelReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeSetThinkingLevelRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Level: "high"}

	if err := service.SetThinkingLevel(context.Background(), req); err != nil {
		t.Fatalf("SetThinkingLevel first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	if err := service.SetThinkingLevel(context.Background(), req); err != nil {
		t.Fatalf("SetThinkingLevel replay: %v", err)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
	if got := engine.ThinkingLevel(); got != "high" {
		t.Fatalf("thinking level = %q, want high", got)
	}
}

func TestServiceSetFastModeEnabledReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5", ProviderCapabilitiesOverride: &runtimeControlOpenAICapabilities})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeSetFastModeEnabledRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Enabled: true}

	first, err := service.SetFastModeEnabled(context.Background(), req)
	if err != nil {
		t.Fatalf("SetFastModeEnabled first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	second, err := service.SetFastModeEnabled(context.Background(), req)
	if err != nil {
		t.Fatalf("SetFastModeEnabled replay: %v", err)
	}
	if first != second {
		t.Fatalf("responses = (%+v, %+v), want identical replay", first, second)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
	if !engine.FastModeEnabled() {
		t.Fatal("expected fast mode to remain enabled")
	}
}

func TestServiceSetReviewerEnabledReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5", Reviewer: runtime.ReviewerConfig{Model: "gpt-5", ClientFactory: func() (llm.Client, error) { return &runtimeControlFakeClient{}, nil }}})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeSetReviewerEnabledRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Enabled: true}

	first, err := service.SetReviewerEnabled(context.Background(), req)
	if err != nil {
		t.Fatalf("SetReviewerEnabled first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	second, err := service.SetReviewerEnabled(context.Background(), req)
	if err != nil {
		t.Fatalf("SetReviewerEnabled replay: %v", err)
	}
	if first != second {
		t.Fatalf("responses = (%+v, %+v), want identical replay", first, second)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
	if got := engine.ReviewerFrequency(); got != "edits" {
		t.Fatalf("reviewer frequency = %q, want edits", got)
	}
}

func TestServiceSetAutoCompactionEnabledReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeSetAutoCompactionEnabledRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Enabled: false}

	first, err := service.SetAutoCompactionEnabled(context.Background(), req)
	if err != nil {
		t.Fatalf("SetAutoCompactionEnabled first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	second, err := service.SetAutoCompactionEnabled(context.Background(), req)
	if err != nil {
		t.Fatalf("SetAutoCompactionEnabled replay: %v", err)
	}
	if first != second {
		t.Fatalf("responses = (%+v, %+v), want identical replay", first, second)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
	if engine.AutoCompactionEnabled() {
		t.Fatal("expected auto compaction to remain disabled")
	}
}

func TestServiceCompactContextReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, engine, client := newRuntimeControlCompactionFixture(t)
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeCompactContextRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Args: "compact now"}

	if err := service.CompactContext(context.Background(), req); err != nil {
		t.Fatalf("CompactContext first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	if err := service.CompactContext(context.Background(), req); err != nil {
		t.Fatalf("CompactContext replay: %v", err)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
	if client.compactionCalls != 1 {
		t.Fatalf("compaction call count = %d, want 1", client.compactionCalls)
	}
	if got := countEventsByKind(t, store, "history_replaced"); got != 1 {
		t.Fatalf("history_replaced event count = %d, want 1", got)
	}
}

func TestServiceCompactContextForPreSubmitReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, engine, client := newRuntimeControlCompactionFixture(t)
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeCompactContextForPreSubmitRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1"}

	if err := service.CompactContextForPreSubmit(context.Background(), req); err != nil {
		t.Fatalf("CompactContextForPreSubmit first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	if err := service.CompactContextForPreSubmit(context.Background(), req); err != nil {
		t.Fatalf("CompactContextForPreSubmit replay: %v", err)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
	if client.compactionCalls != 1 {
		t.Fatalf("compaction call count = %d, want 1", client.compactionCalls)
	}
	if got := countEventsByKind(t, store, "history_replaced"); got != 1 {
		t.Fatalf("history_replaced event count = %d, want 1", got)
	}
}

func TestServiceInterruptReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeInterruptRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1"}

	if err := service.Interrupt(context.Background(), req); err != nil {
		t.Fatalf("Interrupt first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	if err := service.Interrupt(context.Background(), req); err != nil {
		t.Fatalf("Interrupt replay: %v", err)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
}

func newRuntimeControlCompactionFixture(t *testing.T) (*session.Store, *runtime.Engine, *runtimeControlFakeClient) {
	t.Helper()
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{
		responses: []llm.Response{{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200000},
		}},
		compactionResponses: []llm.CompactionResponse{{
			OutputItems: []llm.ResponseItem{
				{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, MessageType: llm.MessageTypeCompactionSummary, Content: "summary"},
				{Type: llm.ResponseItemTypeCompaction, EncryptedContent: "checkpoint"},
			},
			Usage:             llm.Usage{WindowTokens: 200000},
			TrimmedItemsCount: 1,
		}},
	}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5", ProviderCapabilitiesOverride: &runtimeControlOpenAICapabilities})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	if _, err := engine.SubmitUserMessage(context.Background(), "hello"); err != nil {
		t.Fatalf("seed runtime transcript: %v", err)
	}
	return store, engine, client
}

func countEventsByKind(t *testing.T, store *session.Store, kind string) int {
	t.Helper()
	events, err := sessiontest.CollectEvents(store)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	count := 0
	for _, evt := range events {
		if evt.Kind == kind {
			count++
		}
	}
	return count
}

func TestServiceSubmitUserMessageReplaysSuccessfulRetryAfterLeaseRotation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	first := serverapi.RuntimeSubmitUserMessageRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Text: "hello"}

	firstResp, err := service.SubmitUserMessage(context.Background(), first)
	if err != nil {
		t.Fatalf("SubmitUserMessage first: %v", err)
	}
	second := first
	second.ControllerLeaseID = "lease-2"
	secondResp, err := service.SubmitUserMessage(context.Background(), second)
	if err != nil {
		t.Fatalf("SubmitUserMessage replay after lease rotation: %v", err)
	}
	if firstResp != secondResp {
		t.Fatalf("responses = (%+v, %+v), want identical replay", firstResp, secondResp)
	}
	if client.calls != 1 {
		t.Fatalf("generate call count = %d, want 1", client.calls)
	}
}

func TestServiceSubmitUserShellCommandReplaysSuccessfulRetryAfterLeaseRotation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeShellHandler{}}), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	first := serverapi.RuntimeSubmitUserShellCommandRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Command: "pwd"}

	if err := service.SubmitUserShellCommand(context.Background(), first); err != nil {
		t.Fatalf("SubmitUserShellCommand first: %v", err)
	}
	second := first
	second.ControllerLeaseID = "lease-2"
	if err := service.SubmitUserShellCommand(context.Background(), second); err != nil {
		t.Fatalf("SubmitUserShellCommand replay after lease rotation: %v", err)
	}
	if got := countDirectShellCommandMessages(t, store, "pwd"); got != 1 {
		t.Fatalf("direct shell message count = %d, want 1", got)
	}
}

func TestServiceAppendCommittedEntryDedupesSuccessfulRetry(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	req := serverapi.RuntimeAppendCommittedEntryRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Role: "warning", Text: "be careful"}

	if err := service.AppendCommittedEntry(context.Background(), req); err != nil {
		t.Fatalf("AppendCommittedEntry first: %v", err)
	}
	if err := service.AppendCommittedEntry(context.Background(), req); err != nil {
		t.Fatalf("AppendCommittedEntry replay: %v", err)
	}
	count := 0
	for _, entry := range engine.ChatSnapshot().Entries {
		if entry.Role == "warning" && entry.Text == "be careful" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("local entry count = %d, want 1", count)
	}
}

func TestServiceAppendCommittedEntryReplaysVisibility(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	req := serverapi.RuntimeAppendCommittedEntryRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Role: "warning", Text: "visible warning", Visibility: string(transcript.EntryVisibilityAll)}

	if err := service.AppendCommittedEntry(context.Background(), req); err != nil {
		t.Fatalf("AppendCommittedEntry first: %v", err)
	}
	if err := service.AppendCommittedEntry(context.Background(), req); err != nil {
		t.Fatalf("AppendCommittedEntry replay: %v", err)
	}
	count := 0
	for _, entry := range engine.ChatSnapshot().Entries {
		if entry.Role == "warning" && entry.Text == "visible warning" {
			count++
			if entry.Visibility != transcript.EntryVisibilityAll {
				t.Fatalf("entry visibility = %q, want all", entry.Visibility)
			}
		}
	}
	if count != 1 {
		t.Fatalf("visible warning entry count = %d, want 1", count)
	}
}

func TestServiceSubmitQueuedUserMessagesDedupesSuccessfulRetry(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	engine.QueueUserMessage("hello")
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	req := serverapi.RuntimeSubmitQueuedUserMessagesRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1"}

	first, err := service.SubmitQueuedUserMessages(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitQueuedUserMessages first: %v", err)
	}
	second, err := service.SubmitQueuedUserMessages(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitQueuedUserMessages replay: %v", err)
	}
	if first != second {
		t.Fatalf("responses = (%+v, %+v), want identical replay", first, second)
	}
	if client.calls != 1 {
		t.Fatalf("generate call count = %d, want 1", client.calls)
	}
	if got := countUserMessagesWithContent(t, store, "hello"); got != 1 {
		t.Fatalf("queued user flush count = %d, want 1", got)
	}
}

func TestServiceDiscardQueuedUserMessageDedupesSuccessfulRetry(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	firstQueued := engine.QueueUserMessage("same")
	otherQueued := engine.QueueUserMessage("other")
	duplicateQueued := engine.QueueUserMessage("same")
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	req := serverapi.RuntimeDiscardQueuedUserMessageRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", QueueItemID: duplicateQueued.ID}

	first, err := service.DiscardQueuedUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("DiscardQueuedUserMessage first: %v", err)
	}
	second, err := service.DiscardQueuedUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("DiscardQueuedUserMessage replay: %v", err)
	}
	if !first.Discarded || !second.Discarded {
		t.Fatalf("discard results = (%t, %t), want both true", first.Discarded, second.Discarded)
	}
	if !engine.DiscardQueuedUserMessage(firstQueued.ID) {
		t.Fatal("expected first duplicate text item to remain")
	}
	if !engine.DiscardQueuedUserMessage(otherQueued.ID) {
		t.Fatal("expected other queued item to remain")
	}
	if engine.DiscardQueuedUserMessage(duplicateQueued.ID) {
		t.Fatal("did not expect discarded queue item to remain")
	}
}

func TestServiceRecordPromptHistoryDedupesSuccessfulRetry(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	history := newRuntimeControlPromptHistoryStore(store.Meta().SessionID)
	service := NewService(stubRuntimeResolver{engine: engine}, nil).WithPromptHistoryStore(history)
	req := serverapi.RuntimeRecordPromptHistoryRequest{ClientRequestID: "req-1", SessionID: store.Meta().SessionID, ControllerLeaseID: "lease-1", Text: "/resume"}

	if err := service.RecordPromptHistory(context.Background(), req); err != nil {
		t.Fatalf("RecordPromptHistory first: %v", err)
	}
	if err := service.RecordPromptHistory(context.Background(), req); err != nil {
		t.Fatalf("RecordPromptHistory replay: %v", err)
	}
	if got := countPromptHistoryEvents(t, store, "/resume"); got != 1 {
		t.Fatalf("prompt history count = %d, want 1", got)
	}
}

func countPromptHistoryEvents(t *testing.T, store *session.Store, text string) int {
	t.Helper()
	registered, ok := runtimeControlPromptHistoryStores.Load(store.Meta().SessionID)
	if !ok {
		return 0
	}
	history := registered.(*runtimeControlPromptHistoryStore)
	history.mu.Lock()
	defer history.mu.Unlock()
	count := 0
	for _, record := range history.records {
		if record.Text == text {
			count++
		}
	}
	return count
}
