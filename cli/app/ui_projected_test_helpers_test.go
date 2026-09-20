package app

import (
	"context"
	"core/internal/testharness/testsetup"
	"core/server/llm"
	"core/server/registry"
	"core/server/runtime"
	"core/server/runtimecontrol"
	"core/server/runtimeview"
	"core/server/runtimewire"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/sessionruntime"
	"core/server/sessionview"
	"core/server/tools"
	"core/shared/apicontract"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func transcriptTestMessage(sequence uint64, payload proto.Message) *transcriptpb.Message {
	event := &transcriptpb.Event{}
	switch payload := payload.(type) {
	case *transcriptpb.Hydration:
		event.Payload = &transcriptpb.Event_Hydration{Hydration: payload}
	case *transcriptpb.CommittedRow:
		event.Payload = &transcriptpb.Event_CommittedRow{CommittedRow: payload}
	case *transcriptpb.AssistantDelta:
		event.Payload = &transcriptpb.Event_AssistantDelta{AssistantDelta: payload}
	case *transcriptpb.AssistantStreamAbort:
		event.Payload = &transcriptpb.Event_AssistantStreamAbort{AssistantStreamAbort: payload}
	case *transcriptpb.ThinkingStatusUpdate:
		event.Payload = &transcriptpb.Event_ThinkingStatusUpdate{ThinkingStatusUpdate: payload}
	case *transcriptpb.ReasoningTraceUpdate:
		event.Payload = &transcriptpb.Event_ReasoningTraceUpdate{ReasoningTraceUpdate: payload}
	case *transcriptpb.ReasoningTraceReset:
		event.Payload = &transcriptpb.Event_ReasoningTraceReset{ReasoningTraceReset: payload}
	case *transcriptpb.ToolStart:
		event.Payload = &transcriptpb.Event_ToolStart{ToolStart: payload}
	case *transcriptpb.ToolAbort:
		event.Payload = &transcriptpb.Event_ToolAbort{ToolAbort: payload}
	case *transcriptpb.UserMessageFlushed:
		event.Payload = &transcriptpb.Event_UserMessageFlushed{UserMessageFlushed: payload}
	case *transcriptpb.QueuedMessageState:
		event.Payload = &transcriptpb.Event_QueuedMessageState{QueuedMessageState: payload}
	case *transcriptpb.StepState:
		event.Payload = &transcriptpb.Event_StepState{StepState: payload}
	case *runtimepb.ReadModelUpdate:
		event.Payload = &transcriptpb.Event_RuntimeReadModelUpdate{RuntimeReadModelUpdate: payload}
	case *transcriptpb.SessionStatus:
		event.Payload = &transcriptpb.Event_SessionStatus{SessionStatus: payload}
	case *transcriptpb.SessionIdentity:
		event.Payload = &transcriptpb.Event_SessionIdentity{SessionIdentity: payload}
	case *transcriptpb.CompactionStatus:
		event.Payload = &transcriptpb.Event_CompactionStatus{CompactionStatus: payload}
	case *runtimepb.ContextUsage:
		event.Payload = &transcriptpb.Event_ContextUsage{ContextUsage: payload}
	case *runtimepb.GoalView:
		event.Payload = &transcriptpb.Event_GoalStatus{GoalStatus: payload}
	case *transcriptpb.BackgroundActivity:
		event.Payload = &transcriptpb.Event_BackgroundActivity{BackgroundActivity: payload}
	case *transcriptpb.Prompt:
		event.Payload = &transcriptpb.Event_Prompt{Prompt: payload}
	case *transcriptpb.WorktreeTransitionOutcome:
		event.Payload = &transcriptpb.Event_WorktreeTransitionOutcome{WorktreeTransitionOutcome: payload}
	case *transcriptpb.OperationalDiagnostic:
		event.Payload = &transcriptpb.Event_OperationalDiagnostic{OperationalDiagnostic: payload}
	case *transcriptpb.LiveRunFinished:
		event.Payload = &transcriptpb.Event_LiveRunFinished{LiveRunFinished: payload}
	case *transcriptpb.PendingWorkChanged:
		event.Payload = &transcriptpb.Event_PendingWorkChanged{PendingWorkChanged: payload}
	case *transcriptpb.PendingWorkRestored:
		event.Payload = &transcriptpb.Event_PendingWorkRestored{PendingWorkRestored: payload}
	case *transcriptpb.SessionSettingFeedback:
		event.Payload = &transcriptpb.Event_SessionSettingFeedback{SessionSettingFeedback: payload}
	case *transcriptpb.HumanInputInterrupted:
		event.Payload = &transcriptpb.Event_HumanInputInterrupted{HumanInputInterrupted: payload}
	default:
		panic("unsupported transcript test payload")
	}
	return &transcriptpb.Message{Sequence: sequence, Event: event}
}

func waitForTestCondition(t *testing.T, timeout time.Duration, label string, condition func() bool) {
	t.Helper()
	testsetup.RequireUntil(t, time.Now().Add(timeout), 10*time.Millisecond, condition, "timed out waiting for %s", label)
}

func newProjectedTestUIModel(runtimeClient clientui.RuntimeClient, opts ...UIOption) *uiModel {
	return NewProjectedUIModel(runtimeClient, opts...).(*uiModel)
}

func newProjectedClosedUIModel(runtimeClient clientui.RuntimeClient, opts ...UIOption) *uiModel {
	return newProjectedTestUIModel(runtimeClient, opts...)
}

func newSizedProjectedClosedUIModel(runtimeClient clientui.RuntimeClient, width, height int, opts ...UIOption) *uiModel {
	return sizedTestUIModel(newProjectedClosedUIModel(runtimeClient, opts...), width, height)
}

func setTestUITerminalSize(m *uiModel, width, height int) *uiModel {
	m.terminalGeometry = terminalGeometryKnown(width, height)
	return m
}

func sizedTestUIModel(m *uiModel, width, height int) *uiModel {
	m = setTestUITerminalSize(m, width, height)
	return m
}

func newProjectedStaticUIModel(opts ...UIOption) *uiModel {
	return newProjectedTestUIModel(nil, opts...)
}

type recordingPromptControl struct {
	singlePromptOnlyControl
	batchRequests chan *promptpb.AnswerBatchRequest
}

type singlePromptOnlyControl struct{}

func (singlePromptOnlyControl) AnswerPromptBatch(context.Context, *promptpb.AnswerBatchRequest) (*promptpb.AnswerBatchSuccess, error) {
	panic("unexpected prompt batch answer")
}

func newRecordingPromptControl() *recordingPromptControl {
	return &recordingPromptControl{batchRequests: make(chan *promptpb.AnswerBatchRequest, 8)}
}

func (c *recordingPromptControl) AnswerPromptBatch(_ context.Context, request *promptpb.AnswerBatchRequest) (*promptpb.AnswerBatchSuccess, error) {
	c.batchRequests <- request
	return resolvedPromptBatchResponse(request), nil
}

func resolvedPromptBatchResponse(request *promptpb.AnswerBatchRequest) *promptpb.AnswerBatchSuccess {
	results := make([]*promptpb.AnswerBatchEntryResult, 0, len(request.Entries))
	for _, entry := range request.Entries {
		results = append(results, &promptpb.AnswerBatchEntryResult{ToolCallId: entry.ToolCallId, Outcome: promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_RESOLVED})
	}
	return &promptpb.AnswerBatchSuccess{Results: results}
}

func newProjectedPromptTestUIModel(t *testing.T, opts ...UIOption) (*uiModel, *recordingPromptControl) {
	t.Helper()
	control := newRecordingPromptControl()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	model := newProjectedStaticUIModel(opts...)
	model.promptAnswers = newTranscriptPromptAnswerer(ctx, control)
	return model, control
}

func runPromptDeliveryCommand(t *testing.T, model *uiModel, command tea.Cmd) *uiModel {
	t.Helper()
	if command == nil {
		t.Fatal("expected prompt delivery command")
	}
	return updateUIModel(t, model, command())
}

func submitAskPromptKey(t *testing.T, model *uiModel, control *recordingPromptControl, key tea.KeyMsg) (*uiModel, *promptpb.AnswerBatchRequest) {
	t.Helper()
	next, command := model.Update(key)
	updated := runPromptDeliveryCommand(t, next.(*uiModel), command)
	return updated, requirePromptAnswerBatchRequest(t, control)
}

func requirePromptAnswerBatchRequest(t *testing.T, control *recordingPromptControl) *promptpb.AnswerBatchRequest {
	t.Helper()
	select {
	case request := <-control.batchRequests:
		return request
	default:
		t.Fatal("completed prompt delivery recorded no prompt answer batch request")
		return &promptpb.AnswerBatchRequest{}
	}
}

func requireQuestionAnswerEntry(t *testing.T, request *promptpb.AnswerBatchRequest) *promptpb.AnswerBatchEntry {
	t.Helper()
	if len(request.Entries) != 1 || request.Entries[0].GetQuestionAnswer() == nil {
		t.Fatalf("prompt answer batch = %+v, want one Question answer", request)
	}
	return request.Entries[0]
}

func requireApprovalAnswerEntry(t *testing.T, request *promptpb.AnswerBatchRequest) *promptpb.AnswerBatchEntry {
	t.Helper()
	if len(request.Entries) != 1 || request.Entries[0].GetApprovalAnswer() == nil {
		t.Fatalf("prompt answer batch = %+v, want one Approval answer", request)
	}
	return request.Entries[0]
}

type projectedAuthorityRuntime struct {
	client    clientui.RuntimeClient
	reads     *sessionview.Service
	sessionID string
}

func newProjectedAuthorityUIModel(t *testing.T, client llm.Client, cfg runtime.Config, opts ...UIOption) *uiModel {
	t.Helper()
	store, persistence := createAuthoritativeTestSession(t, t.TempDir(), "ws", t.TempDir())
	fixture := newProjectedAuthorityRuntime(t, store, persistence, client, cfg)
	return newProjectedTestUIModel(fixture.client, opts...)
}

func newProjectedAuthorityRuntime(
	t *testing.T,
	store *session.Store,
	persistence *sessiontest.Persistence,
	client llm.Client,
	cfg runtime.Config,
) projectedAuthorityRuntime {
	t.Helper()
	if store == nil || persistence == nil {
		t.Fatal("projected Authority runtime requires a durable session fixture")
	}
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("parse session id: %v", err)
	}
	settings := config.DefaultOnboardingSettings()
	settings.ProviderOverride = "openai"
	settings.Reviewer.Frequency = "off"
	if cfg.Model == "" {
		settings.Model = "gpt-5"
	} else {
		settings.Model = cfg.Model
	}
	if cfg.ContextWindowTokens > 0 {
		settings.ModelContextWindow = cfg.ContextWindowTokens
	}
	plan, err := sessionruntime.NewAgentRuntimePlan(sessionruntime.AgentRuntimePlanOptions{
		MainWorkspaceRoot:     store.Meta().WorkspaceRoot,
		Settings:              settings,
		QuestionsEnabled:      textutil.Value(true),
		AutoCompactionEnabled: textutil.Value(true),
		FilesystemContext: func() tools.FilesystemContext {
			context, err := runtimewire.NewFilesystemContext(store.Meta().WorkspaceRoot, store.Meta().WorkspaceRoot, "test")
			if err != nil {
				t.Fatalf("NewFilesystemContext: %v", err)
			}
			return context
		}(),
		Client: client,
	})
	if err != nil {
		t.Fatalf("new projected runtime plan: %v", err)
	}
	activity := registry.NewRuntimeRegistry()
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot:   t.TempDir(),
		StoreOptions:      persistence.Options(),
		ResourceLifecycle: activity,
		EventFeed: func(resource runtimeids.SessionResourceRef, event runtime.Event) {
			activity.PublishAuthorityRuntimeEvent(resource, event)
		},
	})
	if _, err := authority.OpenRuntime(t.Context(), sessionruntime.RuntimeOpenRequest{
		SessionID: sessionID,
		OwnerID:   "projected-ui-test",
		Runtime:   &plan,
	}); err != nil {
		t.Fatalf("open projected runtime: %v", err)
	}
	t.Cleanup(func() {
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close projected runtime: %v", err)
		}
	})
	reads := sessionview.NewService(persistence, activity, nil)
	controls := runtimecontrol.NewService(authority).WithRuntimeActivityResolver(activity)
	runtimeClient := newUIRuntimeClientWithReads(sessionID.String(), reads, controls, nil, nil).(*sessionRuntimeClient)
	snapshot, err := activity.RuntimeReadModelFeedSnapshot(context.Background(), sessionID.String())
	if err != nil {
		t.Fatalf("projected runtime snapshot: %v", err)
	}
	err = authority.WithCurrentRuntime(t.Context(), sessionID, func(_ context.Context, engine *runtime.Engine) error {
		view, err := runtimeview.MainViewFromRuntimeActivity(
			engine,
			snapshot.Version,
			snapshot.Activity,
		)
		if err != nil {
			return err
		}
		runtimeClient.storeMainView(view)
		return nil
	})
	if err != nil {
		t.Fatalf("read projected runtime: %v", err)
	}
	return projectedAuthorityRuntime{
		client:    runtimeClient,
		reads:     reads,
		sessionID: sessionID.String(),
	}
}

