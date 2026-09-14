package runtimecontrol

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"core/server/metadata"
	"core/server/promptcontrol"
	"core/server/runtime"
	"core/server/runtimeactivity"
	"core/server/session"
	"core/server/sessionruntime"
	"core/server/workflowexecution"
	servicecontract "core/shared/apicontract"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
	"core/shared/transcript"
)

type RuntimeActivityResolver interface {
	RuntimeReadModelFeedSnapshot(ctx context.Context, sessionID string) (*runtimepb.ReadModelUpdate, error)
}

type sessionSettingPublisher interface {
	PublishSessionSettingFeedback(sessionID string, feedback *transcriptpb.SessionSettingFeedback) error
}

type PromptHistoryStore interface {
	RecordPromptHistoryEntry(ctx context.Context, entry metadata.PromptHistoryEntry) (metadata.PromptHistoryRecord, error)
}

type PromptCommandResolver interface {
	ResolvePromptCommand(ctx context.Context, sessionID, name, arguments string) (string, error)
}

type WorkflowTaskSessionResolver interface {
	SessionHasWorkflowTask(ctx context.Context, sessionID string) (bool, error)
}

type WorkflowSessionReactivator interface {
	ReactivateWorkflowSession(
		context.Context,
		runtimeids.SessionID,
	) (sessionruntime.ExecutionHandle, error)
}

type WorkflowSessionPreparationReader interface {
	WorkflowSessionPreparing(context.Context, runtimeids.SessionID) (bool, error)
}

var errWorkflowTaskSessionAutoCompactionDisable = errors.New("auto-compaction cannot be disabled for workflow task sessions")

type Service struct {
	authority      *sessionruntime.Authority
	activity       RuntimeActivityResolver
	promptStore    PromptHistoryStore
	promptCommands PromptCommandResolver
	workflowTasks  WorkflowTaskSessionResolver
	reactivator    WorkflowSessionReactivator
	preparations   WorkflowSessionPreparationReader
	continuation   workflowexecution.WorkflowSessionContinuationValidator
	persisted      session.PersistedSessionResolver
	pendingPrompts promptcontrol.PendingPromptSource
	attention      servicecontract.AttentionNotificationService
}

type sessionUserTurnRequest struct {
	SessionID string
	Kind      runtimeinput.Kind
	Text      string
	Name      string
	Arguments string
}

type goalSetRequest struct {
	SessionID string
	Objective string
	Actor     string
	RunID     string
	StepID    string
}

type goalStatusRequest struct {
	SessionID string
	Status    string
	Actor     string
	RunID     string
	StepID    string
}

type goalClearRequest struct {
	SessionID string
	Actor     string
}

func NewService(authority *sessionruntime.Authority) *Service {
	return &Service{authority: authority}
}

