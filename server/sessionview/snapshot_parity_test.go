package sessionview

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/toolspec"
	"core/shared/transcript"
)

func TestSessionSnapshotSourcesParityForMainView(t *testing.T) {
	fixture := newSessionSnapshotParityFixture(t, config.CacheWarningModeVerbose)
	live := mustMainView(t, fixture.live, fixture.sessionID)
	dormant := mustMainView(t, fixture.dormant, fixture.sessionID)

	assertEqual(t, "session id", live.Session.SessionID, dormant.Session.SessionID)
	assertEqual(t, "session name", live.Session.SessionName, dormant.Session.SessionName)
	assertEqual(t, "freshness", live.Session.ConversationFreshness, dormant.Session.ConversationFreshness)
	assertEqual(t, "transcript metadata", live.Session.Transcript, dormant.Session.Transcript)
	assertEqual(t, "execution target", live.Session.ExecutionTarget, dormant.Session.ExecutionTarget)
	assertEqual(t, "parent session id", live.Status.ParentSessionID, dormant.Status.ParentSessionID)
	assertEqual(t, "last committed final", live.Status.LastCommittedAssistantFinalAnswer, dormant.Status.LastCommittedAssistantFinalAnswer)
	assertEqual(t, "update status", live.Status.Update, dormant.Status.Update)
	assertEqual(t, "active run", normalizedRunView(live.ActiveRun), normalizedRunView(dormant.ActiveRun))
}

func TestSessionSnapshotSourcesParityForTranscriptQueries(t *testing.T) {
	fixture := newSessionSnapshotParityFixture(t, config.CacheWarningModeVerbose)
	pageRequests := map[string]serverapi.SessionTranscriptPageRequest{
		"default":      {SessionID: fixture.sessionID},
		"offset_limit": {SessionID: fixture.sessionID, Offset: 1, Limit: 4},
	}
	for name, req := range pageRequests {
		t.Run(name, func(t *testing.T) {
			live := mustTranscriptPage(t, fixture.live, req)
			dormant := mustTranscriptPage(t, fixture.dormant, req)
			assertEqual(t, "transcript page", normalizedTranscriptPage(live), normalizedTranscriptPage(dormant))
		})
	}

	suffixReq := serverapi.SessionCommittedTranscriptSuffixRequest{SessionID: fixture.sessionID, AfterEntryCount: 2, Limit: 3}
	liveSuffix := mustCommittedSuffix(t, fixture.live, suffixReq)
	dormantSuffix := mustCommittedSuffix(t, fixture.dormant, suffixReq)
	assertEqual(t, "committed suffix", normalizedCommittedSuffix(liveSuffix), normalizedCommittedSuffix(dormantSuffix))
}

func TestSessionSnapshotSourcesParityForRunQueriesAndErrors(t *testing.T) {
	fixture := newSessionSnapshotParityFixture(t, config.CacheWarningModeVerbose)
	liveRun := mustRun(t, fixture.live, fixture.sessionID, fixture.completedRunID)
	dormantRun := mustRun(t, fixture.dormant, fixture.sessionID, fixture.completedRunID)
	assertEqual(t, "durable run", normalizedRunView(liveRun), normalizedRunView(dormantRun))

	_, liveErr := fixture.live.GetRun(context.Background(), serverapi.RunGetRequest{SessionID: fixture.sessionID, RunID: "missing-run"})
	_, dormantErr := fixture.dormant.GetRun(context.Background(), serverapi.RunGetRequest{SessionID: fixture.sessionID, RunID: "missing-run"})
	if liveErr == nil || dormantErr == nil {
		t.Fatalf("expected missing run errors, got live=%v dormant=%v", liveErr, dormantErr)
	}
	assertEqual(t, "missing run error", liveErr.Error(), dormantErr.Error())
}

func TestSessionSnapshotSourcesParityForActiveRunStatus(t *testing.T) {
	store, engine, release, done := startBlockingRuntimeRun(t)
	live := NewService(NewStaticSessionResolver(store), NewStaticRuntimeResolver(engine), nil)
	dormant := NewService(NewStaticSessionResolver(store), nil, nil)

	liveMain := mustMainView(t, live, store.Meta().SessionID)
	dormantMain := mustMainView(t, dormant, store.Meta().SessionID)
	assertEqual(t, "active main run", normalizedRunView(liveMain.ActiveRun), normalizedRunView(dormantMain.ActiveRun))
	if liveMain.ActiveRun == nil {
		t.Fatal("expected active run")
	}
	liveRun := mustRun(t, live, store.Meta().SessionID, liveMain.ActiveRun.RunID)
	dormantRun := mustRun(t, dormant, store.Meta().SessionID, liveMain.ActiveRun.RunID)
	assertEqual(t, "active run lookup", normalizedRunView(liveRun), normalizedRunView(dormantRun))

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("submit user message: %v", err)
	}
}

