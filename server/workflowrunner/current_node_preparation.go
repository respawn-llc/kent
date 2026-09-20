package workflowrunner

import (
	"context"
	"errors"
	"path/filepath"

	"core/server/launch"
	"core/server/metadata"
	"core/server/session"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
	"core/shared/toolspec"
)

// plannedCurrentNodeSession contains only private preparation. The admitted
// queue owns it after the cutover; before that, discarding it has no effects.
type plannedCurrentNodeSession struct {
	creation *session.CreationPlan
	outgoing *launch.SessionPlan
}

func (s *Starter) PrepareCurrentNode(
	ctx context.Context,
	input workflowstore.CurrentNodeStartContext,
	delivery workflowruntime.TaskPromptDelivery,
) (workflowexecution.CurrentNodePreparation, error) {
	if s.closed.Load() {
		return workflowexecution.CurrentNodePreparation{}, errors.New("workflow runtime starter closed")
	}
	if input.Node.Kind == workflow.NodeKindScript {
		_, err := currentNodeScriptCommand(input, s.cfg.PersistenceRoot)
		return workflowexecution.CurrentNodePreparation{}, err
	}
	if input.Node.Kind != workflow.NodeKindAgent {
		return workflowexecution.CurrentNodePreparation{}, errors.New("Current Node preparation requires an executable Node")
	}
	root, err := requireCurrentNodeExecutionRoot(input)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	policy, err := resolveCurrentNodeSessionPolicy(input)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	id := input.CurrentNode.SessionID
	clone := policy.cloneRetainedSession && delivery != workflowruntime.TaskPromptDeliveryResume
	fresh := id == nil || clone
	if clone {
		delivery = workflowruntime.TaskPromptDeliveryAssignment
	}
	if fresh {
		id = textutil.Value(runtimeids.NewSessionID())
	}
	update := currentNodeSessionExecutionTargetUpdate(root, *id)
	target, err := s.metadata.PrepareSessionExecutionTarget(ctx, update)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	boundary, err := s.metadata.ResolveProjectWorkspaceBoundary(ctx, input.Task.ProjectID)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	roots, err := s.metadata.ListManagedWorktreeRoots(ctx)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	execution := launch.PreparedExecutionContext{ExecutionTarget: target, ProjectWorkspaceBoundary: boundary, ManagedWorktreeRoots: roots}
	cfg := s.cfg
	cfg.WorkspaceRoot = root.SourceWorkspaceRoot
	planner := launch.Planner{
		Config: cfg, ContainerDir: filepath.Join(cfg.PersistenceRoot, "projects", input.Task.ProjectID, "sessions"),
		StoreOptions: s.storeOptions, PersistedSessions: s.metadata,
	}
	preparation := plannedCurrentNodeSession{}
	var plan launch.SessionPlan
	var meta session.Meta
	if fresh && input.CurrentNode.SessionID == nil {
		prepared, err := planner.PrepareSession(ctx, launch.SessionPreparationRequest{
			Request:   launch.SessionRequest{Mode: launch.ModeHeadless, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin())},
			SessionID: *id, ExecutionTarget: target, ProjectWorkspaceBoundary: boundary, ManagedWorktreeRoots: roots,
		})
		if err != nil {
			return workflowexecution.CurrentNodePreparation{}, err
		}
		plan, preparation.creation = prepared.Plan, &prepared.Creation
		meta = prepared.Creation.Snapshot().Meta
	} else {
		record, err := session.ResolvePersistedSessionRecord(ctx, s.metadata, input.CurrentNode.SessionID.String())
		if err != nil {
			return workflowexecution.CurrentNodePreparation{}, err
		}
		meta = *record.Meta
		if fresh {
			creation, err := s.prepareCurrentNodeClone(ctx, planner, record, *id)
			if err != nil {
				return workflowexecution.CurrentNodePreparation{}, err
			}
			preparation.creation = &creation
			meta = creation.Snapshot().Meta
		}
		plan, err = planner.PlanPreparedSession(ctx, launch.SessionRequest{
			Mode: launch.ModeHeadless, Intent: serverapi.OpenExistingSessionLaunchIntent(*id),
			SkipContinuationAgentRoleValidation: input.ContextMode == workflow.ContextModeCompactAndContinueSession,
		}, meta, execution)
		if err != nil {
			return workflowexecution.CurrentNodePreparation{}, err
		}
	}
	if input.ContextMode == workflow.ContextModeCompactAndContinueSession {
		needsCompaction := delivery != workflowruntime.TaskPromptDeliveryResume
		if !needsCompaction {
			prepared, err := s.currentNodeResumeUsesPreparedSession(ctx, input)
			if err != nil {
				return workflowexecution.CurrentNodePreparation{}, err
			}
			needsCompaction = !prepared
		}
		if needsCompaction {
			outgoing := plan
			preparation.outgoing = &outgoing
			meta = session.ProjectCompactedMeta(meta)
			plan, err = planner.PlanPreparedSession(ctx, launch.SessionRequest{
				Mode: launch.ModeHeadless, Intent: serverapi.OpenExistingSessionLaunchIntent(*id),
				SkipContinuationAgentRoleValidation: true,
			}, meta, execution)
			if err != nil {
				return workflowexecution.CurrentNodePreparation{}, err
			}
		}
	}
	selection, err := currentNodeAgentExecutionSelection(input)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	if err := s.validateRole(selection.Assignee); err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	if selection.Origin == workflow.AssigneeOriginTransitionSelected {
		plan, err = launch.WithRequiredRunPromptTools(plan, []toolspec.ID{toolspec.ToolAskQuestion})
		if err != nil {
			return workflowexecution.CurrentNodePreparation{}, err
		}
	}
	overrides := serverapi.RunPromptOverrides{}
	if policy.assignee == currentNodeSessionAssigneeEstablishTarget &&
		(delivery != workflowruntime.TaskPromptDeliveryResume || preparation.outgoing != nil) {
		overrides = workflowPromptOverrides(selection.Assignee)
	}
	overridesPlan, err := launch.PrepareRunPromptOverridesWithContext(cfg, overrides, launch.RunPromptPreparationContext{
		Mode: launch.ModeHeadless, ModelLock: meta.Locked, ToolLock: meta.Locked,
		OmittedTarget: &launch.PreparedBaseTarget{Settings: plan.ActiveSettings, Source: plan.Source, EnabledTools: plan.EnabledTools},
	})
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	plan, _, err = planner.ApplyPreparedRunPromptOverridesFromMeta(plan, meta, overrides, overridesPlan, launch.RunPromptOverrideOptions{
		RequiredTools: plan.RequiredTools, WorkflowThinking: workflowThinkingMutationFor(input, selection),
	})
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	name, err := workflowSessionNameFromCurrentNode(input)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	preview, err := renderCurrentNodePrompt(input.TransitionPrompt, input)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	reminder, err := s.currentNodeWorktreeReminder(ctx, input)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	plan.SessionName, plan.FirstPromptPreview, plan.WorktreeReminder = &name, preview, reminder
	binding := &workflowstore.PlannedCurrentNodeSession{CurrentNode: input.CurrentNode.Reference, SessionID: *id}
	if preparation.creation != nil {
		creation := *preparation.creation
		continuation := session.ContinuationContext{}
		if plan.Continuation != nil {
			continuation = *plan.Continuation
		}
		creation, err = creation.WithLaunchMetadata(plan.SessionName, continuation)
		if err == nil {
			creation, err = creation.WithListingMetadata(name, preview)
		}
		if err == nil {
			creation, err = creation.WithWorktreeReminder(reminder)
		}
		if err != nil {
			return workflowexecution.CurrentNodePreparation{}, err
		}
		preparation.creation = &creation
		snapshot, err := s.metadata.PrepareSessionSnapshot(ctx, creation.Snapshot(), update)
		if err != nil {
			return workflowexecution.CurrentNodePreparation{}, err
		}
		binding.Snapshot = &snapshot
	}
	prepared := preparedCurrentNodeAgentSession{root: root, plan: plan}
	if input.CurrentNode.Scheduling == nil {
		return workflowexecution.CurrentNodePreparation{}, errors.New("Agent preparation requires scheduling state")
	}
	prepared.client, err = s.newWorkflowProviderClient(ctx, plan)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	prepared.mode, err = s.resolveCurrentNodeCompletionMode(ctx, input, plan)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	if _, err := s.buildCurrentNodeAgentRuntimePlan(input, prepared); err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	input.CurrentNode.SessionID = id
	assignment, err := s.currentNodeAgentAssignment(ctx, input, prepared)
	if err != nil {
		return workflowexecution.CurrentNodePreparation{}, err
	}
	return workflowexecution.CurrentNodePreparation{
		Session: binding,
		Assignment: &currentNodeAgentAssignmentSteer{
			reference: input.CurrentNode.Reference, input: input, prepared: prepared,
			starter: s, assignment: assignment, ready: make(chan struct{}),
			planned: preparation, delivery: delivery,
		},
	}, nil
}

