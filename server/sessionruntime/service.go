package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata"
	"core/server/runlog"
	"core/server/runtime"
	"core/server/runtimewire"
	"core/server/session"
	"core/server/tools"
	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type API struct {
	metadataStore            *metadata.Store
	recoveredWarningProvider func() (string, bool, error)
	authority                *Authority
	runtimeClientFactory     runtimewire.RuntimeClientFactory
	managedWorktreeBaseDir   string
}

type APIOptions struct {
	RuntimeClientFactory     runtimewire.RuntimeClientFactory
	RecoveredWarningProvider func() (string, bool, error)
	ManagedWorktreeBaseDir   string
}

func NewAPI(metadataStore *metadata.Store, authority *Authority, options APIOptions) *API {
	return &API{
		metadataStore:            metadataStore,
		recoveredWarningProvider: options.RecoveredWarningProvider,
		authority:                authority,
		runtimeClientFactory:     options.RuntimeClientFactory,
		managedWorktreeBaseDir:   options.ManagedWorktreeBaseDir,
	}
}

func appendRecoveredWarning(store *session.Store, provider func() (string, bool, error)) error {
	if provider == nil {
		return nil
	}
	warning, ok, err := provider()
	if err != nil {
		return err
	}
	if !ok || warning == "" || store == nil {
		return nil
	}
	if store.Meta().GeneratedRecoveredWarningIssued {
		return nil
	}
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		return err
	}
	warningText := warning
	_, err = eventLog.AppendGeneratedRecoveredWarning(session.LocalEntryRecord{
		Visibility: session.EntryVisibilityOngoing,
		Role:       "warning",
		Text:       &warningText,
	})
	return err
}

func applyAgentSelection(store *session.Store, target *session.ChatSettingsState) (bool, error) {
	if target == nil {
		return false, nil
	}
	result, err := store.CommitChatSettingsState(*target)
	return result.Changed, err
}

func (s *API) ActivateSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeActivateRequest) (err error) {
	var target serverapi.SessionTarget = serverapi.UnresolvedSessionTarget{}
	defer func() { err = serverapi.WithSessionTarget(target, err) }()
	if err := req.Validate(); err != nil {
		return err
	}
	if s == nil || s.authority == nil {
		return errors.New("session runtime authority is required")
	}
	sessionID, err := s.metadataStore.ResolveSessionSelector(ctx, req.SessionID)
	if err != nil {
		return err
	}
	target = serverapi.CanonicalSessionTarget{ID: sessionID}
	identity, err := s.metadataStore.SessionIdentity(ctx, sessionID)
	if err != nil {
		return err
	}
	target = serverapi.ProjectedSessionTarget{Identity: identity}
	resource, err := s.authority.openRuntime(ctx, RuntimeOpenRequest{
		SessionID: sessionID,
	}, func(activationCtx context.Context, store *session.Store) (*AgentRuntimePlan, AgentResourceSelection, error) {
		if _, err := store.PromoteSubagentToMain(); err != nil {
			return nil, nil, err
		}
		agentSelectionChanged := false
		if req.AgentSelection != nil {
			target, targetErr := session.ChatSettingsStateFromRole(
				req.AgentSelection.AgentRole,
				session.ChatSettings{
					Supervisor:     req.AgentSelection.Baseline.Supervisor,
					Thinking:       req.AgentSelection.Baseline.Thinking,
					Fast:           req.AgentSelection.Baseline.Fast,
					Questions:      req.AgentSelection.Baseline.Questions,
					AutoCompaction: req.AgentSelection.Baseline.AutoCompaction,
				},
			)
			if targetErr != nil {
				return nil, nil, targetErr
			}
			target.ConnectionID = store.Meta().ConnectionID
			agentSelectionChanged, targetErr = applyAgentSelection(store, &target)
			if targetErr != nil {
				return nil, nil, targetErr
			}
		}
		persisted := store.Meta().ChatSettings
		if persisted != nil && req.ThinkingOverrideExplicit {
			cloned := *persisted
			cloned.Thinking = nil
			persisted = &cloned
		}
		current := &session.ChatSettingsOverrides{
			Supervisor:     textutil.Value(req.ActiveSettings.Reviewer.Frequency),
			Thinking:       textutil.Value(req.ActiveSettings.ThinkingLevel),
			Fast:           textutil.Value(req.ActiveSettings.PriorityRequestMode),
			Questions:      req.QuestionsEnabled,
			AutoCompaction: req.AutoCompactionEnabled,
		}
		effective, resolveErr := session.ResolveEffectiveChatSettings(
			persisted,
			current,
			session.ChatSettings{
				Supervisor:     req.ActiveSettings.Reviewer.Frequency,
				Thinking:       req.ActiveSettings.ThinkingLevel,
				Fast:           req.ActiveSettings.PriorityRequestMode,
				Questions:      *req.QuestionsEnabled,
				AutoCompaction: *req.AutoCompactionEnabled,
			},
		)
		if resolveErr != nil {
			return nil, nil, resolveErr
		}
		req.ActiveSettings.Reviewer.Frequency = effective.Supervisor
		req.ActiveSettings.ThinkingLevel = effective.Thinking
		req.ActiveSettings.PriorityRequestMode = effective.Fast
		questions := effective.Questions
		autoCompaction := effective.AutoCompaction
		req.QuestionsEnabled = &questions
		req.AutoCompactionEnabled = &autoCompaction
		plan, planErr := s.interactiveRuntimePlan(ctx, req, sessionID.String())
		if planErr != nil {
			return nil, nil, planErr
		}
		if agentSelectionChanged {
			return &plan, ReplaceAgentResource{}, nil
		}
		return &plan, OpenAgentResource{}, nil
	})
	if err == nil && req.ThinkingOverrideExplicit {
		// The FIFO may wait for the current Step. Do not hold Session admission
		// while that Step's tools may still need it.
		return s.authority.WithRuntime(ctx, resource, func(ctx context.Context, engine *runtime.Engine) error {
			return engine.SetThinkingLevel(ctx, req.ActiveSettings.ThinkingLevel)
		})
	}
	return err
}

