package workflowrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"core/prompts"
	"core/server/auth"
	"core/server/launch"
	"core/server/llm"
	"core/server/metadata"
	"core/server/runtime"
	"core/server/runtimewire"
	"core/server/session"
	"core/server/sessionruntime"
	askquestion "core/server/tools"
	"core/server/workflow"
	"core/server/workflowattention"
	"core/server/workflowexecution"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/server/worktree"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"

	"github.com/google/uuid"
)

const (
	ReasonRuntimeFailed = "workflow_runtime_failed"
)

type RuntimeStore interface {
	ResolveCurrentNodeStartContext(context.Context, workflow.CurrentNodeReference) (workflowstore.CurrentNodeStartContext, error)
	ValidateCurrentNodeSessionBinding(context.Context, runtimeids.SessionID, workflow.CurrentNodeReference) error
	CountTaskComments(context.Context, workflow.TaskID) (int64, error)
}

type WorkflowAttentionRegistry interface {
	workflowattention.QuestionAttentionRegistry
	workflowattention.ApprovalQuestionAttentionRegistry
}

type Starter struct {
	cfg                  config.App
	metadata             *metadata.Store
	store                RuntimeStore
	authManager          *auth.Manager
	attention            WorkflowAttentionRegistry
	runtimeAuthority     *sessionruntime.Authority
	storeOptions         []session.StoreOption
	runtimeClientFactory runtimewire.RuntimeClientFactory
	taskAwarenessSource  workflowruntime.TaskAwarenessSource
	closed               atomic.Bool
}

type StarterOptions struct {
	RuntimeClientFactory runtimewire.RuntimeClientFactory
	RuntimeAuthority     *sessionruntime.Authority
	TaskDependencies     TaskDependencyCounter
}

func NewStarter(cfg config.App, metadataStore *metadata.Store, store RuntimeStore, authManager *auth.Manager, attention WorkflowAttentionRegistry, opts StarterOptions) (*Starter, error) {
	if strings.TrimSpace(cfg.PersistenceRoot) == "" {
		return nil, errors.New("workflow runtime persistence root is required")
	}
	if metadataStore == nil || store == nil || opts.RuntimeAuthority == nil || opts.TaskDependencies == nil {
		return nil, errors.New("workflow runtime dependencies are required")
	}
	taskAwarenessSource, err := NewTaskAwarenessSource(store, opts.TaskDependencies)
	if err != nil {
		return nil, err
	}
	return &Starter{
		cfg:                  cfg,
		metadata:             metadataStore,
		store:                store,
		authManager:          authManager,
		attention:            attention,
		runtimeAuthority:     opts.RuntimeAuthority,
		storeOptions:         metadataStore.AuthoritativeSessionStoreOptions(),
		runtimeClientFactory: opts.RuntimeClientFactory,
		taskAwarenessSource:  taskAwarenessSource,
	}, nil
}

type currentNodeAgentAssignmentSteer struct {
	reference  workflow.CurrentNodeReference
	input      workflowstore.CurrentNodeStartContext
	prepared   preparedCurrentNodeAgentSession
	starter    *Starter
	assignment runtime.WorkflowAssignment
	ready      chan struct{}
	mu         sync.Mutex
	started    bool
	receipt    session.CommitReceipt
	err        error
	planned    plannedCurrentNodeSession
	delivery   workflowruntime.TaskPromptDelivery
}

