package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"core/prompts"
	"core/server/llm"
	"core/server/session"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type WorkflowAssignment struct {
	ContextMode    workflow.ContextMode
	CompletionMode workflowruntime.CompletionMode
	Prompt         workflowruntime.PromptContract
}

type WorkflowAssignmentSnapshot struct {
	message  *llm.Message
	thinking workflow.ThinkingMutation
}

// PersistedWorkflowAssignmentContext supplies the runtime-owned context needed
// to seed a fresh dormant Session before its first workflow assignment.
type PersistedWorkflowAssignmentContext struct {
	Workdir                 string
	GlobalConfigDir         string
	Model                   string
	ThinkingLevel           string
	ThinkingMutation        workflow.ThinkingMutation
	SkillPolicy             config.SkillPolicy
	SubagentCatalogSettings config.Settings
	EnabledTools            []toolspec.ID
}

type WorkflowAssignmentSteer struct {
	deferred runtimeDeferred[session.CommitReceipt]
}

func PersistedWorkflowAssignmentIdentity(store *session.Store) (*string, error) {
	assignment, err := store.ActiveWorkflowAssignmentProjection()
	if err != nil {
		return nil, err
	}
	if assignment == nil ||
		assignment.MessageType == nil ||
		*assignment.MessageType != session.MessageTypeWorkflowMode ||
		assignment.SourcePath == nil {
		return nil, nil
	}
	identity := strings.TrimSpace(*assignment.SourcePath)
	if identity == "" {
		return nil, nil
	}
	return &identity, nil
}

func (e *Engine) ActiveWorkflowAssignmentIdentity(ctx context.Context) (*string, error) {
	return awaitEngineRuntimeOperation(
		ctx,
		e,
		func(context.Context) (*string, error) {
			return workflowAssignmentIdentityFromItems(e.transcriptRuntimeState().SnapshotItems()), nil
		},
	)
}

func newWorkflowAssignmentSteer() WorkflowAssignmentSteer {
	return WorkflowAssignmentSteer{deferred: newRuntimeDeferred[session.CommitReceipt]()}
}

func CompletedWorkflowAssignmentSteer(receipt session.CommitReceipt, err error) WorkflowAssignmentSteer {
	steer := newWorkflowAssignmentSteer()
	steer.complete(receipt, err)
	return steer
}

func (s WorkflowAssignmentSteer) Wait(ctx context.Context) (session.CommitReceipt, error) {
	if s.deferred.state == nil {
		return session.CommitReceipt{}, errors.New("workflow assignment steer is uninitialized")
	}
	return s.deferred.Await(ctx)
}

func (s WorkflowAssignmentSteer) complete(receipt session.CommitReceipt, err error) {
	if err == nil && !receipt.Committed {
		err = errors.New("workflow assignment message was not committed")
	}
	s.deferred.complete(receipt, err)
}

func (e *Engine) SteerWorkflowAssignment(assignment WorkflowAssignment) (WorkflowAssignmentSteer, error) {
	snapshot, err := NewWorkflowAssignmentSnapshot(assignment)
	if err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	return e.SteerWorkflowAssignmentSnapshot(snapshot)
}

func NewWorkflowAssignmentSnapshot(assignment WorkflowAssignment) (WorkflowAssignmentSnapshot, error) {
	message, err := buildWorkflowAssignmentMessage(assignment)
	if err != nil {
		return WorkflowAssignmentSnapshot{}, err
	}
	return WorkflowAssignmentSnapshot{message: &message}, nil
}

func (e *Engine) SteerWorkflowAssignmentSnapshot(snapshot WorkflowAssignmentSnapshot) (WorkflowAssignmentSteer, error) {
	if err := validateWorkflowAssignmentSnapshot(snapshot); err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	return e.steerWorkflowAssignmentSnapshot(snapshot)
}

type workflowAssignmentPromptSelection struct {
	kind   prompts.WorkflowTaskPromptKind
	inject bool
}

func (e *Engine) SteerWorkflowAssignmentResume(
	ctx context.Context,
	assignment WorkflowAssignment,
) (WorkflowAssignmentSteer, error) {
	if e == nil || e.closed.Load() {
		return WorkflowAssignmentSteer{}, ErrEngineClosed
	}
	deferred := submitEngineRuntimeOperation(e, func(operationCtx context.Context) (session.CommitReceipt, error) {
		currentIdentity := workflowAssignmentIdentityFromItems(e.transcriptRuntimeState().SnapshotItems())
		kind, inject, err := selectWorkflowTaskPrompt(
			currentIdentity,
			assignment.Prompt.Identity,
			workflowTaskPromptTriggerResumeDelivery,
		)
		if err != nil {
			return session.CommitReceipt{}, err
		}
		return e.steerWorkflowAssignmentSelection(operationCtx, assignment, workflowAssignmentPromptSelection{
			kind:   kind,
			inject: inject,
		})
	})
	return WorkflowAssignmentSteer{deferred: deferred}, nil
}

