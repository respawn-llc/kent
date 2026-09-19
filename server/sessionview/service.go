package sessionview

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"core/server/chatcontext"
	"core/server/launch"
	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/worktree"
	servicecontract "core/shared/apicontract"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/invariant"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type PersistedSessionResolver = session.PersistedSessionResolver

type promptHistoryReader interface {
	ReadPromptHistory(context.Context, string) ([]string, error)
}

type ExecutionTargetResolver interface {
	ResolveSessionExecutionTarget(ctx context.Context, sessionID string) (*worktreepb.SessionExecutionTarget, error)
}

type chatContextWorkspaceResolver interface {
	Resolve(workspaceRoot string) (config.App, error)
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
	promptHistory     promptHistoryReader
	persisted         PersistedSessionResolver
	mainViews         runtimeMainViewSnapshotProvider
	targets           ExecutionTargetResolver
	app               config.App
	auth              servicecontract.AuthStatusService
	git               *worktree.GitInspector
	cacheWarningMode  config.CacheWarningMode
	contextWorkspaces chatContextWorkspaceResolver
	workflowSessions  workflowSessionStatusResolver
}

func (s *Service) WithPromptHistoryReader(reader promptHistoryReader) *Service {
	s.promptHistory = reader
	return s
}

func (s *Service) GetPromptHistory(ctx context.Context, req *sessionpb.PromptHistoryRequest) (*sessionpb.PromptHistorySuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if _, err := session.ResolvePersistedSessionRecord(ctx, s.persisted, req.SessionId); err != nil {
		return nil, err
	}
	if s.promptHistory == nil {
		return nil, errors.New("prompt history reader is required")
	}
	prompts, err := s.promptHistory.ReadPromptHistory(ctx, req.SessionId)
	if err != nil {
		return nil, err
	}
	return &sessionpb.PromptHistorySuccess{Prompts: prompts}, nil
}

func (s *Service) WithExecutionEnvironmentConfig(app config.App) *Service {
	if s != nil {
		s.app = app
	}
	return s
}

func (s *Service) WithExecutionEnvironmentAuth(provider servicecontract.AuthStatusService) *Service {
	if s != nil {
		s.auth = provider
	}
	return s
}

