package app

import (
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"

	"context"
	"core/shared/apicontract"
	"core/shared/authstatus"
	"core/shared/client"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/lifecyclecontract"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	projectpb "core/shared/protoapi/gen/kent/api/project"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type launchMode string

const (
	launchModeInteractive launchMode = "interactive"
	launchModeHeadless    launchMode = "headless"
)

type sessionLaunchRequest struct {
	Mode      launchMode
	Intent    serverapi.SessionLaunchIntent
	Overrides serverapi.RunPromptOverrides
}

type sessionLaunchPlan struct {
	Mode                       launchMode
	SessionID                  string
	ActiveSettings             config.Settings
	EnabledTools               []toolspec.ID
	ConfiguredModelName        *string
	SessionTitle               *string
	ModelContractLocked        bool
	QuestionsEnabled           bool
	AutoCompactionEnabled      bool
	ThinkingOverrideExplicit   bool
	ActivationAgentSelection   *serverapi.SessionRuntimeAgentSelection
	ExplicitToolSelection      *config.ToolSelection
	StatusConfig               uiStatusConfig
	ExecutionTarget            *worktreepb.SessionExecutionTarget
	Source                     config.SourceReport
	ClientLifecycleCommand     []string
	ClientLifecycleOpeningKind lifecyclecontract.OpeningKind
}

type runtimeLaunchPlan struct {
	Wiring           *runtimeWiring
	stopEventStreams func()
	close            func() error
	detachClose      func() error
	closeOnce        sync.Once
	closeErr         error
}

func validateLaunchSessionTitle(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if strings.TrimSpace(*value) == "" {
		return nil, errors.New("session launch title cannot be blank")
	}
	title := *value
	return &title, nil
}

func (p *runtimeLaunchPlan) Close() error {
	return p.closeWithPolicy(false)
}

func (p *runtimeLaunchPlan) DetachOnlyClose() error {
	return p.closeWithPolicy(true)
}

func (p *runtimeLaunchPlan) closeWithPolicy(detachOnly bool) error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		if p.stopEventStreams != nil {
			p.stopEventStreams()
		}
		if detachOnly && p.detachClose != nil {
			p.closeErr = p.detachClose()
		} else if p.close != nil {
			p.closeErr = p.close()
		}
	})
	return p.closeErr
}

type sessionPickerRunner func(context.Context, sessionPageLoader, string, sessionPickerHeaderInfo) (sessionPickerResult, error)

type sessionViewReader interface {
	GetSessionMainView(ctx context.Context, req *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error)
}

type launchPlannerServer interface {
	Connection() config.Connection
	LocalPreferences() config.LocalPreferences
	ProjectBinding() (client.ProjectAttachment, bool)
	ChatSettingsClient() apicontract.ChatSettingsService
	PresentationTheme() string
	ProjectID() string
	AuthStatusClient() apicontract.AuthStatusService
	ProjectViewClient() apicontract.ProjectViewService
	ServerStatusClient() apicontract.ServerStatusService
	SessionLaunchClient() apicontract.SessionLaunchService
	SessionViewClient() apicontract.SessionViewService
}

type launchPlanner struct {
	server      launchPlannerServer
	pickSession sessionPickerRunner
}

func newSessionLaunchPlanner(server launchPlannerServer) *launchPlanner {
	return &launchPlanner{server: server, pickSession: runSessionPickerFlow}
}

type projectScopedSessionPageLoader struct {
	projectID string
	client    apicontract.ProjectViewService
}

func (l projectScopedSessionPageLoader) ProjectID() string {
	return l.projectID
}

func (l projectScopedSessionPageLoader) ListSessionPage(ctx context.Context, request sessionPageRequest) (sessionPageResponse, error) {
	category, err := protoapi.SessionCategoryToProto(request.Category)
	if err != nil {
		return sessionPageResponse{}, err
	}
	generatedRequest := &projectpb.SessionPageRequest{ProjectId: request.ProjectID, Category: category}
	if request.Offset != nil {
		offset := int32(*request.Offset)
		generatedRequest.Offset = &offset
	}
	if request.Limit != nil {
		limit := int32(*request.Limit)
		generatedRequest.Limit = &limit
	}
	response, err := l.client.ListSessionPage(ctx, generatedRequest)
	if err != nil {
		return sessionPageResponse{}, err
	}
	responseCategory, err := protoapi.SessionCategoryFromProto(response.Category)
	if err != nil {
		return sessionPageResponse{}, err
	}
	sessions, err := client.SessionSummariesFromProto(response.Sessions)
	if err != nil {
		return sessionPageResponse{}, err
	}
	page := sessionPageResponse{
		ProjectID: response.ProjectId,
		Category:  responseCategory,
		Sessions:  sessions,
	}
	if response.NextOffset != nil {
		nextOffset := int(*response.NextOffset)
		page.NextOffset = &nextOffset
	}
	return page, nil
}