func newUnavailableRuntimeControlService() *runtimecontrol.Service {
	activity := registry.NewRuntimeRegistry()
	return runtimecontrol.NewService(sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})).
		WithRuntimeActivityResolver(activity)
}

func newTestSessionRuntimeClient(reads apicontract.SessionViewService, controls apicontract.RuntimeControlService) *sessionRuntimeClient {
	return newUIRuntimeClientWithReads("session-1", reads, controls, nil, nil).(*sessionRuntimeClient)
}

func newTestSessionRuntimeClientWithControls(controls apicontract.RuntimeControlService) *sessionRuntimeClient {
	return newTestSessionRuntimeClient(&countingSessionViewClient{}, controls)
}

func testQuestionPrompt(id, question string, suggestions ...string) *transcriptpb.Prompt {
	return &transcriptpb.Prompt{
		Status: transcriptpb.PromptStatus_PROMPT_STATUS_PENDING,
		Prompt: &transcriptpb.Prompt_Question{Question: &promptpb.Question{
			ToolCallId:  id,
			SessionId:   ongoingTestSessionID().String(),
			StepId:      ongoingTestStepID().String(),
			Question:    question,
			CreatedAt:   timestamppb.New(time.Unix(1, 0).UTC()),
			Suggestions: append([]string(nil), suggestions...),
		}},
	}
}

