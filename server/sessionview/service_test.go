package sessionview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"core/server/llm"
	"core/server/session"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/textutil"
)

type serviceFakeLLM struct {
	responses []llm.Response
}

func (f *serviceFakeLLM) Generate(context.Context, llm.Request, llm.StreamCallbacks) (llm.Response, error) {
	if len(f.responses) == 0 {
		return llm.Response{}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *serviceFakeLLM) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true}, nil
}

type serviceBlockingLLM struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type staticExecutionTargetResolver struct {
	target *worktreepb.SessionExecutionTarget
}

func newDormantMainViewTestService(
	t *testing.T,
	store *session.Store,
	target *worktreepb.SessionExecutionTarget,
) *Service {
	t.Helper()
	settings := config.DefaultOnboardingSettings()
	return NewService(
		newTestSessionResolver(store),
		nil,
		staticExecutionTargetResolver{target: target},
	).WithChatContextWorkspaceResolver(&sessionChatContextWorkspaceResolver{
		app: config.App{Settings: settings},
	})
}

func TestValidateSessionTranscriptPageResponseUsesInvariantFailurePolicy(t *testing.T) {
	response := &transcriptpb.PageSuccess{
		Transcript: &transcriptpb.Page{
			SessionId:             "12345678-1234-4234-8234-123456789012",
			ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
			Entries: []*transcriptpb.CommittedRow{{
				Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
				Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
				Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 0, RowOrdinal: 1},
				Row:        &transcriptpb.CommittedRow_User{User: &transcriptpb.UserRow{Text: "malformed"}},
			}},
		},
	}

	t.Run("diagnostic mode returns contract error", func(t *testing.T) {
		t.Setenv("KENT_INVARIANT_MODE", "diagnostic")
		if err := validateSessionTranscriptPageResponse(response); err == nil {
			t.Fatal("malformed transcript page returned nil error")
		}
	})
	t.Run("panic mode fails fast with diagnostic", func(t *testing.T) {
		t.Setenv("KENT_INVARIANT_MODE", "panic")
		defer func() {
			if recover() == nil {
				t.Fatal("malformed transcript page did not panic in invariant panic mode")
			}
		}()
		_ = validateSessionTranscriptPageResponse(response)
	})
}

func (r staticExecutionTargetResolver) ResolveSessionExecutionTarget(context.Context, string) (*worktreepb.SessionExecutionTarget, error) {
	return r.target, nil
}

type failingPersistedSessionResolver struct {
	err error
}

func (r failingPersistedSessionResolver) ResolvePersistedSession(context.Context, string) (session.PersistedSessionRecord, error) {
	return session.PersistedSessionRecord{}, r.err
}

func (c *serviceBlockingLLM) Generate(ctx context.Context, _ llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	c.once.Do(func() { close(c.started) })
	select {
	case <-ctx.Done():
		return llm.Response{}, ctx.Err()
	case <-c.release:
		content := "done"
		phase := llm.MessagePhaseFinal
		return llm.Response{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: &content, Phase: &phase},
			Usage:     llm.Usage{WindowTokens: 200000},
		}, nil
	}
}

func (*serviceBlockingLLM) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true}, nil
}

func TestServiceGetSessionMainViewReturnsCompletedRuntimeProjectionWhileRunIsBlocked(t *testing.T) {
	dir := t.TempDir()
	store := newSessionViewStore(t, dir, "ws", dir)
	started := make(chan struct{})
	release := make(chan struct{})
	client := &serviceBlockingLLM{started: started, release: release}
	fixture := newSessionViewRuntimeFixture(t, store, client)
	svc := NewService(newTestSessionResolver(store), fixture.activity, nil)
	handle := fixture.startUserTurn(t, "run tools")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active run")
	}

	resp, err := svc.GetSessionMainView(context.Background(), &sessionpb.MainViewRequest{SessionId: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("get session main view: %v", err)
	}
	if resp.MainView.Activity.State == runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE {
		t.Fatalf("expected completed Runtime Main View, got %+v", resp.MainView.Activity)
	}
	close(release)
	if _, err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("submit user message: %v", err)
	}
}