func (s *Service) runAgentExecution(
	ctx context.Context,
	sessionID string,
	run func(context.Context, *runtime.Engine) error,
) error {
	if s == nil || s.authority == nil {
		return errors.New("session runtime authority is required")
	}
	id, err := runtimeids.ParseSessionID(strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	descriptor, err := session.NewOpenSessionDescriptor(id)
	if err != nil {
		return err
	}
	err = s.authority.RunCurrentAgentExecution(ctx, descriptor, run)
	if err == nil {
		return nil
	}
	if errors.Is(err, sessionruntime.ErrSessionRunActive) ||
		errors.Is(err, sessionruntime.ErrSessionWorkflowActivationActive) {
		return errors.Join(serverapi.ErrSessionRunStarting, err)
	}
	return err
}

func (s *Service) WithRuntimeActivityResolver(resolver RuntimeActivityResolver) *Service {
	if s == nil {
		return nil
	}
	s.activity = resolver
	return s
}

func (s *Service) WithPromptHistoryStore(store PromptHistoryStore) *Service {
	if s == nil {
		return nil
	}
	s.promptStore = store
	return s
}

func (s *Service) WithPromptCommandResolver(resolver PromptCommandResolver) *Service {
	if s == nil {
		return nil
	}
	s.promptCommands = resolver
	return s
}

func (s *Service) WithWorkflowTaskSessionResolver(resolver WorkflowTaskSessionResolver) *Service {
	if s == nil {
		return nil
	}
	s.workflowTasks = resolver
	return s
}

func (s *Service) WithWorkflowSessionReactivator(reactivator WorkflowSessionReactivator) *Service {
	if s == nil {
		return nil
	}
	s.reactivator = reactivator
	return s
}

func (s *Service) WithWorkflowSessionPreparationReader(reader WorkflowSessionPreparationReader) *Service {
	if s == nil {
		return nil
	}
	s.preparations = reader
	return s
}

func (s *Service) WithWorkflowSessionContinuationValidator(
	validator workflowexecution.WorkflowSessionContinuationValidator,
) *Service {
	if s == nil {
		return nil
	}
	s.continuation = validator
	return s
}

func (s *Service) WithPersistedSessionResolver(resolver session.PersistedSessionResolver) *Service {
	if s == nil {
		return nil
	}
	s.persisted = resolver
	return s
}

func (s *Service) WithLiveWatchPromptSources(prompts promptcontrol.PendingPromptSource, attention servicecontract.AttentionNotificationService) *Service {
	if s != nil {
		s.pendingPrompts, s.attention = prompts, attention
	}
	return s
}

func (s *Service) withRuntime(ctx context.Context, sessionID string, fn func(context.Context, *runtime.Engine) error) error {
	if s == nil || s.authority == nil {
		return errors.New("session runtime authority is required")
	}
	id, err := runtimeids.ParseSessionID(strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	return s.authority.WithCurrentRuntime(ctx, id, fn)
}

type runtimeCommandAttempt struct {
	caller     context.Context
	ctx        context.Context
	cancel     context.CancelCauseFunc
	stopCaller func() bool

	mu       sync.Mutex
	accepted bool
	finished bool
}

func newRuntimeCommandAttempt(caller context.Context) *runtimeCommandAttempt {
	if caller == nil {
		caller = context.Background()
	}
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(caller))
	attempt := &runtimeCommandAttempt{caller: caller, ctx: ctx, cancel: cancel}
	attempt.stopCaller = context.AfterFunc(caller, func() {
		attempt.mu.Lock()
		defer attempt.mu.Unlock()
		if !attempt.accepted && !attempt.finished {
			attempt.cancel(context.Cause(caller))
		}
	})
	if cause := context.Cause(caller); cause != nil {
		attempt.cancel(cause)
	}
	return attempt
}

func (a *runtimeCommandAttempt) Context() context.Context {
	if a == nil {
		return context.Background()
	}
	return a.ctx
}

func (a *runtimeCommandAttempt) Accept(commit func() (bool, error)) (bool, error) {
	if a == nil {
		return false, errors.New("runtime command attempt is required")
	}
	if commit == nil {
		return false, errors.New("runtime command acceptance mutation is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished {
		return false, errors.New("runtime command attempt already finished")
	}
	if a.accepted {
		return false, errors.New("runtime command was accepted more than once")
	}
	if cause := context.Cause(a.caller); cause != nil {
		a.cancel(cause)
		return false, cause
	}
	committed, err := commit()
	if committed {
		a.accepted = true
		if a.stopCaller != nil {
			a.stopCaller()
		}
	}
	return committed, err
}

func (a *runtimeCommandAttempt) Accepted() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.accepted
}

func (a *runtimeCommandAttempt) Finish() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finished = true
	if a.stopCaller != nil {
		a.stopCaller()
	}
	a.cancel(context.Canceled)
}

func runRuntimeCommand[Resp any](
	ctx context.Context,
	run func(context.Context) (Resp, bool, error),
) (Resp, error) {
	var zero Resp
	response, accepted, err := run(ctx)
	if !accepted {
		if errors.Is(err, serverapi.ErrRuntimeCommandNotAccepted) {
			return zero, err
		}
		if err == nil {
			err = errors.New("runtime command completed without accepting a mutation")
		}
		return zero, serverapi.NewRuntimeCommandNotAcceptedError(err)
	}
	return response, err
}