func (p *launchPlanner) PlanSession(ctx context.Context, req sessionLaunchRequest) (sessionLaunchPlan, error) {
	if p == nil || p.server == nil || p.server.SessionLaunchClient() == nil {
		return sessionLaunchPlan{}, errors.New("launch planner bootstrap is required")
	}
	if err := req.Intent.Validate(); err != nil {
		return sessionLaunchPlan{}, err
	}
	var mode sessionlaunchpb.SessionLaunchMode
	switch req.Mode {
	case launchModeInteractive:
		mode = sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE
	case launchModeHeadless:
		mode = sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_HEADLESS
	default:
		return sessionLaunchPlan{}, errors.New("Session launch mode is invalid")
	}
	intent, err := protoapi.SessionLaunchIntentToProto(req.Intent)
	if err != nil {
		return sessionLaunchPlan{}, err
	}
	overrides := req.Overrides
	generatedRequest := &sessionlaunchpb.SessionPlanRequest{Mode: mode, Intent: intent}
	if overrides.HasAny() {
		generatedRequest.Overrides, err = protoapi.RunPromptOverridesToProto(overrides)
		if err != nil {
			return sessionLaunchPlan{}, err
		}
	}
	resp, err := p.server.SessionLaunchClient().PlanSession(ctx, generatedRequest)
	if err != nil {
		return sessionLaunchPlan{}, err
	}
	if resp == nil || resp.Plan == nil {
		return sessionLaunchPlan{}, errors.New("Session launch plan is required")
	}
	settings, err := protoapi.SessionSettingsFromProto(resp.Plan.ActiveSettings)
	if err != nil {
		return sessionLaunchPlan{}, err
	}
	source, err := protoapi.SessionSourceReportFromProto(resp.Plan.Source)
	if err != nil {
		return sessionLaunchPlan{}, err
	}
	executionTarget, err := loadSelectedSessionExecutionTarget(ctx, p.server.SessionViewClient(), resp.Plan.SessionId)
	if err != nil {
		return sessionLaunchPlan{}, err
	}
	enabledTools := make([]toolspec.ID, 0, len(resp.Plan.EnabledToolIds))
	for _, generated := range resp.Plan.EnabledToolIds {
		id, err := protoapi.SessionToolIDFromProto(generated)
		if err != nil {
			return sessionLaunchPlan{}, err
		}
		enabledTools = append(enabledTools, id)
	}
	cfg := p.server.Connection()
	activeSettings := settings
	local := p.server.LocalPreferences()
	activeSettings.Theme = p.server.PresentationTheme()
	activeSettings.Debug = local.Debug
	activeSettings.NotificationMethod = local.NotificationMethod
	activeSettings.TUINativeProgressBar = local.TUINativeProgressBar
	authSelection := authstatus.ProviderSelection(activeSettings)
	sessionTitle, err := validateLaunchSessionTitle(resp.Plan.SessionName)
	if err != nil {
		return sessionLaunchPlan{}, err
	}
	activationAgentSelection, err := protoapi.SessionRuntimeAgentSelectionFromProto(
		resp.Plan.ActivationAgentSelection,
	)
	if err != nil {
		return sessionLaunchPlan{}, err
	}
	explicitTools, err := protoapi.ToolSelectionFromProto(resp.Plan.ExplicitToolSelection)
	if err != nil {
		return sessionLaunchPlan{}, err
	}
	return sessionLaunchPlan{
		Mode:                     req.Mode,
		SessionID:                resp.Plan.SessionId,
		ActiveSettings:           activeSettings,
		EnabledTools:             enabledTools,
		ConfiguredModelName:      textutil.Pointer(resp.Plan.ConfiguredModelName),
		SessionTitle:             sessionTitle,
		ModelContractLocked:      resp.Plan.ModelContractLocked,
		QuestionsEnabled:         resp.Plan.QuestionsEnabled,
		AutoCompactionEnabled:    resp.Plan.AutoCompactionEnabled,
		ThinkingOverrideExplicit: resp.Plan.ThinkingOverrideExplicit,
		ActivationAgentSelection: activationAgentSelection,
		ExplicitToolSelection:    explicitTools,
		StatusConfig: uiStatusConfig{
			WorkspaceRoot:   executionTarget.EffectiveWorkdir,
			ExecutionTarget: executionTarget,
			PersistenceRoot: cfg.PersistenceRoot,
			SessionViews:    p.server.SessionViewClient(),
			Settings:        activeSettings,
			AuthSelection:   authSelection,
			Source:          source,
			AuthStatus:      p.server.AuthStatusClient(),
		},
		ExecutionTarget: executionTarget,
		Source:          source,
	}, nil
}