func TestServiceGetSessionMainViewDoesNotRequireStoreResolutionForLiveRuntime(t *testing.T) {
	store := newSessionViewStore(t, t.TempDir(), "ws", t.TempDir())
	fixture := newSessionViewRuntimeFixture(t, store, &serviceFakeLLM{})
	response, err := NewService(failingPersistedSessionResolver{err: errors.New("store unavailable")}, fixture.activity, nil).
		GetSessionMainView(t.Context(), &sessionpb.MainViewRequest{SessionId: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("get live main view: %v", err)
	}
	if response.MainView.Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE {
		t.Fatalf("live activity = %+v, want registered idle", response.MainView.Activity)
	}
}

func TestServiceGetSessionMainViewFallsBackToDurableSessionState(t *testing.T) {
	dir := t.TempDir()
	store, parentSessionID := newSessionViewParentAgentChild(t, dir, "ws", dir)
	if err := store.SetName("incident triage"); err != nil {
		t.Fatalf("set name: %v", err)
	}
	role := "worker"
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: &role}); err != nil {
		t.Fatalf("set continuation context: %v", err)
	}
	if _, _, err := store.SetGoal("ship dormant goal", session.GoalActorUser); err != nil {
		t.Fatalf("set goal: %v", err)
	}
	appendSessionViewMessage(t, store, "step-1", session.MessageRoleUser, "hello", nil, nil)
	appendSessionViewMessage(t, store, "step-1", session.MessageRoleAssistant, "final answer", sessionViewMessagePhasePointer(session.MessagePhaseFinal), nil)
	svc := newDormantMainViewTestService(t, store, availableSessionExecutionTarget(dir))
	resp, err := svc.GetSessionMainView(context.Background(), &sessionpb.MainViewRequest{SessionId: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("get session main view: %v", err)
	}
	if err := protoapi.Validate(resp.MainView.Activity); err != nil {
		t.Fatalf("validate dormant session activity: %v", err)
	}
	if resp.MainView.Session.SessionId != store.Meta().SessionID || resp.MainView.Session.GetSessionName() != "incident triage" {
		t.Fatalf("unexpected dormant session view: %+v", resp.MainView.Session)
	}
	if resp.MainView.Session.AgentRole == nil || *resp.MainView.Session.AgentRole != role {
		t.Fatalf("dormant session agent role = %v, want %q", resp.MainView.Session.AgentRole, role)
	}
	if resp.MainView.Status.ParentAgentSessionId == nil || *resp.MainView.Status.ParentAgentSessionId != parentSessionID ||
		resp.MainView.Status.NavigationTargetSessionId == nil || *resp.MainView.Status.NavigationTargetSessionId != parentSessionID ||
		resp.MainView.Status.LastCommittedAssistantFinalAnswer == nil ||
		*resp.MainView.Status.LastCommittedAssistantFinalAnswer != "final answer" {
		t.Fatalf("unexpected dormant status: %+v", resp.MainView.Status)
	}
	if resp.MainView.Status.Goal == nil ||
		resp.MainView.Status.Goal.Goal == nil ||
		resp.MainView.Status.Goal.Goal.Status != runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE ||
		resp.MainView.Status.Goal.Goal.Objective != "ship dormant goal" {
		t.Fatalf("unexpected dormant goal status: %+v", resp.MainView.Status.Goal)
	}
	if resp.MainView.Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE ||
		resp.MainView.Activity.Reviewer != runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE {
		t.Fatalf("dormant activity = %+v, want unavailable", resp.MainView.Activity)
	}
}