func (s *currentNodeAgentAssignmentSteer) Prepare(ctx context.Context) error {
	if s == nil {
		return errors.New("current node agent assignment steer is required")
	}
	selection, err := currentNodeAgentExecutionSelection(s.input)
	if err != nil {
		return err
	}
	thinkingMutation := workflowThinkingMutationFor(s.input, selection)
	s.mu.Lock()
	if s.started {
		ready := s.ready
		s.mu.Unlock()
		select {
		case <-ready:
			return s.err
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	s.started = true
	s.mu.Unlock()

	err = s.planned.materialize(ctx, s.starter, s.input, &s.prepared)
	var steer runtime.WorkflowAssignmentSteer
	var admission sessionruntime.DormantSessionStoreAdmission
	if err == nil {
		admission, err = s.starter.runtimeAuthority.WithDormantSessionStore(ctx, s.prepared.plan.Descriptor, func(_ context.Context, store *session.Store) error {
			var steerErr error
			persistenceContext := s.starter.workflowAssignmentPersistenceContext(s.prepared)
			persistenceContext.ThinkingMutation = thinkingMutation
			if s.delivery == workflowruntime.TaskPromptDeliveryResume {
				steer, steerErr = runtime.SteerPersistedWorkflowAssignmentForResume(store, s.assignment, persistenceContext)
			} else {
				steer, steerErr = runtime.SteerPersistedWorkflowAssignment(store, s.assignment, persistenceContext)
			}
			return steerErr
		})
	}
	if err == nil && admission.RuntimeAvailable {
		err = s.starter.runtimeAuthority.WithCurrentRuntime(ctx, s.prepared.plan.Descriptor.SessionID(), func(_ context.Context, engine *runtime.Engine) error {
			if s.delivery == workflowruntime.TaskPromptDeliveryResume {
				var steerErr error
				steer, steerErr = engine.SteerWorkflowAssignmentResume(ctx, s.assignment)
				return steerErr
			}
			snapshot, snapshotErr := runtime.NewWorkflowAssignmentSnapshot(s.assignment)
			if snapshotErr != nil {
				return snapshotErr
			}
			snapshot = snapshot.WithThinkingMutation(thinkingMutation)
			var steerErr error
			steer, steerErr = engine.SteerWorkflowAssignmentSnapshot(snapshot)
			return steerErr
		})
	}
	var receipt session.CommitReceipt
	if err == nil {
		receipt, err = steer.Wait(ctx)
	}
	s.mu.Lock()
	s.receipt = receipt
	s.err = err
	close(s.ready)
	s.mu.Unlock()
	return err
}

func (s *Starter) workflowAssignmentPersistenceContext(
	prepared preparedCurrentNodeAgentSession,
) runtime.PersistedWorkflowAssignmentContext {
	return runtime.PersistedWorkflowAssignmentContext{
		Workdir:         prepared.root.EffectiveRoot(),
		GlobalConfigDir: s.cfg.PersistenceRoot,
		Model:           prepared.plan.ActiveSettings.Model,
		ThinkingLevel:   prepared.plan.ActiveSettings.ThinkingLevel,
		SkillPolicy:     config.ResolveSkillPolicy(prepared.plan.ActiveSettings),
		SubagentCatalog: config.App{Settings: prepared.plan.ActiveSettings, Source: prepared.plan.Source},
		EnabledTools:    workflowRuntimeEnabledTools(prepared.plan.EnabledTools),
	}
}

func (s *currentNodeAgentAssignmentSteer) Wait(ctx context.Context) (session.CommitReceipt, error) {
	if s == nil {
		return session.CommitReceipt{}, errors.New("current node agent assignment steer is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.ready:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.receipt, s.err
	case <-ctx.Done():
		return session.CommitReceipt{}, context.Cause(ctx)
	}
}

func (s *Starter) StartAgentCurrentNode(
	ctx context.Context,
	reference workflow.CurrentNodeReference,
	taskPromptDelivery workflowruntime.TaskPromptDelivery,
	assignmentSteer workflowexecution.CurrentNodeAssignmentSteer,
	onRetire func(),
	controller workflowruntime.Controller,
) (sessionruntime.ExecutionHandle, error) {
	if s == nil || s.closed.Load() {
		return nil, errors.New("workflow runtime starter closed")
	}
	assignment, ok := assignmentSteer.(*currentNodeAgentAssignmentSteer)
	if !ok || assignment == nil || assignment.starter != s || !assignment.reference.Equal(reference) {
		return nil, fmt.Errorf("current node %v received incompatible assignment %T", reference, assignmentSteer)
	}
	return s.startCurrentNodeAgent(
		ctx,
		assignment.input,
		assignment.prepared,
		taskPromptDelivery,
		onRetire,
		controller,
	)
}

func (s *Starter) currentNodeResumeUsesPreparedSession(
	ctx context.Context,
	input workflowstore.CurrentNodeStartContext,
) (bool, error) {
	if input.ContextMode != workflow.ContextModeCompactAndContinueSession ||
		input.CurrentNode.SessionID == nil {
		return true, nil
	}
	descriptor, err := session.NewOpenSessionDescriptor(*input.CurrentNode.SessionID)
	if err != nil {
		return false, err
	}
	var differentAssignment bool
	admission, err := s.runtimeAuthority.WithDormantSessionStore(
		ctx,
		descriptor,
		func(_ context.Context, store *session.Store) error {
			identity, identityErr := runtime.PersistedWorkflowAssignmentIdentity(store)
			if identityErr != nil {
				return identityErr
			}
			differentAssignment = identity != nil &&
				*identity != workflowruntime.CurrentNodePromptIdentity(input.CurrentNode.Reference)
			return nil
		},
	)
	if err != nil {
		return false, err
	}
	if admission.RuntimeAvailable {
		err = s.runtimeAuthority.WithCurrentRuntime(
			ctx,
			descriptor.SessionID(),
			func(runtimeCtx context.Context, engine *runtime.Engine) error {
				identity, identityErr := engine.ActiveWorkflowAssignmentIdentity(runtimeCtx)
				if identityErr != nil {
					return identityErr
				}
				differentAssignment = identity != nil &&
					*identity != workflowruntime.CurrentNodePromptIdentity(input.CurrentNode.Reference)
				return nil
			},
		)
		if err != nil {
			return false, err
		}
	}
	return !differentAssignment, nil
}

func (s *Starter) startCurrentNodeAgent(
	ctx context.Context,
	input workflowstore.CurrentNodeStartContext,
	prepared preparedCurrentNodeAgentSession,
	taskPromptDelivery workflowruntime.TaskPromptDelivery,
	onRetire func(),
	controller workflowruntime.Controller,
) (sessionruntime.ExecutionHandle, error) {
	reference := input.CurrentNode.Reference
	var err error
	if prepared.client == nil {
		prepared.client, err = s.newWorkflowProviderClient(ctx, prepared.plan)
		if err != nil {
			return nil, err
		}
	}
	if err := s.applyCurrentNodeSessionExecutionTarget(ctx, input, prepared.plan.Descriptor); err != nil {
		return nil, err
	}
	resource := sessionruntime.AgentResourceSelection(sessionruntime.CurrentAgentResource{})
	var replacementPlan *sessionruntime.AgentRuntimePlan
	err = s.runtimeAuthority.WithCurrentRuntime(ctx, prepared.plan.Descriptor.SessionID(), func(_ context.Context, engine *runtime.Engine) error {
		if engine.CompactionMode() != string(prepared.plan.ActiveSettings.CompactionMode) {
			resource = sessionruntime.ReplaceAgentResource{}
		}
		return nil
	})
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		resource = sessionruntime.OpenAgentResource{}
	} else if err != nil {
		return nil, err
	}
	switch resource.(type) {
	case sessionruntime.OpenAgentResource, sessionruntime.ReplaceAgentResource:
		runtimePlan, planErr := s.buildCurrentNodeAgentRuntimePlan(input, prepared)
		if planErr != nil {
			return nil, planErr
		}
		replacementPlan = &runtimePlan
	}
	runtimeConfig, err := BuildCurrentNodeRuntimeConfig(
		input,
		runtimeids.NewExecutionScopeID(),
		taskPromptDelivery,
		prepared.mode,
		s.cfg.Settings.Workflow.MaxInvalidCompletionAttempts,
		prepared.plan.ActiveSettings.Workflow.UseRequiredToolCalls,
		controller,
		s.taskAwarenessSource,
	)
	if err != nil {
		return nil, err
	}
	handle, err := s.runtimeAuthority.StartAgentExecution(
		ctx,
		sessionruntime.AgentExecutionRequest{
			Descriptor: prepared.plan.Descriptor,
			Runtime:    replacementPlan,
			Workflow: &sessionruntime.WorkflowAgentExecution{
				Reference: sessionruntime.WorkflowExecutionRef{
					ProjectID:   input.Task.ProjectID,
					WorkflowID:  input.Workflow.ID,
					CurrentNode: reference,
				},
				Config:   runtimeConfig,
				OnRetire: onRetire,
			},
			Resource: resource,
			Ask: func(askCtx context.Context, scope sessionruntime.ExecutionScope, askReq askquestion.AskQuestionRequest) (askquestion.AskQuestionResolution, error) {
				return s.handleCurrentNodeAsk(askCtx, executionPromptAwaiter{authority: s.runtimeAuthority, scope: scope}, input, prepared.plan.Descriptor.SessionID().String(), askReq)
			},
			Runner: s.currentNodeAgentRunner(input, controller),
		},
	)
	if err != nil {
		return nil, err
	}
	return handle, nil
}

func (s *Starter) currentNodeAgentAssignment(
	ctx context.Context,
	input workflowstore.CurrentNodeStartContext,
	prepared preparedCurrentNodeAgentSession,
) (runtime.WorkflowAssignment, error) {
	instructions, err := BuildCurrentSessionTaskInstructions(input)
	if err != nil {
		return runtime.WorkflowAssignment{}, err
	}
	awareness, err := s.taskAwarenessSource.TaskAwareness(ctx, input.Task.ID)
	if err != nil {
		return runtime.WorkflowAssignment{}, err
	}
	return runtime.WorkflowAssignment{
		ContextMode:    input.ContextMode,
		CompletionMode: prepared.mode,
		Prompt: workflowruntime.PromptContract{
			Identity:               workflowruntime.CurrentNodePromptIdentity(input.CurrentNode.Reference),
			CompletionMode:         prepared.mode,
			UseAutomaticToolChoice: !prepared.plan.ActiveSettings.Workflow.UseRequiredToolCalls,
			Instructions:           instructions,
			Transitions:            workflowCompletionTransitions(input.TransitionOptions, input.TransitionIDs),
			TaskAwareness:          awareness,
		},
	}, nil
}

type preparedCurrentNodeAgentSession struct {
	root   workflowstore.ExecutionRoot
	plan   launch.SessionPlan
	client llm.Client
	mode   workflowruntime.CompletionMode
}

func (s *Starter) buildCurrentNodeAgentRuntimePlan(
	input workflowstore.CurrentNodeStartContext,
	prepared preparedCurrentNodeAgentSession,
) (sessionruntime.AgentRuntimePlan, error) {
	projectWorkspaceBoundary := prepared.plan.ProjectWorkspaceBoundary.Clone()
	filesystemContext, err := runtimewire.NewFilesystemContext(prepared.root.EffectiveRoot(), prepared.root.EffectiveRoot(), projectWorkspaceBoundary)
	if err != nil {
		return sessionruntime.AgentRuntimePlan{}, err
	}
	pathContext, err := s.currentNodeManagedWorktreePathContext(prepared.plan, prepared.root)
	if err != nil {
		return sessionruntime.AgentRuntimePlan{}, err
	}
	return sessionruntime.NewAgentRuntimePlan(sessionruntime.AgentRuntimePlanOptions{
		MainWorkspaceRoot:     prepared.plan.ExecutionTarget.WorkspaceRoot,
		ExplicitToolSelection: prepared.plan.ExplicitToolSelection,
		RequiredTools:         prepared.plan.RequiredTools,
		Settings:              prepared.plan.ActiveSettings, EnabledTools: workflowRuntimeEnabledTools(prepared.plan.EnabledTools),
		FilesystemContext: askquestion.FilesystemContext{Access: filesystemContext.Access, ManagedWorktree: pathContext}, Sources: prepared.plan.Source.Sources, Headless: true, Client: prepared.client,
		QuestionsEnabled:      textutil.Value(prepared.plan.QuestionsEnabled),
		AutoCompactionEnabled: textutil.Value(prepared.plan.AutoCompactionEnabled),
		ReviewerClientFactory: s.runtimeClientFactory,
		StartLogLines:         []string{fmt.Sprintf("workflow.runtime.start task_id=%s session_id=%s node_id=%s execution_root=%s model=%s", input.Task.ID, prepared.plan.Descriptor.SessionID(), input.Node.ID, prepared.root.EffectiveRoot(), prepared.plan.ActiveSettings.Model)},
		AskQuestionBatchSkipped: func(batch askquestion.AskQuestionBatchMetadata) {
			if s.attention == nil {
				return
			}
			if err := workflowattention.PrepareSkippedTaskQuestionBatch(s.attention, currentNodeQuestionContext(input, prepared.plan.Descriptor.SessionID().String()), batch, time.Now().UTC()); err != nil {
				slog.Warn("prepare skipped current-node workflow question batch failed", "task_id", input.Task.ID, "node_id", input.Node.ID, "error", err)
			}
		},
	})
}

func (s *Starter) currentNodeAgentRunner(
	input workflowstore.CurrentNodeStartContext,
	controller workflowruntime.Controller,
) sessionruntime.AgentRunner {
	return func(runCtx context.Context, scope sessionruntime.ExecutionScope, bridge sessionruntime.AgentRuntimeBridge) error {
		var turnEngine *runtime.Engine
		var turnResult runtime.WorkflowTurnResult
		turnErr := bridge.WithEngine(runCtx, func(engineCtx context.Context, engine *runtime.Engine) error {
			turnEngine = engine
			if input.ContextMode == workflow.ContextModeCompactAndContinueSession {
				result, err := engine.SubmitWorkflowContinuationTurn(metadata.WithQueryFailureDiagnostics(engineCtx))
				turnResult = result
				if err != nil {
					return err
				}
			} else {
				result, err := engine.SubmitWorkflowTurn(metadata.WithQueryFailureDiagnostics(engineCtx))
				turnResult = result
				if err != nil {
					return err
				}
			}
			return nil
		})
		if turnResult.Completion != nil && turnEngine != nil {
			completion := *turnResult.Completion
			compactionErr := s.compactCompletedWorkflowSession(runCtx, turnEngine, completion.CommittedResult)
			continuationErr := completion.Continuation.Continue(
				context.WithoutCancel(runCtx),
				compactionErr,
			)
			if completion.Diagnostic != nil {
				slog.Error(
					"Workflow completion committed with a diagnostic",
					"task_id", input.Task.ID,
					"node_id", input.Node.ID,
					"session_id", turnEngine.SessionID(),
					"error", completion.Diagnostic,
				)
			}
			if postCompletionErr := errors.Join(turnErr, compactionErr, continuationErr); postCompletionErr != nil {
				slog.Error(
					"finish accepted Workflow completion",
					"task_id", input.Task.ID,
					"node_id", input.Node.ID,
					"session_id", turnEngine.SessionID(),
					"error", postCompletionErr,
				)
			}
			return nil
		}
		if turnErr == nil {
			turnErr = errors.New("workflow Agent execution ended without a completion outcome")
			return errors.Join(
				turnErr,
				s.failCurrentNodeScope(
					context.WithoutCancel(runCtx),
					controller,
					scope,
					"workflow_runtime_finalized_without_outcome",
					turnErr,
				),
			)
		}
		reason := ReasonRuntimeFailed
		if errors.Is(turnErr, context.Canceled) || context.Cause(runCtx) != nil {
			reason = string(workflow.CurrentNodeInterruptionReasonRuntimeCanceled)
		}
		return errors.Join(turnErr, s.failCurrentNodeScope(context.WithoutCancel(runCtx), controller, scope, reason, turnErr))
	}
}

func (s *Starter) compactCompletedWorkflowSession(
	ctx context.Context,
	engine *runtime.Engine,
	completed workflowstore.CurrentNodeCompletionResult,
) error {
	if engine == nil || !completed.PostCompletionEligible {
		return nil
	}
	for _, intent := range completed.AutomaticIntents {
		if intent.NodeKind != workflow.NodeKindAgent {
			continue
		}
		target, err := s.store.ResolveCurrentNodeStartContext(ctx, intent.CurrentNode)
		if err != nil {
			return err
		}
		if target.ContextMode != workflow.ContextModeCompactAndContinueSession ||
			target.CurrentNode.SessionID == nil || target.CurrentNode.SessionID.String() != engine.SessionID() {
			continue
		}
		policy, err := resolveCurrentNodeSessionPolicy(target)
		if err != nil {
			return err
		}
		if !policy.cloneRetainedSession {
			if err := engine.CompactContextForWorkflowContinuation(ctx); err != nil {
				return workflowexecution.NewTaskStartPreparationError(err, workflow.NewCurrentNodeInterruptionDetail("workflow_compaction_failed", err))
			}
			return nil
		}
	}
	if engine.CompactionMode() == "none" {
		return nil
	}
	shouldCompact := completed.SessionReuseClassification == workflow.SessionReuseGuaranteedCACReuse
	if completed.SessionReuseClassification == workflow.SessionReuseThresholdPossibleReuse {
		threshold, err := engine.WorkflowPreCompactionTokenLimit()
		if err != nil {
			return err
		}
		shouldCompact = engine.ContextUsage().UsedTokens >= threshold
	}
	if !shouldCompact {
		return nil
	}
	_, err := engine.CompactContextForWorkflowPostCompletion(ctx)
	return err
}

func (s *Starter) compactOutgoingCurrentNodeSession(ctx context.Context, input workflowstore.CurrentNodeStartContext, root workflowstore.ExecutionRoot, plan launch.SessionPlan) (resultErr error) {
	plan.ExplicitToolSelection = nil
	required := true
	err := s.runtimeAuthority.WithCurrentRuntime(ctx, plan.Descriptor.SessionID(), func(_ context.Context, engine *runtime.Engine) error {
		required = engine.WorkflowContinuationCompactionRequired()
		return nil
	})
	if err == nil && !required {
		return nil
	}
	if err != nil && !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return err
	}
	client, err := s.newWorkflowProviderClient(ctx, plan)
	if err != nil {
		return err
	}
	runtimePlan, err := s.buildCurrentNodeAgentRuntimePlan(input, preparedCurrentNodeAgentSession{root: root, plan: plan, client: client})
	if err != nil {
		return err
	}
	attachment, err := s.runtimeAuthority.OpenRuntime(ctx, sessionruntime.RuntimeOpenRequest{
		SessionID: plan.Descriptor.SessionID(), OwnerID: uuid.NewString(), Runtime: &runtimePlan,
	})
	if err != nil {
		return err
	}
	defer func() {
		_, releaseErr := attachment.Release(context.WithoutCancel(ctx), sessionruntime.RuntimeReleaseCloseIfIdle)
		resultErr = errors.Join(resultErr, releaseErr)
	}()
	required = false
	var resource sessionruntime.AgentResourceSelection = sessionruntime.CurrentAgentResource{}
	err = s.runtimeAuthority.WithRuntime(ctx, attachment.Resource(), func(_ context.Context, engine *runtime.Engine) error {
		required = engine.WorkflowContinuationCompactionRequired()
		if engine.CompactionMode() != string(plan.ActiveSettings.CompactionMode) {
			resource = sessionruntime.ReplaceAgentResource{}
		}
		return nil
	})
	if err != nil || !required {
		return err
	}
	handle, err := s.runtimeAuthority.StartAgentExecution(ctx, sessionruntime.AgentExecutionRequest{
		Descriptor: plan.Descriptor,
		Resource:   resource,
		Runtime:    &runtimePlan,
		Workflow: &sessionruntime.WorkflowAgentExecution{
			Reference: sessionruntime.WorkflowExecutionRef{ProjectID: input.Task.ProjectID, WorkflowID: input.Workflow.ID, CurrentNode: input.CurrentNode.Reference},
		},
		Runner: func(runCtx context.Context, _ sessionruntime.ExecutionScope, bridge sessionruntime.AgentRuntimeBridge) error {
			return bridge.WithEngine(runCtx, func(engineCtx context.Context, engine *runtime.Engine) error {
				return engine.CompactContextForWorkflowContinuation(engineCtx)
			})
		},
	})
	if err != nil {
		return err
	}
	_, err = handle.Wait(context.WithoutCancel(ctx))
	if err != nil {
		return workflowexecution.NewTaskStartPreparationError(err, workflow.NewCurrentNodeInterruptionDetail("workflow_compaction_failed", err))
	}
	return err
}

type currentNodeSessionAssigneePolicy = workflow.AssigneeSessionPolicy

const (
	currentNodeSessionAssigneeEstablishTarget = workflow.AssigneeSessionPolicyEstablishTarget
	currentNodeSessionAssigneePreserve        = workflow.AssigneeSessionPolicyPreserve
)

type currentNodeSessionPolicy struct {
	cloneRetainedSession bool
	assignee             currentNodeSessionAssigneePolicy
}

func resolveCurrentNodeSessionPolicy(input workflowstore.CurrentNodeStartContext) (currentNodeSessionPolicy, error) {
	source := workflow.CanonicalContextSource(input.EnteringEdge.ContextSource)
	targetOwned := false
	switch source.Kind {
	case workflow.ContextSourceImmediateSource, workflow.ContextSourceSelectedNode:
	case workflow.ContextSourcePreviousTarget, workflow.ContextSourcePreviousTargetOrNew:
		targetOwned = true
	default:
		return currentNodeSessionPolicy{}, fmt.Errorf("current node session policy does not support context source %q", source.Kind)
	}
	assignee, err := workflow.ResolveAssigneeSessionPolicy(workflow.AssigneeSessionPolicyRequest{
		ContextMode:           input.ContextMode,
		ContextSource:         source,
		TargetSessionResolved: input.CurrentNode.SessionID != nil,
	})
	if err != nil {
		return currentNodeSessionPolicy{}, err
	}
	return currentNodeSessionPolicy{
		cloneRetainedSession: input.IsFanoutBranch && !targetOwned && input.ContextMode != workflow.ContextModeNewSession,
		assignee:             assignee,
	}, nil
}

func (s *Starter) currentNodeWorktreeReminder(ctx context.Context, input workflowstore.CurrentNodeStartContext) (*session.WorktreeReminderState, error) {
	root, err := requireCurrentNodeExecutionRoot(input)
	if err != nil {
		return nil, err
	}
	if root.Managed == nil {
		return nil, nil
	}
	record, err := s.metadata.GetWorktreeRecordByID(ctx, root.Managed.WorktreeID)
	if err != nil {
		return nil, err
	}
	worktreeContext, err := worktree.ContextFromRecord(record, root.SourceWorkspaceRoot, root.EffectiveRoot())
	if err != nil {
		return nil, err
	}
	return &session.WorktreeReminderState{
		Mode:            session.WorktreeReminderModeEnter,
		WorktreeContext: worktreeContext,
	}, nil
}

func (s *Starter) applyCurrentNodeSessionExecutionTarget(ctx context.Context, input workflowstore.CurrentNodeStartContext, descriptor session.SessionDescriptor) error {
	root, err := requireCurrentNodeExecutionRoot(input)
	if err != nil {
		return err
	}
	update := currentNodeSessionExecutionTargetUpdate(root, descriptor.SessionID())
	update.WorktreeReminder, err = s.currentNodeWorktreeReminder(ctx, input)
	if err != nil {
		return err
	}
	target, err := s.metadata.ResolveSessionExecutionTarget(ctx, descriptor.SessionID().String())
	if err != nil {
		return err
	}
	return s.runtimeAuthority.SyncExecutionTarget(ctx, descriptor.SessionID().String(), target, update.WorktreeReminder)
}

func (s *Starter) currentNodeManagedWorktreePathContext(plan launch.SessionPlan, root workflowstore.ExecutionRoot) (*askquestion.ManagedWorktreePathContext, error) {
	if strings.TrimSpace(s.cfg.Settings.Worktrees.BaseDir) == "" {
		return nil, nil
	}
	var currentRoot *string
	if root.Managed != nil {
		currentRoot = &root.Managed.Root
	}
	return askquestion.NewManagedWorktreePathContext(s.cfg.Settings.Worktrees.BaseDir, currentRoot, plan.ManagedWorktreeRoots)
}

func workflowSessionNameFromCurrentNode(input workflowstore.CurrentNodeStartContext) (string, error) {
	taskID := input.Task.ShortID
	if taskID == "" {
		taskID = string(input.Task.ID)
	}
	if taskID == "" || input.AcceptedTransitionPath.SourceNodeDisplayName == "" || input.AcceptedTransitionPath.TargetNodeDisplayName == "" {
		return "", errors.New("current workflow session metadata is incomplete")
	}
	return fmt.Sprintf("%s: %s -> %s", taskID, input.AcceptedTransitionPath.SourceNodeDisplayName, input.AcceptedTransitionPath.TargetNodeDisplayName), nil
}

func renderCurrentNodePrompt(text string, input workflowstore.CurrentNodeStartContext) (string, error) {
	source := ""
	if input.SourceSessionID != nil {
		source = input.SourceSessionID.String()
	}
	promptSession := ""
	if input.PromptSessionID != nil {
		promptSession = input.PromptSessionID.String()
	}
	return renderWorkflowPrompt(text, workflowPromptInput{Task: input.Task, Workflow: input.Workflow, Node: input.Node, CurrentNode: input.CurrentNode.Reference, ContextMode: input.ContextMode, SourceSessionID: source, PromptSessionID: promptSession, PriorSessionIDs: input.PriorSessionIDs, TransitionOptions: input.TransitionOptions, TransitionIDs: input.TransitionIDs, TransitionPrompt: text, ParameterValues: input.ParameterValues, PriorValues: input.CurrentNode.PriorValues})
}

func (s *Starter) resolveCurrentNodeCompletionMode(ctx context.Context, input workflowstore.CurrentNodeStartContext, plan launch.SessionPlan, client llm.Client) (workflowruntime.CompletionMode, llm.Client, error) {
	if plan.Locked != nil && plan.Locked.WorkflowCompletionMode != nil {
		mode, err := workflowruntime.ParseCompletionMode(string(*plan.Locked.WorkflowCompletionMode))
		if err != nil {
			return "", client, fmt.Errorf("parse retained Session completion mode: %w", err)
		}
		return mode, client, nil
	}
	configured := s.cfg.Settings.Workflow.CompletionMode
	if input.Node.CompletionMode != "" {
		configured = config.WorkflowCompletionMode(input.Node.CompletionMode)
	}
	selection := workflowruntime.CompletionModeSelection{ConfiguredMode: configured, HasContinueSessionEdge: input.HasContinueSessionOutgoingEdge, ShellAvailable: toolIDEnabled(plan.EnabledTools, toolspec.ToolExecCommand)}
	if workflowCompletionModeNeedsProviderCapabilities(selection) {
		caps, resolved, err := s.workflowProviderCapabilities(ctx, plan, client)
		if err != nil {
			return "", resolved, err
		}
		selection.ProviderCapabilities, client = caps, resolved
	}
	mode, err := workflowruntime.SelectCompletionMode(selection)
	if err != nil {
		return "", client, err
	}
	return mode, client, nil
}

func (s *Starter) withSessionStore(ctx context.Context, descriptor session.SessionDescriptor, callback func(context.Context, *session.Store) error) error {
	return s.runtimeAuthority.WithSessionStore(ctx, descriptor, callback)
}

func (s *Starter) Close() error {
	if s == nil || s.closed.Swap(true) {
		return nil
	}
	return s.runtimeAuthority.StopWorkflowExecutions(context.Background())
}

func workflowCompletionModeNeedsProviderCapabilities(selection workflowruntime.CompletionModeSelection) bool {
	return selection.ConfiguredMode == config.WorkflowCompletionModeStructuredOutput || ((selection.ConfiguredMode == config.WorkflowCompletionModeAuto || selection.ConfiguredMode == "") && selection.ShellAvailable && !selection.HasContinueSessionEdge)
}

func (s *Starter) workflowProviderCapabilities(ctx context.Context, plan launch.SessionPlan, client llm.Client) (llm.ProviderCapabilities, llm.Client, error) {
	if caps, ok := llm.ProviderCapabilitiesFromLockedOrOverride(plan.Locked, plan.ActiveSettings.ProviderCapabilities); ok {
		return caps, client, nil
	}
	if client == nil {
		next, err := s.newWorkflowProviderClient(ctx, plan)
		if err != nil {
			return llm.ProviderCapabilities{}, nil, err
		}
		client = next
	}
	provider, ok := client.(llm.ProviderCapabilitiesClient)
	if !ok {
		return llm.ProviderCapabilities{}, client, fmt.Errorf("provider capabilities are unavailable for client %T", client)
	}
	caps, err := provider.ProviderCapabilities(ctx)
	return caps, client, err
}

func (s *Starter) newWorkflowProviderClient(ctx context.Context, plan launch.SessionPlan) (llm.Client, error) {
	active := plan.ActiveSettings
	if s.runtimeClientFactory != nil {
		providerSettings := runtimewire.RuntimeClientProviderSettings{
			Model:               active.Model,
			ProviderOverride:    active.ProviderOverride,
			OpenAIBaseURL:       active.OpenAIBaseURL,
			ModelVerbosity:      active.ModelVerbosity,
			ProviderIdentifier:  active.ProviderIdentifier,
			Store:               active.Store,
			ContextWindowTokens: active.ModelContextWindow,
			Auth:                "inherit",
		}
		if caps, configured := llm.ProviderCapabilitiesFromLockedOrOverride(plan.Locked, active.ProviderCapabilities); configured {
			providerSettings.ProviderCapabilitiesOverride = &caps
		}
		client, err := s.runtimeClientFactory.NewRuntimeClient(ctx, runtimewire.RuntimeClientRequest{
			Purpose:          runtimewire.RuntimeClientPurposeWorkflow,
			SessionID:        plan.Descriptor.SessionID().String(),
			ActiveSettings:   active,
			EnabledTools:     append([]toolspec.ID(nil), plan.EnabledTools...),
			Sources:          maps.Clone(plan.Source.Sources),
			ProviderSettings: providerSettings,
		})
		if err != nil {
			return nil, err
		}
		if client == nil {
			return nil, errors.New("runtime client factory returned nil workflow client")
		}
		return client, nil
	}
	var authProvider llm.AuthHeaderProvider
	if s.authManager != nil {
		authProvider = s.authManager
	}
	return llm.NewProviderClient(llm.ProviderClientOptions{Provider: llm.Provider(active.ProviderOverride), Model: active.Model, Auth: authProvider, HTTPClient: llm.NewHTTPClient(time.Duration(active.Timeouts.ModelRequestSeconds) * time.Second), OpenAIBaseURL: active.OpenAIBaseURL, ModelVerbosity: string(active.ModelVerbosity), ProviderIdentifier: &active.ProviderIdentifier, Store: active.Store, ContextWindowTokens: active.ModelContextWindow})
}

func workflowPromptOverrides(role string) serverapi.RunPromptOverrides {
	if workflow.IsDefaultAgentRole(role) {
		role = workflow.DefaultAgentRole
	}
	if strings.TrimSpace(role) == "" {
		return serverapi.RunPromptOverrides{}
	}
	return serverapi.RunPromptOverrides{AgentRole: &role}
}

func (s *Starter) validateRole(role string) error {
	if workflow.IsDefaultAgentRole(role) || config.LookupSubagentRole(s.cfg.Settings, strings.TrimSpace(role)).Status == config.SubagentRoleLookupPresent {
		return nil
	}
	return fmt.Errorf("workflow validation failed: [%s]", workflow.CodeAgentRoleMissing)
}

func currentNodeAgentExecutionSelection(input workflowstore.CurrentNodeStartContext) (workflow.AgentExecutionSelection, error) {
	if input.CurrentNode.AgentExecutionSelection == nil {
		return workflow.AgentExecutionSelection{}, errors.New("Agent Current Node execution selection is required")
	}
	selection := input.CurrentNode.AgentExecutionSelection.Clone()
	if err := selection.Validate(); err != nil {
		return workflow.AgentExecutionSelection{}, err
	}
	return selection, nil
}

func workflowThinkingMutationFor(input workflowstore.CurrentNodeStartContext, selection workflow.AgentExecutionSelection) workflow.ThinkingMutation {
	if selection.Thinking != nil {
		return workflow.SetThinking(*selection.Thinking)
	}
	if selection.Origin == workflow.AssigneeOriginRetainedSession &&
		workflow.CanonicalThinkingSelection(input.EnteringEdge.ThinkingSelection) == workflow.ThinkingSelectionPreviousNode {
		return workflow.ClearThinking()
	}
	return workflow.KeepThinking()
}

type executionPromptAwaiter struct {
	authority *sessionruntime.Authority
	scope     sessionruntime.ExecutionScope
}

func (a executionPromptAwaiter) AwaitPromptResolution(ctx context.Context, _ string, req askquestion.AskQuestionRequest) (askquestion.AskQuestionResolution, error) {
	return a.authority.AwaitPromptResolution(ctx, a.scope.ID(), req)
}

func currentNodeQuestionContext(input workflowstore.CurrentNodeStartContext, sessionID string) workflowattention.TaskQuestionContext {
	return workflowattention.TaskQuestionContext{Task: input.Task, CurrentNode: input.CurrentNode.Reference, SessionID: sessionID}
}

func (s *Starter) handleCurrentNodeAsk(ctx context.Context, awaiter workflowattention.QuestionAwaiter, input workflowstore.CurrentNodeStartContext, sessionID string, askReq askquestion.AskQuestionRequest) (askquestion.AskQuestionResolution, error) {
	context := currentNodeQuestionContext(input, sessionID)
	if askReq.Approval {
		return workflowattention.HandleTaskApprovalQuestion(ctx, awaiter, s.attention, workflowattention.TaskQuestionRequest{Context: context, Question: askReq})
	}
	return workflowattention.HandleTaskQuestion(ctx, awaiter, s.attention, workflowattention.TaskQuestionRequest{Context: context, Question: askReq})
}

func workflowRuntimeEnabledTools(enabled []toolspec.ID) []toolspec.ID {
	return append([]toolspec.ID(nil), enabled...)
}

func toolIDEnabled(enabled []toolspec.ID, want toolspec.ID) bool {
	for _, id := range enabled {
		if id == want {
			return true
		}
	}
	return false
}

func BuildCurrentSessionTaskInstructions(input workflowstore.CurrentNodeStartContext) (workflowruntime.TaskInstructions, error) {
	source := ""
	if input.SourceSessionID != nil {
		source = input.SourceSessionID.String()
	}
	promptSession := ""
	if input.PromptSessionID != nil {
		promptSession = input.PromptSessionID.String()
	}
	return buildWorkflowTaskInstructions(workflowPromptInput{Task: input.Task, Workflow: input.Workflow, Node: input.Node, CurrentNode: input.CurrentNode.Reference, ContextMode: input.ContextMode, SourceSessionID: source, PromptSessionID: promptSession, PriorSessionIDs: input.PriorSessionIDs, TransitionOptions: input.TransitionOptions, TransitionIDs: input.TransitionIDs, TransitionPrompt: input.TransitionPrompt, ParameterValues: input.ParameterValues, PriorValues: input.CurrentNode.PriorValues})
}

type workflowPromptInput struct {
	Task              workflowstore.TaskRecord
	Workflow          workflowstore.WorkflowRecord
	Node              workflowstore.NodeRecord
	CurrentNode       workflow.CurrentNodeReference
	ContextMode       workflow.ContextMode
	SourceSessionID   string
	PromptSessionID   string
	PriorSessionIDs   map[workflow.ModelKey]*runtimeids.SessionID
	TransitionOptions []workflowstore.TransitionOption
	TransitionIDs     []string
	TransitionPrompt  string
	ParameterValues   map[string]string
	PriorValues       workflow.MaterializedPriorValues
}

func buildWorkflowTaskInstructions(input workflowPromptInput) (workflowruntime.TaskInstructions, error) {
	prompt, err := renderWorkflowPrompt(input.TransitionPrompt, input)
	if err != nil {
		return workflowruntime.TaskInstructions{}, err
	}
	shortID := input.Task.ShortID
	if shortID == "" {
		shortID = string(input.Task.ID)
	}
	return workflowruntime.TaskInstructions{CurrentNode: input.CurrentNode, TaskShortID: shortID, TaskTitle: input.Task.Title, TaskBody: input.Task.Body, WorkflowID: input.Task.WorkflowID, WorkflowName: strings.TrimSpace(input.Workflow.Name), NodeKey: string(input.Node.Key), NodeDisplayName: input.Node.DisplayName, ContextMode: string(input.ContextMode), SourceSessionID: input.SourceSessionID, Transitions: workflowInstructionTransitions(input.TransitionOptions, input.TransitionIDs), TransitionPrompt: prompt}, nil
}

func workflowTransitions(options []workflowstore.TransitionOption, ids []string) []prompts.WorkflowTransition {
	out := make([]prompts.WorkflowTransition, 0, len(options))
	for _, option := range options {
		if strings.TrimSpace(option.ID) != "" {
			out = append(out, prompts.WorkflowTransition{ID: option.ID, DisplayName: option.DisplayName, Description: option.Description})
		}
	}
	if len(out) != 0 {
		return out
	}
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			out = append(out, prompts.WorkflowTransition{ID: id})
		}
	}
	return out
}