func (e *Engine) steerWorkflowAssignmentSelection(
	ctx context.Context,
	assignment WorkflowAssignment,
	selection workflowAssignmentPromptSelection,
) (session.CommitReceipt, error) {
	if err := context.Cause(ctx); err != nil {
		return session.CommitReceipt{}, err
	}
	if !selection.inject {
		return session.CommitReceipt{Committed: true}, nil
	}
	message, err := buildWorkflowAssignmentMessageForKind(assignment, selection.kind)
	if err != nil {
		return session.CommitReceipt{}, err
	}
	return e.steerPreparedWorkflowMessage(message)
}

func (e *Engine) steerWorkflowAssignmentMessage(message llm.Message) (session.CommitReceipt, error) {
	return e.steerWithCommitReceiptRaw(sessionSteeringProvenance(), steerMessagesWithPersistenceIntent(
		steeringPriorityRuntimeContext,
		steeringMessageEventDefault,
		true,
		[]llm.Message{message},
	))
}

func (e *Engine) steerPreparedWorkflowMessage(message llm.Message) (session.CommitReceipt, error) {
	if latestActiveMetaContextMatches(
		e.transcriptRuntimeState().SnapshotItems(),
		message,
	) {
		return session.CommitReceipt{Committed: true}, nil
	}
	return e.steerWorkflowAssignmentMessage(message)
}

func (e *Engine) steerWorkflowAssignmentSnapshot(snapshot WorkflowAssignmentSnapshot) (WorkflowAssignmentSteer, error) {
	if e == nil || e.closed.Load() {
		return WorkflowAssignmentSteer{}, ErrEngineClosed
	}
	intent := steerMessagesWithPersistenceIntent(
		steeringPriorityRuntimeContext,
		steeringMessageEventDefault,
		true,
		[]llm.Message{snapshot.restorationMessage()},
	)
	deferred := submitEngineRuntimeOperation(e, func(ctx context.Context) (session.CommitReceipt, error) {
		switch snapshot.thinking.Kind() {
		case workflow.ThinkingMutationSet:
			if err := e.setThinkingValue(string(snapshot.thinking.Value())); err != nil {
				return session.CommitReceipt{}, err
			}
		case workflow.ThinkingMutationClear:
			if err := e.clearThinkingValue(ctx); err != nil {
				return session.CommitReceipt{}, err
			}
		}
		return e.steerWithCommitReceiptRaw(sessionSteeringProvenance(), intent)
	})
	return WorkflowAssignmentSteer{deferred: deferred}, nil
}

