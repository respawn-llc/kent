package sessionview

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/server/auth"
	"core/server/chatcontext"
	"core/server/launch"
	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	servicecontract "core/shared/apicontract"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/invariant"
	"core/shared/protoapi"
	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type PersistedSessionResolver = session.PersistedSessionResolver

type ExecutionTargetResolver interface {
	ResolveSessionExecutionTarget(ctx context.Context, sessionID string) (*worktreepb.SessionExecutionTarget, error)
}

type chatContextWorkspaceResolver interface {
	Resolve(workspaceRoot string) (config.App, error)
}

type chatContextAuthReader interface {
	Load(context.Context) (auth.State, error)
}

type workflowSessionStatusResolver interface {
	SessionWorkflowStatus(context.Context, string) (*runtimepb.WorkflowSessionStatus, error)
}

type dormantChatProjection struct {
	target                *worktreepb.SessionExecutionTarget
	settings              config.Settings
	autoCompactionEnabled bool
	questionsEnabled      bool
	fastModeAvailable     bool
	context               *contextpb.Context
	workflow              *runtimepb.WorkflowSessionStatus
}

type Service struct {
	persisted         PersistedSessionResolver
	mainViews         runtimeMainViewSnapshotProvider
	targets           ExecutionTargetResolver
	cacheWarningMode  config.CacheWarningMode
	contextWorkspaces chatContextWorkspaceResolver
	contextAuth       chatContextAuthReader
	workflowSessions  workflowSessionStatusResolver
}

func NewService(
	sessions PersistedSessionResolver,
	mainViews runtimeMainViewSnapshotProvider,
	targets ExecutionTargetResolver,
) *Service {
	svc := &Service{
		persisted:        sessions,
		mainViews:        mainViews,
		targets:          targets,
		cacheWarningMode: config.CacheWarningModeDefault,
	}
	if workflowSessions, ok := sessions.(workflowSessionStatusResolver); ok {
		svc.workflowSessions = workflowSessions
	}
	return svc
}

func (s *Service) SubscribeQuestionHistory(
	ctx context.Context,
	req *sessionpb.QuestionHistorySubscribeRequest,
) (serverapi.QuestionHistorySubscription, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.persisted == nil {
		return nil, session.ErrPersistedSessionResolverRequired
	}
	record, err := s.persisted.ResolvePersistedSession(ctx, req.SessionId)
	if err != nil {
		return nil, err
	}
	cursor, err := session.OpenQuestionHistoryCursor(record.SessionDir, int(req.MaxHandoffs))
	if err != nil {
		return nil, err
	}
	return &questionHistorySubscription{cursor: cursor}, nil
}

func (s *Service) WithChatContextWorkspaceResolver(resolver chatContextWorkspaceResolver) *Service {
	if s != nil {
		s.contextWorkspaces = resolver
	}
	return s
}

func (s *Service) WithChatContextAuthReader(reader chatContextAuthReader) *Service {
	if s != nil {
		s.contextAuth = reader
	}
	return s
}

func (s *Service) ReadSessionChatContext(ctx context.Context, sessionID runtimeids.SessionID) (*contextpb.Context, error) {
	if s == nil || s.persisted == nil {
		return nil, errors.New("persisted Session resolver is required")
	}
	return s.readDormantSessionChatContext(ctx, sessionID)
}

func (s *Service) readDormantSessionChatContext(ctx context.Context, sessionID runtimeids.SessionID) (*contextpb.Context, error) {
	record, err := session.ResolvePersistedSessionRecord(ctx, s.persisted, sessionID.String())
	if err != nil {
		return nil, err
	}
	projection, err := s.resolveDormantChatProjection(ctx, session.ContextSnapshot{
		Meta:  *record.Meta,
		Facts: record.ContextFacts,
	})
	if err != nil {
		return nil, err
	}
	return projection.context, nil
}