func (s *Service) SetSessionName(ctx context.Context, req *runtimepb.SetSessionNameRequest) error {
	if err := protoapi.Validate(req); err != nil {
		return err
	}
	return s.withRuntime(ctx, req.SessionId, func(callbackCtx context.Context, engine *runtime.Engine) error {
		changed, err := engine.SetSessionName(callbackCtx, req.Name)
		if err != nil {
			return err
		}
		if publisher, ok := s.activity.(sessionSettingPublisher); ok {
			name := strings.TrimSpace(req.Name)
			return publisher.PublishSessionSettingFeedback(req.SessionId, &transcriptpb.SessionSettingFeedback{
				Kind:    transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_SESSION_NAME,
				Changed: changed,
				Value:   &transcriptpb.SessionSettingFeedback_SessionName{SessionName: name},
			})
		}
		return nil
	})
}

func (s *Service) AppendCommittedEntry(ctx context.Context, req *transcriptpb.AppendCommittedEntryRequest) error {
	if err := protoapi.Validate(req); err != nil {
		return err
	}
	visibility, err := protoapi.AppendVisibilityFromProto(req.Visibility)
	if err != nil {
		return err
	}
	return s.withRuntime(ctx, req.SessionId, func(callbackCtx context.Context, engine *runtime.Engine) error {
		if visibility == transcript.EntryVisibilityAuto && strings.TrimSpace(req.GetNoticeId()) != "" {
			return engine.AppendCommittedEntryWithNoticeID(callbackCtx, req.Role, req.Text, req.GetNoticeId())
		}
		if visibility == transcript.EntryVisibilityAuto {
			return engine.AppendCommittedEntry(callbackCtx, req.Role, req.Text)
		}
		return engine.AppendCommittedEntryWithVisibility(callbackCtx, req.Role, req.Text, visibility)
	})
}

func (s *Service) AppendSessionEntry(ctx context.Context, sessionID string, role string, text string) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedRole := strings.TrimSpace(role)
	trimmedText := strings.TrimSpace(text)
	if trimmedSessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if trimmedRole == "" {
		return fmt.Errorf("role is required")
	}
	if trimmedText == "" {
		return fmt.Errorf("text is required")
	}
	return s.withRuntime(ctx, trimmedSessionID, func(callbackCtx context.Context, engine *runtime.Engine) error {
		return engine.AppendCommittedEntry(callbackCtx, trimmedRole, trimmedText)
	})
}