func loadSelectedSessionExecutionTarget(ctx context.Context, sessionViews sessionViewReader, sessionID string) (*worktreepb.SessionExecutionTarget, error) {
	if sessionViews == nil {
		return nil, errors.New("session view client is required")
	}
	resp, err := sessionViews.GetSessionMainView(ctx, &sessionpb.MainViewRequest{SessionId: strings.TrimSpace(sessionID)})
	if err != nil {
		return nil, err
	}
	return clientui.NormalizeSessionExecutionTarget(resp.MainView.Session.ExecutionTarget), nil
}

func (p *launchPlanner) PrepareRuntime(ctx context.Context, plan sessionLaunchPlan, diagnosticWriter io.Writer, startLogLine string) (*runtimeLaunchPlan, error) {
	if p == nil || p.server == nil {
		return nil, io.ErrClosedPipe
	}
	runtimeServer, ok := p.server.(runtimeAttachmentSource)
	if !ok {
		return nil, errors.New("runtime attachment server is required")
	}
	return prepareSharedRuntime(ctx, runtimeServer, plan, diagnosticWriter, startLogLine)
}

func (p *launchPlanner) selectSession(ctx context.Context, notice *startupPickerNotice) (sessionPickerResult, error) {
	if p == nil || p.server == nil || p.server.ProjectViewClient() == nil {
		return nil, errors.New("session picker project view client is required")
	}
	if p.pickSession == nil {
		return nil, errors.New("session picker is required")
	}
	projectID := strings.TrimSpace(p.server.ProjectID())
	if projectID == "" {
		return nil, errors.New("session picker project ID is required")
	}
	loader := projectScopedSessionPageLoader{
		projectID: projectID,
		client:    p.server.ProjectViewClient(),
	}
	header, err := p.sessionPickerHeaderInfo()
	if err != nil {
		return nil, err
	}
	header.Notice = notice
	return p.pickSession(ctx, loader, p.server.PresentationTheme(), header)
}

func (p *launchPlanner) sessionPickerHeaderInfo() (sessionPickerHeaderInfo, error) {
	cfg := p.server.Connection()
	binding, present := p.server.ProjectBinding()
	if !present {
		return sessionPickerHeaderInfo{}, errors.New("workspace binding is required for Chat settings")
	}
	request := &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_NewChat{NewChat: &chatsettingspb.NewChatTarget{
			ProjectId: binding.ProjectID, WorkspaceId: binding.WorkspaceID,
		}},
	}
	return sessionPickerHeaderInfo{
		Version: config.Version,
		StatusRequest: populateStatusRequestCacheKeys(uiStatusRequest{
			WorkspaceRoot: binding.WorkspaceRoot, PersistenceRoot: cfg.PersistenceRoot,
		}),
		ServerAddress: net.JoinHostPort(cfg.ServerHost, strconv.Itoa(cfg.ServerPort)),
		Debug:         p.server.LocalPreferences().Debug,
		updateStatus:  p.server.ServerStatusClient(),
		loadModelFacts: func(ctx context.Context) (*sessionPickerModelFacts, error) {
			return loadSessionPickerModelFacts(ctx, p.server.ChatSettingsClient(), request)
		},
	}, nil
}

func loadSessionPickerModelFacts(ctx context.Context, service apicontract.ChatSettingsService, request *chatsettingspb.ReadRequest) (*sessionPickerModelFacts, error) {
	response, err := service.ReadChatSettings(ctx, request)
	if err != nil {
		return nil, err
	}
	catalog := response.GetNewChat()
	if catalog == nil || catalog.InitialSettings == nil {
		return nil, errors.New("new Chat catalog is required")
	}
	var modelFacts *sessionPickerModelFacts
	for _, choice := range catalog.Choices {
		if choice.Agent.Role == catalog.InitialSettings.AgentRole {
			if choice.Agent.Model != nil {
				modelFacts = &sessionPickerModelFacts{Name: choice.Agent.Model, ThinkingLevel: choice.Agent.Thinking}
			}
			break
		}
	}
	return modelFacts, nil
}

func mergeSessionPlanOverrides(base serverapi.RunPromptOverrides, override serverapi.RunPromptOverrides) serverapi.RunPromptOverrides {
	merged := base
	if override.AgentRole != nil {
		value := strings.TrimSpace(*override.AgentRole)
		merged.AgentRole = &value
	}
	if value := strings.TrimSpace(override.Model); value != "" {
		merged.Model = value
	}
	if value := strings.TrimSpace(override.ThinkingLevel); value != "" {
		merged.ThinkingLevel = value
	}
	if value := strings.TrimSpace(override.Theme); value != "" {
		merged.Theme = value
	}
	if override.ModelTimeoutSeconds > 0 {
		merged.ModelTimeoutSeconds = override.ModelTimeoutSeconds
	}
	if value := strings.TrimSpace(override.Tools); value != "" {
		merged.Tools = value
	}
	return merged
}