func workflowInstructionTransitions(options []workflowstore.TransitionOption, ids []string) []workflowruntime.TransitionInstruction {
	transitions := workflowTransitions(options, ids)
	out := make([]workflowruntime.TransitionInstruction, 0, len(transitions))
	for _, transition := range transitions {
		out = append(out, workflowruntime.TransitionInstruction{ID: transition.ID, DisplayName: transition.DisplayName, Description: transition.Description})
	}
	return out
}

func workflowCompletionTransitions(options []workflowstore.TransitionOption, ids []string) []workflowruntime.CompletionTransition {
	out := make([]workflowruntime.CompletionTransition, 0, len(options))
	for _, option := range options {
		if strings.TrimSpace(option.ID) != "" {
			out = append(out, workflowruntime.CompletionTransition{ID: option.ID, DisplayName: option.DisplayName, Description: option.Description, Parameters: append([]workflow.Parameter(nil), option.Parameters...)})
		}
	}
	if len(out) != 0 {
		return out
	}
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			out = append(out, workflowruntime.CompletionTransition{ID: id})
		}
	}
	return out
}

type nodePromptTemplateData struct {
	TaskId, TaskShortId, TaskTitle, TaskBody, NodeId, NodeKey, NodeDisplayName, SessionId string
	Params                                                                                map[string]promptParameterNamespace
}