func testApprovalPrompt(id, question string, decisions ...clientui.ApprovalDecision) *transcriptpb.Prompt {
	options := make([]*promptpb.ApprovalOption, 0, len(decisions))
	for _, decision := range decisions {
		generated, err := protoapi.ApprovalDecisionToProto(decision)
		if err != nil {
			panic(err)
		}
		options = append(options, &promptpb.ApprovalOption{Decision: generated})
	}
	return &transcriptpb.Prompt{
		Status: transcriptpb.PromptStatus_PROMPT_STATUS_PENDING,
		Prompt: &transcriptpb.Prompt_Approval{Approval: &promptpb.Approval{
			ToolCallId: id,
			SessionId:  ongoingTestSessionID().String(),
			StepId:     ongoingTestStepID().String(),
			Question:   &question,
			CreatedAt:  timestamppb.New(time.Unix(1, 0).UTC()),
			Options:    options,
		}},
	}
}

func testQuestionAskEvent(id, question string, suggestions ...string) askEvent {
	return askEvent{prompt: testQuestionPrompt(id, question, suggestions...)}
}

func testQuestionAskEventPtr(id, question string, suggestions ...string) *askEvent {
	event := testQuestionAskEvent(id, question, suggestions...)
	return &event
}

func testApprovalAskEvent(id, question string, decisions ...clientui.ApprovalDecision) askEvent {
	return askEvent{prompt: testApprovalPrompt(id, question, decisions...)}
}