func currentNodeSessionExecutionTargetUpdate(root workflowstore.ExecutionRoot, id runtimeids.SessionID) metadata.SessionExecutionTargetUpdate {
	update := metadata.SessionExecutionTargetUpdate{
		SessionID: id.String(), Workspace: &metadata.SessionExecutionTargetUpdateWorkspace{ID: root.SourceWorkspaceID}, CwdRelpath: ".",
	}
	if root.Managed != nil {
		update.Worktree = &metadata.SessionExecutionTargetUpdateWorktree{ID: root.Managed.WorktreeID}
	}
	return update
}

func (s *Starter) prepareCurrentNodeClone(ctx context.Context, planner launch.Planner, source session.PersistedSessionRecord, id runtimeids.SessionID) (session.CreationPlan, error) {
	descriptor, err := session.NewCreateSessionDescriptor(id, planner.ContainerDir, filepath.Base(planner.ContainerDir), planner.Config.WorkspaceRoot, sessioncontract.SessionCategorySubagent)
	if err != nil {
		return session.CreationPlan{}, err
	}
	thinking, err := launch.ResolveForkThinking(s.cfg, *source.Meta, true)
	if err != nil {
		return session.CreationPlan{}, err
	}
	return session.PrepareClone(descriptor, source, "", thinking, s.storeOptions...)
}