const currentParameterValueKey = "\x00current"

type promptParameterNamespace map[string]string

func (n promptParameterNamespace) String() string { return n[currentParameterValueKey] }

func renderWorkflowPrompt(text string, input workflowPromptInput) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	tmpl, err := template.New("workflow transition prompt").Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("parse workflow transition prompt template: %w", err)
	}
	var out strings.Builder
	err = tmpl.Execute(&out, nodePromptTemplateData{
		TaskId:          string(input.Task.ID),
		TaskShortId:     input.Task.ShortID,
		TaskTitle:       input.Task.Title,
		TaskBody:        input.Task.Body,
		NodeId:          string(input.Node.ID),
		NodeKey:         string(input.Node.Key),
		NodeDisplayName: input.Node.DisplayName,
		SessionId:       input.PromptSessionID,
		Params:          promptParameterData(input.ParameterValues, input.PriorValues.TransitionParameters, input.PriorSessionIDs),
	})
	return out.String(), err
}

func promptParameterData(
	current map[string]string,
	prior map[workflow.ModelKey]map[string]string,
	priorSessionIDs map[workflow.ModelKey]*runtimeids.SessionID,
) map[string]promptParameterNamespace {
	out := map[string]promptParameterNamespace{workflow.RuntimePromptParameterCommentary: {currentParameterValueKey: ""}}
	for transition, values := range prior {
		namespace := out[string(transition)]
		if namespace == nil {
			namespace = promptParameterNamespace{}
		}
		for key, value := range values {
			namespace[key] = value
		}
		out[string(transition)] = namespace
	}
	for transition, sessionID := range priorSessionIDs {
		namespace := out[string(transition)]
		if namespace == nil {
			namespace = promptParameterNamespace{}
		}
		namespace[workflow.RuntimePromptParameterSessionID] = ""
		if sessionID != nil {
			namespace[workflow.RuntimePromptParameterSessionID] = sessionID.String()
		}
		out[string(transition)] = namespace
	}
	for key, value := range current {
		namespace := out[key]
		if namespace == nil {
			namespace = promptParameterNamespace{}
		}
		namespace[currentParameterValueKey] = value
		out[key] = namespace
	}
	return out
}

var _ workflowexecution.CurrentNodePublicationRunner = (*Starter)(nil)