func TestLiveRuntimeSnapshotReturnsActiveRunWithoutSessionStore(t *testing.T) {
	store, engine, release, done := startBlockingRuntimeRun(t)
	live := NewService(nil, NewStaticRuntimeResolver(engine), nil)
	liveMain := mustMainView(t, live, store.Meta().SessionID)
	if liveMain.ActiveRun == nil {
		t.Fatal("expected active run")
	}
	activeRun := mustRun(t, live, store.Meta().SessionID, liveMain.ActiveRun.RunID)
	assertEqual(t, "active run without store", normalizedRunView(activeRun), normalizedRunView(liveMain.ActiveRun))

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("submit user message: %v", err)
	}
}

type sessionSnapshotParityFixture struct {
	sessionID      string
	completedRunID string
	live           *Service
	dormant        *Service
}

func newSessionSnapshotParityFixture(t *testing.T, cacheWarningMode config.CacheWarningMode) sessionSnapshotParityFixture {
	t.Helper()
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetName("parity session"); err != nil {
		t.Fatalf("set name: %v", err)
	}
	if err := store.SetParentSessionID("parent-session"); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleUser, Content: "u1"}); err != nil {
		t.Fatalf("append u1: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: "a1", Phase: llm.MessagePhaseFinal}); err != nil {
		t.Fatalf("append a1: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "cache_warning", transcript.CacheWarning{Scope: transcript.CacheWarningScopeConversation, Reason: transcript.CacheWarningReasonNonPostfix}); err != nil {
		t.Fatalf("append cache warning: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "local_entry", map[string]any{"role": "compaction_summary", "text": "manual compacted summary"}); err != nil {
		t.Fatalf("append compaction summary: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleUser, Content: "u2"}); err != nil {
		t.Fatalf("append u2: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: "a2", Phase: llm.MessagePhaseFinal}); err != nil {
		t.Fatalf("append a2: %v", err)
	}
	startedAt := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(10 * time.Second)
	if _, err := store.AppendRunStarted(session.RunRecord{RunID: "run-completed", StepID: "step-1", StartedAt: startedAt}); err != nil {
		t.Fatalf("append run start: %v", err)
	}
	if _, err := store.AppendRunFinished(session.RunRecord{RunID: "run-completed", StepID: "step-1", Status: session.RunStatusCompleted, StartedAt: startedAt, FinishedAt: finishedAt}); err != nil {
		t.Fatalf("append run finish: %v", err)
	}

	engine, err := runtime.New(store, &serviceFakeLLM{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5", CacheWarningMode: cacheWarningMode})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	target := staticExecutionTargetResolver{target: clientui.SessionExecutionTarget{
		WorkspaceID:      "workspace-1",
		WorkspaceRoot:    dir,
		CwdRelpath:       ".",
		EffectiveWorkdir: dir,
	}}
	update := staticUpdateStatusProvider{status: clientui.UpdateStatus{Checked: true, Available: true, CurrentVersion: "1.0.0", LatestVersion: "1.1.0"}}
	live := NewService(NewStaticSessionResolver(store), NewStaticRuntimeResolver(engine), target).WithCacheWarningMode(cacheWarningMode).WithUpdateStatusProvider(update)
	dormant := NewService(NewStaticSessionResolver(store), nil, target).WithCacheWarningMode(cacheWarningMode).WithUpdateStatusProvider(update)
	return sessionSnapshotParityFixture{sessionID: store.Meta().SessionID, completedRunID: "run-completed", live: live, dormant: dormant}
}

func startBlockingRuntimeRun(t *testing.T) (*session.Store, *runtime.Engine, chan struct{}, chan error) {
	t.Helper()
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	client := &serviceFakeLLM{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "working", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{
				ID:    "call_shell_1",
				Name:  string(toolspec.ToolExecCommand),
				Input: []byte(`{"command":"pwd"}`),
			}},
			Usage: llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}
	engine, err := runtime.New(store, client, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: serviceBlockingTool{started: started, release: release}}), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, submitErr := engine.SubmitUserMessage(context.Background(), "run tools")
		done <- submitErr
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active run")
	}
	return store, engine, release, done
}

func mustMainView(t *testing.T, svc *Service, sessionID string) clientui.RuntimeMainView {
	t.Helper()
	resp, err := svc.GetSessionMainView(context.Background(), serverapi.SessionMainViewRequest{SessionID: sessionID})
	if err != nil {
		t.Fatalf("get main view: %v", err)
	}
	return resp.MainView
}

func mustTranscriptPage(t *testing.T, svc *Service, req serverapi.SessionTranscriptPageRequest) clientui.TranscriptPage {
	t.Helper()
	resp, err := svc.GetSessionTranscriptPage(context.Background(), req)
	if err != nil {
		t.Fatalf("get transcript page: %v", err)
	}
	return resp.Transcript
}

func mustCommittedSuffix(t *testing.T, svc *Service, req serverapi.SessionCommittedTranscriptSuffixRequest) clientui.CommittedTranscriptSuffix {
	t.Helper()
	resp, err := svc.GetSessionCommittedTranscriptSuffix(context.Background(), req)
	if err != nil {
		t.Fatalf("get committed suffix: %v", err)
	}
	return resp.Suffix
}

func mustRun(t *testing.T, svc *Service, sessionID, runID string) *clientui.RunView {
	t.Helper()
	resp, err := svc.GetRun(context.Background(), serverapi.RunGetRequest{SessionID: sessionID, RunID: runID})
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	return resp.Run
}

type comparableTranscriptPage struct {
	SessionID             string
	SessionName           string
	ConversationFreshness clientui.ConversationFreshness
	Revision              int64
	TotalEntries          int
	Offset                int
	NextOffset            int
	HasMore               bool
	Entries               []comparableChatEntry
}

func normalizedTranscriptPage(page clientui.TranscriptPage) comparableTranscriptPage {
	return comparableTranscriptPage{
		SessionID:             page.SessionID,
		SessionName:           page.SessionName,
		ConversationFreshness: page.ConversationFreshness,
		Revision:              page.Revision,
		TotalEntries:          page.TotalEntries,
		Offset:                page.Offset,
		NextOffset:            page.NextOffset,
		HasMore:               page.HasMore,
		Entries:               normalizedChatEntries(page.Entries),
	}
}

type comparableCommittedSuffix struct {
	SessionID             string
	SessionName           string
	ConversationFreshness clientui.ConversationFreshness
	Revision              int64
	CommittedEntryCount   int
	StartEntryCount       int
	NextEntryCount        int
	HasMore               bool
	Entries               []comparableChatEntry
}

func normalizedCommittedSuffix(suffix clientui.CommittedTranscriptSuffix) comparableCommittedSuffix {
	return comparableCommittedSuffix{
		SessionID:             suffix.SessionID,
		SessionName:           suffix.SessionName,
		ConversationFreshness: suffix.ConversationFreshness,
		Revision:              suffix.Revision,
		CommittedEntryCount:   suffix.CommittedEntryCount,
		StartEntryCount:       suffix.StartEntryCount,
		NextEntryCount:        suffix.NextEntryCount,
		HasMore:               suffix.HasMore,
		Entries:               normalizedChatEntries(suffix.Entries),
	}
}

type comparableChatEntry struct {
	Visibility   clientui.EntryVisibility
	Role         string
	Text         string
	OngoingText  string
	Phase        string
	MessageType  string
	CompactLabel string
}

func normalizedChatEntries(entries []clientui.ChatEntry) []comparableChatEntry {
	out := make([]comparableChatEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, comparableChatEntry{
			Visibility:   entry.Visibility,
			Role:         entry.Role,
			Text:         entry.Text,
			OngoingText:  entry.OngoingText,
			Phase:        entry.Phase,
			MessageType:  entry.MessageType,
			CompactLabel: entry.CompactLabel,
		})
	}
	return out
}

func normalizedRunView(run *clientui.RunView) *clientui.RunView {
	if run == nil {
		return nil
	}
	copyRun := *run
	return &copyRun
}

func assertEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s mismatch\nlive=%s\ndormant=%s", label, strings.TrimSpace(fmt.Sprintf("%+v", got)), strings.TrimSpace(fmt.Sprintf("%+v", want)))
	}
}