func (s *Service) ShouldCompactBeforeUserMessage(ctx context.Context, req *runtimepb.ShouldCompactRequest) (*runtimepb.ShouldCompactSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	var shouldCompact bool
	err := s.withRuntime(ctx, req.SessionId, func(callbackCtx context.Context, engine *runtime.Engine) error {
		var err error
		shouldCompact, err = engine.ShouldCompactBeforeUserMessage(callbackCtx, req.Text)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &runtimepb.ShouldCompactSuccess{ShouldCompact: shouldCompact}, nil
}

func (s *Service) SubmitUserShellCommand(ctx context.Context, req *runtimepb.ShellCommandRequest) error {
	if err := protoapi.Validate(req); err != nil {
		return err
	}
	_, err := runRuntimeCommand(ctx, func(ctx context.Context) (struct{}, bool, error) {
		attempt := newRuntimeCommandAttempt(ctx)
		defer attempt.Finish()
		commandErr := s.runAgentExecution(attempt.Context(), req.SessionId, func(runCtx context.Context, engine *runtime.Engine) error {
			_, err := engine.SubmitUserShellCommandWithAcceptance(runCtx, req.Command, attempt.Accept)
			return err
		})
		return struct{}{}, attempt.Accepted(), commandErr
	})
	return err
}

func (s *Service) CompactContext(ctx context.Context, req *runtimepb.CompactContextRequest) error {
	if err := protoapi.Validate(req); err != nil {
		return err
	}
	_, err := runRuntimeCommand(ctx, func(ctx context.Context) (struct{}, bool, error) {
		attempt := newRuntimeCommandAttempt(ctx)
		defer attempt.Finish()
		commandErr := s.runAgentExecution(attempt.Context(), req.SessionId, func(runCtx context.Context, engine *runtime.Engine) error {
			return admitManualCompaction(runCtx, engine, req, attempt.Accept)
		})
		return struct{}{}, attempt.Accepted(), commandErr
	})
	return err
}

func (s *Service) AdmitManualCompaction(
	ctx context.Context,
	req *runtimepb.CompactContextRequest,
) (bool, error) {
	if err := protoapi.Validate(req); err != nil {
		return false, err
	}
	attempt := newRuntimeCommandAttempt(ctx)
	defer attempt.Finish()
	commandErr := s.withRuntime(attempt.Context(), req.SessionId, func(runCtx context.Context, engine *runtime.Engine) error {
		workflowState, stateErr := engine.WorkflowSessionState()
		if stateErr != nil {
			return stateErr
		}
		active := engine.ActiveRun()
		if workflowState != nil && (active == nil || active.ActiveKind != runtime.ActiveKindWorkflowTurn) {
			return serverapi.ErrRuntimeUnavailable
		}
		return admitManualCompaction(runCtx, engine, req, attempt.Accept)
	})
	return attempt.Accepted(), commandErr
}

func admitManualCompaction(
	ctx context.Context,
	engine *runtime.Engine,
	req *runtimepb.CompactContextRequest,
	accept runtime.CommandAcceptance,
) error {
	requestID, err := runtimeids.ParseCompactionRequestID(req.RequestId)
	if err != nil {
		return err
	}
	_, err = engine.CompactContextAdmissionForRequestWithAcceptance(
		ctx,
		requestID,
		protoapi.ManualCompactionAdmissionFromProto(req.Admission),
		accept,
	)
	return err
}

func (s *Service) Interrupt(ctx context.Context, req *runtimepb.InterruptRequest) (*runtimepb.ReadModelUpdate, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.authority == nil {
		return nil, errors.New("session runtime authority is required")
	}
	sessionID := strings.TrimSpace(req.SessionId)
	return s.interrupt(ctx, sessionID)
}

func (s *Service) interrupt(ctx context.Context, sessionID string) (*runtimepb.ReadModelUpdate, error) {
	sessionID = strings.TrimSpace(sessionID)
	id, err := runtimeids.ParseSessionID(sessionID)
	if err != nil {
		return nil, err
	}
	interrupted, err := s.authority.InterruptCurrentAgentTurn(ctx, id, nil)
	if err == nil && !interrupted {
		err = serverapi.NewRuntimeCommandNotAcceptedError(errors.New("no active Agent Turn"))
	}
	switch {
	case errors.Is(err, sessionruntime.ErrExecutionNoLongerLive):
		err = serverapi.NewRuntimeCommandNotAcceptedError(errors.New("no active Agent Turn"))
	case errors.Is(err, serverapi.ErrRuntimeUnavailable):
		err = serverapi.NewRuntimeCommandNotAcceptedError(err)
	}
	if err != nil {
		return nil, err
	}
	return s.runtimeInterruptResponse(ctx, sessionID)
}

func (s *Service) runtimeInterruptResponse(ctx context.Context, sessionID string) (*runtimepb.ReadModelUpdate, error) {
	var snapshot *runtimepb.ReadModelUpdate
	var err error
	if s.activity != nil {
		snapshot, err = s.activity.RuntimeReadModelFeedSnapshot(ctx, sessionID)
	} else {
		err = errors.New("runtime activity resolver is unavailable")
	}
	if err != nil {
		slog.WarnContext(ctx, "runtime interrupt activity snapshot unavailable", "session_id", sessionID, "error", err)
		version := runtimeactivity.NextReadModelVersion(sessionID)
		return &runtimepb.ReadModelUpdate{
			Version: version,
			Activity: &runtimepb.Activity{
				State:              runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE,
				Reviewer:           runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
				DiagnosticRecovery: true,
			},
		}, nil
	}
	return &runtimepb.ReadModelUpdate{
		Version:  snapshot.Version,
		Activity: snapshot.Activity,
	}, nil
}

func (s *Service) ListPendingWork(ctx context.Context, req *runtimepb.ListPendingWorkRequest) (*runtimepb.ListPendingWorkSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	var pending runtimeinput.PendingWork
	err := s.withRuntime(ctx, req.SessionId, func(_ context.Context, engine *runtime.Engine) error {
		var snapshotErr error
		pending, snapshotErr = engine.PendingWorkSnapshot()
		return snapshotErr
	})
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return &runtimepb.ListPendingWorkSuccess{
			PendingWork: &runtimepb.PendingWork{},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	work, err := protoapi.PendingWorkToProto(pending)
	if err != nil {
		return nil, err
	}
	return &runtimepb.ListPendingWorkSuccess{PendingWork: work}, nil
}

func (s *Service) RemovePendingWork(ctx context.Context, req *runtimepb.RemovePendingWorkRequest) (*runtimepb.RemovePendingWorkSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	itemID, err := runtimeids.ParseQueueItemID(req.ItemId)
	if err != nil {
		return nil, err
	}
	var restoration runtimeinput.PendingWorkRestoration
	err = s.withRuntime(ctx, req.SessionId, func(callbackCtx context.Context, engine *runtime.Engine) error {
		var removeErr error
		restoration, removeErr = engine.RemovePendingWork(callbackCtx, itemID)
		var notPending *runtimeinput.PendingWorkRemovalError
		if errors.As(removeErr, &notPending) {
			return &serverapi.PendingWorkNotPendingError{ItemID: notPending.ItemID}
		}
		return removeErr
	})
	if err != nil {
		return nil, err
	}
	kind, err := protoapi.PendingWorkKindToProto(restoration.Kind)
	if err != nil {
		return nil, err
	}
	return &runtimepb.RemovePendingWorkSuccess{Restoration: &runtimepb.PendingWorkRestoration{
		Kind: kind, CanonicalInput: restoration.CanonicalInput,
	}}, nil
}

func (s *Service) RecordPromptHistory(ctx context.Context, req *promptpb.RecordHistoryRequest) error {
	if err := protoapi.Validate(req); err != nil {
		return err
	}
	return s.withRuntime(ctx, req.SessionId, func(_ context.Context, _ *runtime.Engine) error {
		_, err := s.recordPromptHistory(ctx, strings.TrimSpace(req.SessionId), req.Text)
		return err
	})
}

func (s *Service) recordPromptHistory(ctx context.Context, sessionID string, text string) (metadata.PromptHistoryRecord, error) {
	if s == nil || s.promptStore == nil {
		return metadata.PromptHistoryRecord{}, nil
	}
	return s.promptStore.RecordPromptHistoryEntry(ctx, metadata.PromptHistoryEntry{
		SessionID: strings.TrimSpace(sessionID),
		Text:      text,
	})
}

func (s *Service) rejectWorkflowAutoCompactionDisable(ctx context.Context, sessionID string, engine *runtime.Engine) error {
	workflowSession, err := s.workflowTaskSession(ctx, sessionID, engine)
	if err != nil {
		return err
	}
	if workflowSession {
		return errWorkflowTaskSessionAutoCompactionDisable
	}
	return nil
}

func (s *Service) workflowTaskSession(ctx context.Context, sessionID string, engine *runtime.Engine) (bool, error) {
	if engine != nil {
		workflowState, err := engine.WorkflowSessionState()
		if err != nil {
			return false, err
		}
		if workflowState != nil && workflowState.TaskID != "" {
			return true, nil
		}
	}
	if s != nil && s.workflowTasks != nil {
		workflow, err := s.workflowTasks.SessionHasWorkflowTask(ctx, sessionID)
		if err != nil {
			return false, err
		}
		return workflow, nil
	}
	return false, nil
}

var _ servicecontract.RuntimeControlService = (*Service)(nil)