func (p *plannedCurrentNodeSession) materialize(ctx context.Context, starter *Starter, input workflowstore.CurrentNodeStartContext, prepared *preparedCurrentNodeAgentSession) error {
	if err := starter.store.ValidateCurrentNodeSessionBinding(ctx, prepared.plan.Descriptor.SessionID(), input.CurrentNode.Reference); err != nil {
		return err
	}
	if p.creation != nil {
		if err := p.materializeCreation(ctx, starter); err != nil {
			return err
		}
	}
	if p.outgoing != nil {
		if err := starter.compactOutgoingCurrentNodeSession(ctx, input, prepared.root, *p.outgoing); err != nil {
			return err
		}
	}
	return starter.withSessionStore(ctx, prepared.plan.Descriptor, func(_ context.Context, store *session.Store) error {
		if p.outgoing != nil {
			if _, err := prepared.plan.ActiveSettings.SelectedConnection(); err != nil {
				return err
			}
			if err := store.SetConnectionID(*prepared.plan.ActiveSettings.Connection); err != nil {
				return err
			}
		}
		if prepared.plan.Locked != nil && prepared.plan.Locked.WorkflowCompletionMode == nil {
			if _, err := store.BackfillLockedWorkflowCompletionMode(prepared.mode); err != nil {
				return err
			}
		}
		if prepared.plan.Continuation != nil {
			if err := store.SetContinuationContext(*prepared.plan.Continuation); err != nil {
				return err
			}
		}
		if err := store.SetListingMetadata(*prepared.plan.SessionName, prepared.plan.FirstPromptPreview); err != nil {
			return err
		}
		return store.SetWorktreeReminderState(prepared.plan.WorktreeReminder)
	})
}

func (p *plannedCurrentNodeSession) materializeCreation(ctx context.Context, starter *Starter) error {
	if source := p.creation.SourceDescriptor(); source != nil {
		return starter.withSessionStore(ctx, *source, func(ctx context.Context, store *session.Store) error {
			log, err := store.MaterializeEventLog()
			if err != nil {
				return err
			}
			_, err = session.MaterializeCommittedClone(ctx, *p.creation, log, starter.storeOptions...)
			return err
		})
	}
	_, err := session.MaterializeCommittedCreation(ctx, *p.creation, starter.storeOptions...)
	return err
}