func (s *API) interactiveRuntimePlan(ctx context.Context, req serverapi.SessionRuntimeActivateRequest, sessionID string) (AgentRuntimePlan, error) {
	if s == nil || s.metadataStore == nil {
		return AgentRuntimePlan{}, errors.New("metadata store is required")
	}
	target, err := s.metadataStore.ResolveSessionExecutionTarget(ctx, sessionID)
	if err != nil {
		return AgentRuntimePlan{}, err
	}
	if err := context.Cause(ctx); err != nil {
		return AgentRuntimePlan{}, err
	}
	projectID, err := s.metadataStore.ResolveSessionProjectID(ctx, sessionID)
	if err != nil {
		return AgentRuntimePlan{}, err
	}
	managedWorktreeRoots, err := s.metadataStore.ListManagedWorktreeRoots(ctx)
	if err != nil {
		return AgentRuntimePlan{}, err
	}
	executionRoot := target.WorkspaceRoot
	var currentWorktreeRoot *string
	if target.Worktree != nil {
		executionRoot = target.Worktree.Root
		root := target.Worktree.Root
		currentWorktreeRoot = &root
	}
	filesystemContext, err := runtimewire.NewFilesystemContext(target.EffectiveWorkdir, executionRoot, projectID)
	if err != nil {
		return AgentRuntimePlan{}, err
	}
	enabledTools, err := parseToolIDs(req.EnabledToolIDs)
	if err != nil {
		return AgentRuntimePlan{}, err
	}
	startLogLines := []string{
		fmt.Sprintf(
			"app.interactive.start session_id=%s workspace=%s workdir=%s model=%s",
			sessionID,
			target.WorkspaceRoot,
			target.EffectiveWorkdir,
			req.ActiveSettings.Model,
		),
		fmt.Sprintf(
			"config.settings files=%+v created=%t",
			req.Source.Files,
			req.Source.CreatedDefaultConfig,
		),
	}
	for _, line := range runlog.FormatConfigSourceLines(req.Source.Sources) {
		startLogLines = append(startLogLines, "config.source "+line)
	}
	var managedWorktreePathContext *tools.ManagedWorktreePathContext
	if strings.TrimSpace(s.managedWorktreeBaseDir) != "" {
		managedWorktreePathContext, err = tools.NewManagedWorktreePathContext(s.managedWorktreeBaseDir, currentWorktreeRoot, managedWorktreeRoots)
		if err != nil {
			return AgentRuntimePlan{}, err
		}
	}
	return NewAgentRuntimePlan(AgentRuntimePlanOptions{
		MainWorkspaceRoot:        target.WorkspaceRoot,
		Settings:                 req.ActiveSettings,
		EnabledTools:             enabledTools,
		FilesystemContext:        tools.FilesystemContext{Access: filesystemContext.Access, ManagedWorktree: managedWorktreePathContext},
		Sources:                  req.Source.Sources,
		ExplicitToolSelection:    req.ExplicitToolSelection,
		QuestionsEnabled:         req.QuestionsEnabled,
		AutoCompactionEnabled:    req.AutoCompactionEnabled,
		ClientFactory:            s.runtimeClientFactory,
		StartLogLines:            startLogLines,
		RecoveredWarningProvider: s.recoveredWarningProvider,
	})
}

var errUnknownToolID = errors.New("unknown tool id")

func parseToolIDs(raw []string) ([]toolspec.ID, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	ids := make([]toolspec.ID, 0, len(raw))
	for _, item := range raw {
		id, ok := toolspec.ParseID(item)
		if !ok {
			return nil, fmt.Errorf("%w %q", errUnknownToolID, item)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func runtimeUnavailableErr(sessionID string) error {
	return errors.Join(serverapi.ErrRuntimeUnavailable, fmt.Errorf("session %q has no active runtime available", strings.TrimSpace(sessionID)))
}

var _ servicecontract.SessionRuntimeService = (*API)(nil)