func CapturePersistedWorkflowAssignment(
	store *session.Store,
) (WorkflowAssignmentSnapshot, bool, error) {
	if store == nil {
		return WorkflowAssignmentSnapshot{}, false, errors.New("session store is required")
	}
	activeAssignment, err := store.ActiveWorkflowAssignmentProjection()
	if err != nil {
		return WorkflowAssignmentSnapshot{}, false, err
	}
	snapshot := WorkflowAssignmentSnapshot{}
	if activeAssignment != nil {
		message, err := llmMessageFromSessionRecord(*activeAssignment)
		if err != nil {
			return WorkflowAssignmentSnapshot{}, false, err
		}
		snapshot.message = &message
	}
	if err := validateWorkflowAssignmentSnapshot(snapshot); err != nil {
		return WorkflowAssignmentSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s WorkflowAssignmentSnapshot) WithThinkingMutation(mutation workflow.ThinkingMutation) WorkflowAssignmentSnapshot {
	s.thinking = mutation
	return s
}

func SteerPersistedWorkflowAssignmentSnapshot(
	store *session.Store,
	snapshot WorkflowAssignmentSnapshot,
) (WorkflowAssignmentSteer, error) {
	if store == nil {
		return WorkflowAssignmentSteer{}, errors.New("session store is required")
	}
	if err := validateWorkflowAssignmentSnapshot(snapshot); err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	engine, err := newPersistedSteeringEngine(store)
	if err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	return completePersistedWorkflowAssignment(engine, snapshot.restorationMessage()), nil
}

func validateWorkflowAssignmentSnapshot(snapshot WorkflowAssignmentSnapshot) error {
	if snapshot.thinking.Kind() == workflow.ThinkingMutationSet {
		if err := snapshot.thinking.Value().Validate(); err != nil {
			return err
		}
	}
	if snapshot.message != nil &&
		(snapshot.message.MessageType == nil || *snapshot.message.MessageType != llm.MessageTypeWorkflowMode) {
		return errors.New("workflow assignment snapshot is required")
	}
	return nil
}

func (s WorkflowAssignmentSnapshot) restorationMessage() llm.Message {
	if s.message != nil {
		return *s.message
	}
	return llm.Message{
		Role:        llm.RoleDeveloper,
		MessageType: textutil.Value(llm.MessageTypeWorkflowModeExit),
		Content: textutil.Value(
			"The preceding Workflow assignment was discarded. There is no current executable Workflow Node until a later assignment arrives.",
		),
	}
}

func SteerPersistedWorkflowAssignment(
	store *session.Store,
	assignment WorkflowAssignment,
	deliveryContext PersistedWorkflowAssignmentContext,
) (WorkflowAssignmentSteer, error) {
	kind, err := workflowAssignmentPromptKind(assignment.ContextMode)
	if err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	return steerPersistedWorkflowAssignment(store, assignment, deliveryContext, kind, true)
}

func SteerPersistedWorkflowAssignmentForResume(
	store *session.Store,
	assignment WorkflowAssignment,
	deliveryContext PersistedWorkflowAssignmentContext,
) (WorkflowAssignmentSteer, error) {
	if store == nil {
		return WorkflowAssignmentSteer{}, errors.New("session store is required")
	}
	currentIdentity, err := PersistedWorkflowAssignmentIdentity(store)
	if err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	kind, inject, err := selectWorkflowTaskPrompt(
		currentIdentity,
		assignment.Prompt.Identity,
		workflowTaskPromptTriggerResumeDelivery,
	)
	if err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	return steerPersistedWorkflowAssignment(store, assignment, deliveryContext, kind, inject)
}

func steerPersistedWorkflowAssignment(
	store *session.Store,
	assignment WorkflowAssignment,
	deliveryContext PersistedWorkflowAssignmentContext,
	kind prompts.WorkflowTaskPromptKind,
	inject bool,
) (WorkflowAssignmentSteer, error) {
	if store == nil {
		return WorkflowAssignmentSteer{}, errors.New("session store is required")
	}
	engine, err := newPersistedSteeringEngine(store)
	if err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	switch deliveryContext.ThinkingMutation.Kind() {
	case workflow.ThinkingMutationSet:
		if err := deliveryContext.ThinkingMutation.Value().Validate(); err != nil {
			return WorkflowAssignmentSteer{}, err
		}
		if err := store.SetThinkingOverride(textutil.Value(string(deliveryContext.ThinkingMutation.Value()))); err != nil {
			return WorkflowAssignmentSteer{}, err
		}
	case workflow.ThinkingMutationClear:
		if err := store.SetThinkingOverride(nil); err != nil {
			return WorkflowAssignmentSteer{}, err
		}
	}
	recent, err := engine.eventLog.ReadRecentRecords(1)
	if err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	// Dormant admission must call this before appending any other event. A
	// pre-existing record is the durable witness that base meta context has
	// already been seeded; unrelated callback writes would invalidate that
	// freshness test.
	if len(recent.Records) == 0 {
		builder := newActiveMetaContextBuilder(
			engine.store.Meta(),
			deliveryContext.Workdir,
			deliveryContext.Model,
			deliveryContext.ThinkingLevel,
			deliveryContext.GlobalConfigDir,
			deliveryContext.SkillPolicy,
			time.Now(),
		).withSubagents(deliveryContext.SubagentCatalogSettings, deliveryContext.EnabledTools)
		if err := engine.steerDormantBaseMetaContext(builder, config.SubagentInvocationContextWorkflow); err != nil {
			return WorkflowAssignmentSteer{}, err
		}
	}
	if !inject {
		return CompletedWorkflowAssignmentSteer(session.CommitReceipt{Committed: true}, nil), nil
	}
	message, err := buildWorkflowAssignmentMessageForKind(assignment, kind)
	if err != nil {
		return WorkflowAssignmentSteer{}, err
	}
	return completePersistedWorkflowAssignment(engine, message), nil
}

func completePersistedWorkflowAssignment(
	engine *Engine,
	message llm.Message,
) WorkflowAssignmentSteer {
	receipt, err := engine.steerDormantWithCommitReceipt(steerMessagesWithPersistenceIntent(
		steeringPriorityRuntimeContext,
		steeringMessageEventDefault,
		true,
		[]llm.Message{message},
	))
	return CompletedWorkflowAssignmentSteer(receipt, err)
}