func (s *Service) WithExecutionEnvironmentGit(inspector *worktree.GitInspector) *Service {
	if s != nil {
		s.git = inspector
	}
	return s
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
	provider, err := llm.ResolveEffectiveProviderCapabilities(
		snapshot.Meta.Locked,
		current.Settings,
	)
	if err != nil {
		return dormantChatProjection{}, err
	}
	policy := chatcontext.ResolvePolicy(current.Settings, provider.Capabilities, snapshot.Meta.Locked)
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
		fastModeAvailable:     llm.SupportsFastModeProvider(provider.Capabilities),
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

func (s *Service) GetSessionExecutionEnvironment(ctx context.Context, req *sessionpb.ExecutionEnvironmentRequest) (*sessionpb.ExecutionEnvironmentSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s == nil || s.persisted == nil {
		return nil, session.ErrPersistedSessionResolverRequired
	}
	record, err := session.ResolvePersistedSessionRecord(ctx, s.persisted, req.SessionId)
	if err != nil {
		return nil, err
	}
	meta := *record.Meta
	if strings.TrimSpace(meta.SessionID) != req.SessionId {
		return nil, fmt.Errorf("session execution environment identity mismatch: requested %q, resolved %q", req.SessionId, meta.SessionID)
	}
	environment := &sessionpb.ExecutionEnvironment{SessionId: req.SessionId}
	target, targetErr := s.resolveExecutionTarget(ctx, req.SessionId)
	if targetErr != nil {
		environment.Workspace = &sessionpb.ExecutionWorkspaceField{Result: &sessionpb.ExecutionWorkspaceField_Failed{Failed: &sessionpb.ExecutionFieldError{Code: sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE, Message: targetErr.Error()}}}
	} else {
		environment.Workspace = resolveSessionExecutionWorkspace(target)
	}
	environment.Branch = s.resolveBranch(ctx, target, environment.Workspace)
	model, modelErr := launch.ResolveReadOnlySessionModel(s.app, meta)
	if modelErr != nil {
		var unavailable *launch.ReadOnlySessionModelUnavailableError
		if errors.As(modelErr, &unavailable) {
			environment.Model = &sessionpb.ExecutionModelField{Result: &sessionpb.ExecutionModelField_Unavailable{Unavailable: unavailable.Reason}}
		} else {
			environment.Model = &sessionpb.ExecutionModelField{Result: &sessionpb.ExecutionModelField_Failed{Failed: &sessionpb.ExecutionFieldError{Code: sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_INVALID_CONFIGURATION, Message: modelErr.Error()}}}
		}
	} else {
		environment.Model = &sessionpb.ExecutionModelField{Result: &sessionpb.ExecutionModelField_Available{Available: &sessionpb.ExecutionModel{
			Name:     model.Name,
			Provider: model.Provider.ID(),
			Locked:   model.Locked,
		}}}
	}
	environment.Auth = s.resolveAuth(ctx, environment.Model)
	return &sessionpb.ExecutionEnvironmentSuccess{Environment: environment}, nil
}

func (s *Service) resolveExecutionTarget(ctx context.Context, sessionID string) (*worktreepb.SessionExecutionTarget, error) {
	if s.targets == nil {
		return nil, nil
	}
	return s.targets.ResolveSessionExecutionTarget(ctx, sessionID)
}

func resolveSessionExecutionWorkspace(target *worktreepb.SessionExecutionTarget) *sessionpb.ExecutionWorkspaceField {
	target = clientui.NormalizeSessionExecutionTarget(target)
	if clientui.SessionExecutionTargetIsZero(target) {
		return &sessionpb.ExecutionWorkspaceField{Result: &sessionpb.ExecutionWorkspaceField_Unavailable{
			Unavailable: sessionpb.ExecutionWorkspaceUnavailableReason_EXECUTION_WORKSPACE_UNAVAILABLE_REASON_NOT_CONFIGURED,
		}}
	}
	availability := target.WorkspaceAvailability
	targetKind := "workspace"
	if target.Worktree != nil {
		availability = target.Worktree.Availability
		targetKind = "worktree"
	}
	switch availability {
	case projectpb.ProjectAvailability_PROJECT_AVAILABILITY_MISSING, projectpb.ProjectAvailability_PROJECT_AVAILABILITY_INACCESSIBLE:
		return &sessionpb.ExecutionWorkspaceField{Result: &sessionpb.ExecutionWorkspaceField_Failed{Failed: &sessionpb.ExecutionFieldError{
			Code: sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE,
			Message: fmt.Sprintf(
				"session execution %s target is %s",
				targetKind,
				availability,
			),
		}}}
	}
	if strings.TrimSpace(target.EffectiveWorkdir) == "" {
		return &sessionpb.ExecutionWorkspaceField{Result: &sessionpb.ExecutionWorkspaceField_Failed{Failed: &sessionpb.ExecutionFieldError{
			Code:    sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE,
			Message: "session execution target has no effective workdir",
		}}}
	}
	return &sessionpb.ExecutionWorkspaceField{Result: &sessionpb.ExecutionWorkspaceField_Available{Available: &sessionpb.ExecutionWorkspace{Path: target.EffectiveWorkdir}}}
}

func (s *Service) resolveBranch(
	ctx context.Context,
	target *worktreepb.SessionExecutionTarget,
	workspace *sessionpb.ExecutionWorkspaceField,
) *sessionpb.ExecutionBranchField {
	if workspace.GetAvailable() == nil {
		return unavailableBranch(sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_NOT_GIT_REPOSITORY)
	}
	if s.git == nil {
		return unavailableBranch(sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_NOT_GIT_REPOSITORY)
	}
	entries, err := s.git.List(ctx, target.EffectiveWorkdir)
	if err != nil {
		var listErr *worktree.GitWorktreeListError
		if errors.As(err, &listErr) && listErr.Kind == worktree.GitWorktreeListErrorNotRepository {
			return unavailableBranch(sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_NOT_GIT_REPOSITORY)
		}
		return failedBranch(err)
	}
	canonicalWorkdir, err := config.CanonicalWorkspaceRoot(target.EffectiveWorkdir)
	if err != nil {
		return failedBranch(err)
	}
	for _, entry := range entries {
		relative, err := filepath.Rel(entry.Root, canonicalWorkdir)
		if err != nil || !filepath.IsLocal(relative) {
			continue
		}
		if relative == "." || filepath.IsLocal(relative) {
			if entry.Detached {
				return unavailableBranch(sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_DETACHED_HEAD)
			}
			if entry.Branch == nil {
				return unavailableBranch(sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_NOT_GIT_REPOSITORY)
			}
			return &sessionpb.ExecutionBranchField{Result: &sessionpb.ExecutionBranchField_Available{Available: &sessionpb.ExecutionBranch{Name: entry.Branch.Name()}}}
		}
	}
	return unavailableBranch(sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_NOT_GIT_REPOSITORY)
}

func unavailableBranch(reason sessionpb.ExecutionBranchUnavailableReason) *sessionpb.ExecutionBranchField {
	return &sessionpb.ExecutionBranchField{Result: &sessionpb.ExecutionBranchField_Unavailable{Unavailable: reason}}
}

func failedBranch(err error) *sessionpb.ExecutionBranchField {
	return &sessionpb.ExecutionBranchField{Result: &sessionpb.ExecutionBranchField_Failed{Failed: &sessionpb.ExecutionFieldError{
		Code: sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE, Message: err.Error(),
	}}}
}

func (s *Service) resolveAuth(ctx context.Context, model *sessionpb.ExecutionModelField) *sessionpb.ExecutionAuthField {
	effectiveModel := model.GetAvailable()
	if effectiveModel == nil || s.auth == nil || !sessionExecutionProviderUsesKentManagedAuth(effectiveModel.Provider) {
		return &sessionpb.ExecutionAuthField{Result: &sessionpb.ExecutionAuthField_Unavailable{
			Unavailable: sessionpb.ExecutionAuthUnavailableReason_EXECUTION_AUTH_UNAVAILABLE_REASON_NOT_APPLICABLE,
		}}
	}
	status, err := s.auth.GetStatus(ctx, &authpb.GetStatusRequest{})
	if err != nil {
		return failedAuth(err)
	}
	if unavailable := status.GetResolution().GetUnavailable(); unavailable != nil {
		return failedAuth(errors.New(unavailable.GetCause()))
	}
	method, err := sessionExecutionAuthMethodFromProto(status.GetResolution().GetKnown().GetMethod())
	if err != nil {
		return failedAuth(err)
	}
	return &sessionpb.ExecutionAuthField{Result: &sessionpb.ExecutionAuthField_Available{Available: &sessionpb.ExecutionAuth{
		Provider: effectiveModel.Provider,
		Method:   method,
	}}}
}

func failedAuth(err error) *sessionpb.ExecutionAuthField {
	return &sessionpb.ExecutionAuthField{Result: &sessionpb.ExecutionAuthField_Failed{Failed: &sessionpb.ExecutionFieldError{
		Code: sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE, Message: err.Error(),
	}}}
}

func sessionExecutionAuthMethodFromProto(method authpb.AuthMethod) (sessionpb.ExecutionAuthMethod, error) {
	switch method {
	case authpb.AuthMethod_AUTH_METHOD_NONE:
		return sessionpb.ExecutionAuthMethod_EXECUTION_AUTH_METHOD_NONE, nil
	case authpb.AuthMethod_AUTH_METHOD_API_KEY:
		return sessionpb.ExecutionAuthMethod_EXECUTION_AUTH_METHOD_API_KEY, nil
	case authpb.AuthMethod_AUTH_METHOD_OAUTH:
		return sessionpb.ExecutionAuthMethod_EXECUTION_AUTH_METHOD_O_AUTH, nil
	default:
		return sessionpb.ExecutionAuthMethod_EXECUTION_AUTH_METHOD_UNSPECIFIED, fmt.Errorf("invalid execution auth method %s", method)
	}
}

func sessionExecutionProviderUsesKentManagedAuth(provider string) bool {
	capabilities, err := llm.InferProviderCapabilities(strings.TrimSpace(provider))
	return err == nil && capabilities.IsOpenAIFirstParty
}

var _ servicecontract.SessionViewService = (*Service)(nil)
var _ chatcontext.SessionOwner = (*Service)(nil)