func TestServiceGetSessionMainViewProjectsCompleteDormantStateWithoutGoal(t *testing.T) {
	workspaceRoot := t.TempDir()
	executionRoot := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workspaceRoot)
	if _, err := store.SetUsageState(&session.UsageState{InputTokens: 25_000}); err != nil {
		t.Fatalf("SetUsageState: %v", err)
	}
	if err := store.SetSessionContextFacts(2, true); err != nil {
		t.Fatalf("SetSessionContextFacts: %v", err)
	}
	settings := config.DefaultOnboardingSettings()
	settings.Reviewer.Frequency = "edits"
	settings.ThinkingLevel = "high"
	settings.ModelContextWindow = 100_000
	settings.ContextCompactionThresholdTokens = 75_000
	settings.CompactionMode = config.CompactionModeLocal
	target := availableSessionExecutionTarget(executionRoot)
	service := NewService(
		newTestSessionResolver(store),
		nil,
		staticExecutionTargetResolver{target: target},
	).WithChatContextWorkspaceResolver(&sessionChatContextWorkspaceResolver{
		app: config.App{Settings: settings},
	})

	response, err := service.GetSessionMainView(t.Context(), &sessionpb.MainViewRequest{
		SessionId: store.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("GetSessionMainView: %v", err)
	}
	if err := protoapi.Validate(response); err != nil {
		t.Fatalf("dormant Main View contract: %v", err)
	}
	status := response.MainView.Status
	if status.ReviewerFrequency != "edits" ||
		status.ThinkingLevel != "high" ||
		status.CompactionMode != string(config.CompactionModeLocal) {
		t.Fatalf("dormant status settings = %+v", status)
	}
	if status.ContextUsage.UsedTokens != 25_000 ||
		status.ContextUsage.WindowTokens != 100_000 ||
		status.CompactionCount != 2 {
		t.Fatalf("dormant Context status = %+v", status)
	}
	if status.Goal == nil || status.Goal.Goal != nil ||
		status.Goal.Availability == nil ||
		*status.Goal.Availability != runtimepb.GoalAvailability_GOAL_AVAILABILITY_AVAILABLE {
		t.Fatalf("dormant no-Goal projection = %+v", status.Goal)
	}
	if response.MainView.Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE {
		t.Fatalf("dormant activity = %+v, want unavailable", response.MainView.Activity)
	}
	if response.MainView.Session.ExecutionTarget != target {
		t.Fatalf("execution target = %+v, want %+v", response.MainView.Session.ExecutionTarget, target)
	}
}