func (s *Service) resolveDormantChatProjection(ctx context.Context, snapshot session.ContextSnapshot) (dormantChatProjection, error) {
	if s.targets == nil {
		return dormantChatProjection{}, errors.New("Session execution-target resolver is required")
	}
	if s.contextWorkspaces == nil {
		return dormantChatProjection{}, errors.New("fresh workspace config resolver is required")
	}
	target, err := s.targets.ResolveSessionExecutionTarget(ctx, snapshot.Meta.SessionID)
	if err != nil {
		return dormantChatProjection{}, err
	}
	executionRoot, err := clientui.SessionExecutionWorkspaceRoot(target, target.WorkspaceRoot)
	if err != nil {
		return dormantChatProjection{}, err
	}
	app, err := s.contextWorkspaces.Resolve(executionRoot)
	if err != nil {
		return dormantChatProjection{}, err
	}
	current, err := launch.ResolveReadOnlySessionContextSettings(app, snapshot.Meta, false)
	if err != nil {
		return dormantChatProjection{}, err
	}
	capabilities, err := llm.ResolveEffectiveProviderCapabilities(
		ctx,
		snapshot.Meta.Locked,
		current.Settings,
		s.contextAuth,
	)
	if err != nil {
		return dormantChatProjection{}, err
	}
	policy := chatcontext.ResolvePolicy(current.Settings, capabilities, snapshot.Meta.Locked)
	usedTokens := int64(0)
	if snapshot.Meta.UsageState != nil {
		usedTokens = int64(snapshot.Meta.UsageState.InputTokens)
	}
	completedCount := int64(0)
	if snapshot.Facts.CompletedCompactionCount != nil {
		completedCount = int64(*snapshot.Facts.CompletedCompactionCount)
	}
	manualEligible := snapshot.Facts.ManualCompactEligible != nil && *snapshot.Facts.ManualCompactEligible
	contextView := chatcontext.Project(chatcontext.ProjectionInput{
		Policy:                   policy,
		UsedTokens:               usedTokens,
		AutoCompactionEnabled:    current.AutoCompactionEnabled,
		CompletedCompactionCount: completedCount,
		ManualCompactEligible:    manualEligible,
	})
	var workflowSession *runtimepb.WorkflowSessionStatus
	if s.workflowSessions != nil {
		workflowSession, err = s.workflowSessions.SessionWorkflowStatus(ctx, snapshot.Meta.SessionID)
		if err != nil {
			return dormantChatProjection{}, err
		}
	}
	return dormantChatProjection{
		target:                target,
		settings:              chatcontext.ApplyPolicy(current.Settings, policy),
		autoCompactionEnabled: current.AutoCompactionEnabled,
		questionsEnabled:      current.QuestionsEnabled,
		fastModeAvailable:     llm.SupportsFastModeProvider(capabilities),
		context:               contextView,
		workflow:              workflowSession,
	}, nil
}

func (s *Service) WithCacheWarningMode(mode config.CacheWarningMode) *Service {
	if s == nil {
		return nil
	}
	s.cacheWarningMode = normalizeServiceCacheWarningMode(mode)
	return s
}

func normalizeServiceCacheWarningMode(mode config.CacheWarningMode) config.CacheWarningMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(config.CacheWarningModeOff):
		return config.CacheWarningModeOff
	case string(config.CacheWarningModeVerbose):
		return config.CacheWarningModeVerbose
	default:
		return config.CacheWarningModeDefault
	}
}

func (s *Service) GetSessionMainView(ctx context.Context, req *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	view, err := s.resolveMainView(ctx, req.SessionId)
	if err != nil {
		return nil, err
	}
	return &sessionpb.MainViewSuccess{MainView: view}, nil
}

func (s *Service) SessionTranscriptTailEntries(ctx context.Context, sessionID string) ([]runtime.ChatEntry, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, serverapi.ErrSessionIDRequired
	}
	view, err := session.ResolvePersistedSessionView(ctx, s.persisted, sessionID)
	if err != nil {
		return nil, err
	}
	return (dormantSessionSnapshot{view: view, cacheWarningMode: s.cacheWarningMode}).TranscriptTailEntries(ctx)
}

func (s *Service) GetSessionTranscriptPage(ctx context.Context, req *transcriptpb.PageRequest) (*transcriptpb.PageSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	view, err := session.ResolvePersistedSessionView(ctx, s.persisted, req.SessionId)
	if err != nil {
		return nil, err
	}
	page, err := (dormantSessionSnapshot{view: view, cacheWarningMode: s.cacheWarningMode}).TranscriptPage(ctx, req)
	if err != nil {
		return nil, err
	}
	response := &transcriptpb.PageSuccess{Transcript: page}
	if err := validateSessionTranscriptPageResponse(response); err != nil {
		return nil, err
	}
	return response, nil
}

func validateSessionTranscriptPageResponse(response *transcriptpb.PageSuccess) error {
	if err := protoapi.Validate(response); err != nil {
		invariant.NewPolicy().Check(false, invariant.ReadModelPublicationDiagnostic(
			invariant.ReadModelPublicationDiagnosticInput{
				Operation:        "session_view.transcript_page",
				SessionID:        response.GetTranscript().GetSessionId(),
				PublicationCause: err.Error(),
				OwnerSnapshots:   "canonical_transcript_page",
				ResolverInputs:   "session_transcript_page",
			},
		))
		return fmt.Errorf("validate session transcript page response: %w", err)
	}
	return nil
}

func (s *Service) GetLatestCommittedAssistantFinalAnswer(ctx context.Context, req *transcriptpb.LatestFinalAnswerRequest) (*transcriptpb.LatestFinalAnswerSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, session.ErrPersistedSessionResolverRequired
	}
	view, err := session.ResolvePersistedSessionView(ctx, s.persisted, req.SessionId)
	if err != nil {
		return nil, err
	}
	answer, err := runtime.LatestCommittedAssistantFinalAnswerFromEventLog(view)
	if err != nil {
		return nil, err
	}
	if answer != nil && strings.TrimSpace(*answer) == "" {
		return nil, errors.New("latest committed assistant final answer must not be blank")
	}
	return &transcriptpb.LatestFinalAnswerSuccess{Answer: answer}, nil
}

var _ servicecontract.SessionViewService = (*Service)(nil)
var _ chatcontext.SessionOwner = (*Service)(nil)