func TestServiceGetSessionMainViewIncludesExecutionTarget(t *testing.T) {
	dir := t.TempDir()
	store := newSessionViewStore(t, dir, "ws", dir)
	target := &worktreepb.SessionExecutionTarget{
		WorkspaceId:           textutil.Value("workspace-1"),
		WorkspaceName:         "workspace",
		WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		WorkspaceRoot:         dir,
		CwdRelpath:            ".",
		EffectiveWorkdir:      dir,
	}
	svc := newDormantMainViewTestService(t, store, target)

	resp, err := svc.GetSessionMainView(context.Background(), &sessionpb.MainViewRequest{SessionId: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("get session main view: %v", err)
	}
	if resp.MainView.Session.ExecutionTarget.GetWorkspaceId() != "workspace-1" {
		t.Fatalf("workspace id = %q, want workspace-1", resp.MainView.Session.ExecutionTarget.GetWorkspaceId())
	}
	if resp.MainView.Session.ExecutionTarget.EffectiveWorkdir != dir {
		t.Fatalf("effective workdir = %q, want %q", resp.MainView.Session.ExecutionTarget.EffectiveWorkdir, dir)
	}
}

func TestServiceRequiresSessionStoreResolverForDormantReads(t *testing.T) {
	svc := NewService(nil, nil, nil)

	if _, err := svc.GetSessionMainView(context.Background(), &sessionpb.MainViewRequest{SessionId: "session-1"}); err == nil || !errors.Is(err, session.ErrPersistedSessionResolverRequired) {
		t.Fatalf("expected explicit persisted Session resolver error for main view, got %v", err)
	}
	if _, err := svc.SessionTranscriptTailEntries(context.Background(), "session-1"); err == nil || !errors.Is(err, session.ErrPersistedSessionResolverRequired) {
		t.Fatalf("expected explicit persisted Session resolver error for transcript tail entries, got %v", err)
	}
}

func TestServiceWithCacheWarningModeChangesSubsequentDormantReads(t *testing.T) {
	dir := t.TempDir()
	store := newSessionViewStore(t, dir, "ws", dir)
	appendSessionViewRecord(t, store, "step-1", session.CacheWarningRecord{
		Scope:  session.CacheScopeConversation,
		Reason: session.CacheWarningReasonNonPostfix,
	})
	svc := newDormantMainViewTestService(t, store, availableSessionExecutionTarget(dir))

	first, err := svc.SessionTranscriptTailEntries(context.Background(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("get dormant transcript tail entries default: %v", err)
	}
	if got := first[0].Visibility; got != clientui.EntryVisibilityDetail {
		t.Fatalf("default cache warning visibility = %q, want %q", got, clientui.EntryVisibilityDetail)
	}

	svc.WithCacheWarningMode(config.CacheWarningModeVerbose)
	second, err := svc.SessionTranscriptTailEntries(context.Background(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("get dormant transcript tail entries verbose: %v", err)
	}
	if got := second[0].Visibility; got != clientui.EntryVisibilityOngoing {
		t.Fatalf("verbose cache warning visibility = %q, want %q", got, clientui.EntryVisibilityOngoing)
	}
}

func TestServiceSessionTranscriptTailEntriesObservesRevisionAdvance(t *testing.T) {
	dir := t.TempDir()
	store := newSessionViewStore(t, dir, "ws", dir)
	appendSessionViewMessage(t, store, "11111111-1111-4111-8111-111111111111", session.MessageRoleAssistant, "line 0", sessionViewMessagePhasePointer(session.MessagePhaseFinal), nil)
	svc := newDormantMainViewTestService(t, store, availableSessionExecutionTarget(dir))

	first, err := svc.SessionTranscriptTailEntries(context.Background(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("get first transcript tail entries: %v", err)
	}
	if got := len(first); got != 1 {
		t.Fatalf("first entry count = %d, want 1", got)
	}

	appendSessionViewMessage(t, store, "22222222-2222-4222-8222-222222222222", session.MessageRoleAssistant, "line 1", sessionViewMessagePhasePointer(session.MessagePhaseFinal), nil)
	second, err := svc.SessionTranscriptTailEntries(context.Background(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("get second transcript tail entries: %v", err)
	}
	if got := len(second); got != 2 {
		t.Fatalf("updated entry count = %d, want 2", got)
	}
}

func TestServiceDormantHistoryReplacementStartsNewTranscriptSegment(t *testing.T) {
	dir := t.TempDir()
	store := newSessionViewStore(t, dir, "ws", dir)
	appendSessionViewMessage(t, store, "step-1", session.MessageRoleUser, "u1", nil, nil)
	appendSessionViewMessage(t, store, "step-1", session.MessageRoleAssistant, "rolled back final", sessionViewMessagePhasePointer(session.MessagePhaseFinal), nil)
	appendSessionViewMessage(t, store, "step-1", session.MessageRoleUser, "u2", nil, nil)
	appendSessionViewHistoryReplacement(t, store, "step-1", session.HistoryReplacementRecord{
		Engine: "local",
		Mode:   session.CompactionModeAuto,
	})

	svc := newDormantMainViewTestService(t, store, availableSessionExecutionTarget(dir))

	entries, err := svc.SessionTranscriptTailEntries(context.Background(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("get session transcript tail entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entry count = %d, want empty active segment", len(entries))
	}

	mainViewResp, err := svc.GetSessionMainView(context.Background(), &sessionpb.MainViewRequest{SessionId: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("get session main view: %v", err)
	}
	if got := mainViewResp.MainView.Status.LastCommittedAssistantFinalAnswer; got != nil {
		t.Fatalf("last committed assistant final answer = %q, want absence because later user message supersedes it", *got)
	}
}

func TestServiceSessionTranscriptTailEntriesKeepsDormantPersistedCompactionSummaryAndCarryover(t *testing.T) {
	dir := t.TempDir()
	store := newSessionViewStore(t, dir, "ws", dir)
	appendSessionViewMessage(t, store, "step-1", session.MessageRoleUser, "before compaction", nil, nil)
	appendSessionViewHistoryReplacement(t, store, "step-1", session.HistoryReplacementRecord{
		Engine: "local",
		Mode:   session.CompactionModeManual,
	})
	appendSessionViewRecord(t, store, "step-1", session.LocalEntryRecord{
		Visibility: session.EntryVisibilityAuto,
		Role:       "compaction_summary",
		Text:       sessionViewStringPointer("condensed summary"),
	})
	appendSessionViewMessage(t, store, "step-1", session.MessageRoleDeveloper, "Last user message before handoff\n\ncarry this forward", nil, sessionViewMessageTypePointer(session.MessageTypeCompactionPreservedUserMessage))
	svc := newDormantMainViewTestService(t, store, availableSessionExecutionTarget(dir))

	entries, err := svc.SessionTranscriptTailEntries(context.Background(), store.Meta().SessionID)
	if err != nil {
		t.Fatalf("get session transcript tail entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (%+v)", len(entries), entries)
	}
	if entries[0].Role != "compaction_summary" || entries[0].Text != "condensed summary" {
		t.Fatalf("expected persisted compaction summary entry, got %+v", entries[0])
	}
	if entries[1].Role != "manual_compaction_carryover" {
		t.Fatalf("expected legacy compaction-preserved user entry, got %+v", entries[1])
	}
}

func TestServiceDormantReadsDoNotMutatePersistedEvents(t *testing.T) {
	dir := t.TempDir()
	store := newSessionViewStore(t, dir, "ws", dir)
	const stepID = "11111111-1111-4111-8111-111111111111"
	appendSessionViewMessage(t, store, stepID, session.MessageRoleUser, "hello", nil, nil)

	eventsPath := filepath.Join(store.Dir(), "events.jsonl")
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	beforeSequence, err := eventLog.Revision()
	if err != nil {
		t.Fatalf("read event-log revision before: %v", err)
	}
	beforeEvents, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events file before: %v", err)
	}

	svc := newDormantMainViewTestService(t, store, availableSessionExecutionTarget(dir))
	resp, err := svc.GetSessionMainView(context.Background(), &sessionpb.MainViewRequest{SessionId: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("get session main view: %v", err)
	}
	if resp.MainView.Activity.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE {
		t.Fatalf("dormant activity = %+v, want unavailable", resp.MainView.Activity)
	}
	if _, err := svc.GetSessionTranscriptPage(t.Context(), &transcriptpb.PageRequest{SessionId: store.Meta().SessionID}); err != nil {
		t.Fatalf("get session transcript page: %v", err)
	}
	if _, err := svc.SessionTranscriptTailEntries(t.Context(), store.Meta().SessionID); err != nil {
		t.Fatalf("get session transcript tail entries: %v", err)
	}

	afterEvents, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events file after: %v", err)
	}
	if string(beforeEvents) != string(afterEvents) {
		t.Fatalf("events file mutated during read\nbefore=%s\nafter=%s", string(beforeEvents), string(afterEvents))
	}
	afterSequence, err := eventLog.Revision()
	if err != nil {
		t.Fatalf("read event-log revision after: %v", err)
	}
	if afterSequence != beforeSequence {
		t.Fatalf("event-log revision mutated during read: got %d want %d", afterSequence, beforeSequence)
	}
}
