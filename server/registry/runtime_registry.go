package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"core/server/attentionnotify"
	"core/server/runtime"
	"core/server/runtimeactivity"
	"core/server/runtimeview"
	"core/server/sessionruntime"
	askquestion "core/server/tools"
	shelltool "core/server/tools/shell"
	"core/shared/clientui"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type RuntimeRegistry struct {
	authorityMu                sync.Mutex
	authorityBySession         sync.Map
	authorityChanged           chan struct{}
	sleepObserverMu            sync.Mutex
	sleepObserver              func(active bool)
	runStateMu                 sync.Mutex
	blockingActivitySessions   map[string]bool
	pendingPrompts             *pendingPromptStore
	attentionBroker            *attentionnotify.Broker
	attentionNavigation        func(context.Context, string) (*sessionlaunchpb.SessionNavigationBinding, error)
	questionBatches            *attentionnotify.QuestionBatchTracker
	workflowEventPublisher     func(context.Context, serverapi.WorkflowProjectEvent) error
	executionTargetResolver    func(context.Context, string) (*worktreepb.SessionExecutionTarget, error)
	backgroundProcessSnapshots func() []shelltool.Snapshot
}

type authorityRuntimeEntry struct {
	ref         runtimeids.SessionResourceRef
	engine      *runtime.Engine
	sessionFeed *sessionFeedSequencer
	mainView    atomic.Pointer[runtimepb.MainView]

	publicationMu sync.Mutex
	mu            sync.Mutex
	lifecycle     authorityRuntimeEntryLifecycle
	feedReady     bool
}

type authorityRuntimeEntryLifecycle uint8

const (
	authorityRuntimeEntryReady authorityRuntimeEntryLifecycle = iota
	authorityRuntimeEntryDraining
	authorityRuntimeEntryRetired
)

func NewRuntimeRegistry() *RuntimeRegistry {
	return &RuntimeRegistry{
		authorityChanged:         make(chan struct{}),
		blockingActivitySessions: make(map[string]bool),
		pendingPrompts:           &pendingPromptStore{},
	}
}

func (r *RuntimeRegistry) ResourceReady(
	_ context.Context,
	resource sessionruntime.AgentResourceDescriptor,
	engine *runtime.Engine,
	retain sessionruntime.AgentResourceRetainer,
) error {
	if r == nil {
		return errors.New("runtime registry is required")
	}
	ref := resource.Ref
	if err := ref.Validate(); err != nil {
		return err
	}
	if engine == nil {
		return errors.New("authority runtime engine is required")
	}
	if retain == nil {
		return errors.New("authority runtime retainer is required")
	}
	sessionID := ref.SessionID().String()
	entry := &authorityRuntimeEntry{
		ref:         ref,
		engine:      engine,
		sessionFeed: newSessionFeedSequencer(newTranscriptSubscriptionBroker()),
	}
	r.authorityMu.Lock()
	if existing := r.authorityEntryBySession(sessionID); existing != nil {
		r.authorityMu.Unlock()
		return fmt.Errorf(
			"authority runtime resource %s generation %d cannot replace registered generation %d",
			sessionID,
			ref.Generation(),
			existing.ref.Generation(),
		)
	}
	r.authorityBySession.Store(sessionID, entry)
	r.authorityMu.Unlock()
	if err := r.publishCurrentRuntimeActivity(sessionID); err != nil {
		r.authorityMu.Lock()
		if r.authorityEntryBySession(sessionID) == entry {
			r.authorityBySession.Delete(sessionID)
			r.signalAuthorityChangeLocked()
		}
		r.authorityMu.Unlock()
		return fmt.Errorf("initialize authority runtime feed for session %s: %w", sessionID, err)
	}
	entry.mu.Lock()
	ready := entry.lifecycle == authorityRuntimeEntryReady
	if ready {
		entry.feedReady = true
	}
	entry.mu.Unlock()
	if ready {
		r.authorityMu.Lock()
		if r.authorityEntryBySession(sessionID) == entry {
			r.signalAuthorityChangeLocked()
		}
		r.authorityMu.Unlock()
	}
	return nil
}

func (r *RuntimeRegistry) ResourceDraining(_ context.Context, resource sessionruntime.AgentResourceDescriptor) error {
	if r == nil {
		return nil
	}
	ref := resource.Ref
	if err := ref.Validate(); err != nil {
		return err
	}
	entry := r.authorityEntryByRef(ref)
	if entry == nil {
		return nil
	}
	entry.publicationMu.Lock()
	entry.mu.Lock()
	if entry.lifecycle != authorityRuntimeEntryReady {
		entry.mu.Unlock()
		entry.publicationMu.Unlock()
		return nil
	}
	entry.lifecycle = authorityRuntimeEntryDraining
	entry.mu.Unlock()
	entry.mainView.Store(nil)
	entry.publicationMu.Unlock()

	sessionID := ref.SessionID().String()
	r.pendingPrompts.CloseSession(sessionID, func(snapshot PendingPromptSnapshot) {
		r.publishPromptResolution(entry, sessionID, snapshot)
	})
	_ = r.publishCurrentRuntimeActivity(sessionID)
	update, err := r.unavailableRuntimeReadModelFeedSnapshot(sessionID)
	entry.mu.Lock()
	lifecycle := entry.lifecycle
	if lifecycle != authorityRuntimeEntryDraining {
		entry.mu.Unlock()
		panic(fmt.Sprintf("authority runtime resource %s generation %d reached terminal feed closure from lifecycle %d", sessionID, ref.Generation(), lifecycle))
	}
	entry.lifecycle = authorityRuntimeEntryRetired
	entry.mu.Unlock()
	if err == nil {
		entry.sessionFeed.CloseWithRuntimeReadModel(update, io.EOF)
	} else {
		entry.sessionFeed.Close(io.EOF)
	}
	r.updateAggregateRuntimeActivityState(sessionID, false)
	r.authorityMu.Lock()
	if r.authorityEntryBySession(sessionID) == entry {
		r.authorityBySession.Delete(sessionID)
		r.signalAuthorityChangeLocked()
	}
	r.authorityMu.Unlock()
	return err
}

func (r *RuntimeRegistry) authorityEntryBySession(sessionID string) *authorityRuntimeEntry {
	if r == nil {
		return nil
	}
	value, ok := r.authorityBySession.Load(strings.TrimSpace(sessionID))
	if !ok {
		return nil
	}
	entry, ok := value.(*authorityRuntimeEntry)
	if !ok {
		panic("authority Runtime index contains an invalid entry")
	}
	return entry
}

func (r *RuntimeRegistry) authorityEntryAndChange(sessionID string) (*authorityRuntimeEntry, <-chan struct{}) {
	r.authorityMu.Lock()
	defer r.authorityMu.Unlock()
	if r.authorityChanged == nil {
		r.authorityChanged = make(chan struct{})
	}
	return r.authorityEntryBySession(sessionID), r.authorityChanged
}

func (r *RuntimeRegistry) signalAuthorityChangeLocked() {
	if r.authorityChanged != nil {
		close(r.authorityChanged)
	}
	r.authorityChanged = make(chan struct{})
}

func (r *RuntimeRegistry) authorityEntryByRef(ref runtimeids.SessionResourceRef) *authorityRuntimeEntry {
	entry := r.authorityEntryBySession(ref.SessionID().String())
	if entry != nil && entry.ref != ref {
		return nil
	}
	return entry
}

func (r *RuntimeRegistry) withCurrentAuthorityEntry(ref runtimeids.SessionResourceRef, mutate func(*authorityRuntimeEntry) bool) bool {
	entry := r.authorityEntryByRef(ref)
	if entry == nil {
		return false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.lifecycle == authorityRuntimeEntryReady && entry.feedReady && mutate(entry)
}

func (e *authorityRuntimeEntry) transcriptAttachable() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lifecycle == authorityRuntimeEntryReady && e.feedReady
}

func (r *RuntimeRegistry) WithExecutionTargetResolver(resolver func(context.Context, string) (*worktreepb.SessionExecutionTarget, error)) *RuntimeRegistry {
	if r == nil {
		return nil
	}
	r.executionTargetResolver = resolver
	return r
}

func (r *RuntimeRegistry) WithBackgroundProcessSnapshots(source func() []shelltool.Snapshot) *RuntimeRegistry {
	if r == nil {
		return nil
	}
	r.backgroundProcessSnapshots = source
	return r
}

func (r *RuntimeRegistry) WithTranscriptContractViolationPanic(enabled bool) *RuntimeRegistry {
	if r == nil {
		return nil
	}
	transcriptContractViolationsPanic = enabled
	return r
}

func (r *RuntimeRegistry) ActiveRuntimeActivitySnapshots(context.Context) ([]runtimeactivity.ActiveSessionSnapshot, error) {
	if r == nil {
		return []runtimeactivity.ActiveSessionSnapshot{}, nil
	}
	snapshots := make([]runtimeactivity.ActiveSessionSnapshot, 0)
	r.authorityBySession.Range(func(_, value any) bool {
		entry, ok := value.(*authorityRuntimeEntry)
		if !ok {
			panic("Runtime Main View index contains an invalid entry")
		}
		view := entry.mainView.Load()
		if view == nil || !protoapi.RuntimeActivityActiveForControl(view.Activity) {
			return true
		}
		snapshots = append(snapshots, runtimeactivity.ActiveSessionSnapshot{
			SessionID: entry.ref.SessionID().String(),
			Activity:  cloneRuntimeActivity(view.Activity),
		})
		return true
	})
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].SessionID < snapshots[j].SessionID
	})
	return snapshots, nil
}

func (r *RuntimeRegistry) RuntimeReadModelFeedSnapshot(_ context.Context, sessionID string) (*runtimepb.ReadModelUpdate, error) {
	id := strings.TrimSpace(sessionID)
	if r == nil || id == "" {
		return runtimeactivity.BuildFeedSnapshot(
			runtimeactivity.NextReadModelVersion(id),
			runtimeactivity.ResolverSnapshot{},
		)
	}
	resolver := r.runtimeActivityResolverSnapshot(id)
	return runtimeactivity.BuildFeedSnapshot(runtimeactivity.NextReadModelVersion(id), resolver)
}

func (r *RuntimeRegistry) unavailableRuntimeReadModelFeedSnapshot(sessionID string) (*runtimepb.ReadModelUpdate, error) {
	id := strings.TrimSpace(sessionID)
	return runtimeactivity.BuildFeedSnapshot(runtimeactivity.NextReadModelVersion(id), runtimeactivity.ResolverSnapshot{})
}

func (r *RuntimeRegistry) runtimeActivityResolverSnapshot(sessionID string) runtimeactivity.ResolverSnapshot {
	id := strings.TrimSpace(sessionID)
	if r == nil || id == "" {
		return runtimeactivity.ResolverSnapshot{}
	}
	var engine *runtime.Engine
	if entry := r.authorityEntryBySession(id); entry != nil {
		engine = entry.engine
	}
	snapshot := runtimeactivity.ResolverSnapshot{Registry: r.RuntimeActivityRegistrySnapshot(id)}
	snapshot.Active = runtimeactivity.ActiveStepFromProvider(engine)
	if engine != nil {
		snapshot.LiveRunActive = engine.HasActiveLiveRunGroup()
		snapshot.Reviewer = engine.ReviewerActivity()
	}
	if len(r.pendingPrompts.List(id)) > 0 {
		snapshot.PromptWait = true
	}
	return snapshot
}

func (r *RuntimeRegistry) RuntimeActivityRegistrySnapshot(sessionID string) runtimeactivity.RegistrySnapshot {
	if r == nil {
		return runtimeactivity.RegistrySnapshot{}
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return runtimeactivity.RegistrySnapshot{}
	}
	if authorityEntry := r.authorityEntryBySession(id); authorityEntry != nil {
		authorityEntry.mu.Lock()
		lifecycle := authorityEntry.lifecycle
		authorityEntry.mu.Unlock()
		switch lifecycle {
		case authorityRuntimeEntryReady:
			return runtimeactivity.RegistrySnapshot{
				Registered:     true,
				QueueAccepting: true,
			}
		case authorityRuntimeEntryDraining:
			return runtimeactivity.RegistrySnapshot{
				Registered: true,
				Draining:   true,
			}
		case authorityRuntimeEntryRetired:
			return runtimeactivity.RegistrySnapshot{}
		default:
			panic(fmt.Sprintf("authority runtime resource for session %q has unknown registry lifecycle %d", id, lifecycle))
		}
	}
	return runtimeactivity.RegistrySnapshot{}
}

func (r *RuntimeRegistry) publishCurrentRuntimeActivity(sessionID string) error {
	if r == nil {
		return nil
	}
	id := strings.TrimSpace(sessionID)
	update, err := r.RuntimeReadModelFeedSnapshot(context.Background(), id)
	if err != nil {
		return err
	}
	r.PublishRuntimeReadModelUpdate(id, update)
	return nil
}

func (r *RuntimeRegistry) PublishRuntimeEventToAll(evt runtime.Event) error {
	if r == nil {
		return nil
	}
	authorityEntries := make([]*authorityRuntimeEntry, 0)
	r.authorityBySession.Range(func(_, value any) bool {
		entry, ok := value.(*authorityRuntimeEntry)
		if !ok {
			panic("authority Runtime index contains an invalid entry")
		}
		authorityEntries = append(authorityEntries, entry)
		return true
	})
	for _, entry := range authorityEntries {
		if err := r.publishRuntimeEvent(entry, evt); err != nil {
			return err
		}
	}
	return nil
}

func (r *RuntimeRegistry) PublishAuthorityRuntimeEvent(ref runtimeids.SessionResourceRef, evt runtime.Event) error {
	if r == nil {
		return nil
	}
	entry := r.authorityEntryByRef(ref)
	if entry == nil {
		return nil
	}
	return r.publishRuntimeEvent(entry, evt)
}

func (r *RuntimeRegistry) publishRuntimeEvent(entry *authorityRuntimeEntry, evt runtime.Event) error {
	if evt.Kind == runtime.EventRuntimeActivityChanged {
		return r.publishCurrentRuntimeActivity(entry.ref.SessionID().String())
	}
	if !transcriptEventRequiresVisibleSubscriber(evt) || entry.sessionFeed.HasSubscribers() {
		messages, err := runtimeview.TranscriptMessagesFromRuntimeEventChecked(evt)
		if err != nil {
			contractErr := entry.sessionFeed.CloseContractViolation(fmt.Errorf("project runtime transcript event: %w", err))
			return contractErr
		}
		entry.sessionFeed.Publish(messages)
	}
	if runtimeEventShouldPublishSessionStatus(evt) {
		return r.publishTranscriptAndMainView(entry, func() ([]*transcriptpb.Event, error) {
			status, err := runtimeview.TranscriptSessionStatusFromRuntime(entry.engine)
			if err != nil {
				return nil, err
			}
			return []*transcriptpb.Event{{Payload: &transcriptpb.Event_SessionStatus{SessionStatus: status}}}, nil
		})
	}
	return nil
}

func (r *RuntimeRegistry) PublishSessionIdentity(sessionID string) error {
	if r == nil {
		return nil
	}
	id := strings.TrimSpace(sessionID)
	entry := r.authorityEntryBySession(id)
	if entry == nil {
		return nil
	}
	return r.publishTranscriptAndMainView(entry, func() ([]*transcriptpb.Event, error) {
		identity, err := r.sessionIdentity(entry, id)
		if err != nil {
			return nil, err
		}
		return []*transcriptpb.Event{{Payload: &transcriptpb.Event_SessionIdentity{SessionIdentity: identity}}}, nil
	})
}

func (r *RuntimeRegistry) PublishSessionStatus(sessionID string) error {
	if r == nil {
		return nil
	}
	entry := r.authorityEntryBySession(sessionID)
	if entry == nil {
		return nil
	}
	return r.publishTranscriptAndMainView(entry, func() ([]*transcriptpb.Event, error) {
		status, err := r.sessionStatus(entry)
		if err != nil {
			return nil, err
		}
		return []*transcriptpb.Event{{Payload: &transcriptpb.Event_SessionStatus{SessionStatus: status}}}, nil
	})
}

func (r *RuntimeRegistry) PublishSessionSettingFeedback(
	sessionID string,
	feedback *transcriptpb.SessionSettingFeedback,
) error {
	if r == nil {
		return nil
	}
	if err := protoapi.Validate(feedback); err != nil {
		return err
	}
	id := strings.TrimSpace(sessionID)
	entry := r.authorityEntryBySession(id)
	if entry == nil {
		return nil
	}
	return r.publishTranscriptAndMainView(entry, func() ([]*transcriptpb.Event, error) {
		feedbackEvent := &transcriptpb.Event{Payload: &transcriptpb.Event_SessionSettingFeedback{SessionSettingFeedback: feedback}}
		if feedback.Kind == transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_SESSION_NAME {
			identity, err := r.sessionIdentity(entry, id)
			if err != nil {
				return nil, err
			}
			return []*transcriptpb.Event{
				{Payload: &transcriptpb.Event_SessionIdentity{SessionIdentity: identity}},
				feedbackEvent,
			}, nil
		}
		status, err := r.sessionStatus(entry)
		if err != nil {
			return nil, err
		}
		return []*transcriptpb.Event{
			{Payload: &transcriptpb.Event_SessionStatus{SessionStatus: status}},
			feedbackEvent,
		}, nil
	})
}

func (r *RuntimeRegistry) sessionIdentity(
	entry *authorityRuntimeEntry,
	sessionID string,
) (*transcriptpb.SessionIdentity, error) {
	identity, err := runtimeview.TranscriptSessionIdentityFromRuntime(entry.engine)
	if err != nil {
		return nil, err
	}
	target, err := r.resolveSessionExecutionTarget(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	identity.ExecutionTarget = target
	return identity, nil
}

func (r *RuntimeRegistry) sessionStatus(entry *authorityRuntimeEntry) (*transcriptpb.SessionStatus, error) {
	return runtimeview.TranscriptSessionStatusFromRuntime(entry.engine)
}

func (r *RuntimeRegistry) resolveSessionExecutionTarget(ctx context.Context, sessionID string) (*worktreepb.SessionExecutionTarget, error) {
	if r == nil || r.executionTargetResolver == nil {
		return nil, nil
	}
	target, err := r.executionTargetResolver(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, fmt.Errorf("resolve execution target for session %q: %w", strings.TrimSpace(sessionID), err)
	}
	if target == nil {
		return nil, nil
	}
	normalized := clientui.NormalizeSessionExecutionTarget(target)
	if clientui.SessionExecutionTargetIsZero(normalized) {
		return nil, fmt.Errorf("resolve execution target for session %q returned an empty target", strings.TrimSpace(sessionID))
	}
	return normalized, nil
}

func runtimeEventShouldPublishSessionStatus(evt runtime.Event) bool {
	return evt.ContextUsage != nil || evt.GoalStatus != nil || evt.Compaction != nil || evt.Kind == runtime.EventAssistantMessage
}

func transcriptEventRequiresVisibleSubscriber(evt runtime.Event) bool {
	return evt.Kind == runtime.EventAssistantDelta || evt.Kind == runtime.EventAssistantDeltaReset
}

func (r *RuntimeRegistry) PublishRuntimeReadModelUpdate(sessionID string, update *runtimepb.ReadModelUpdate) {
	if r == nil {
		return
	}
	entry := r.authorityEntryBySession(sessionID)
	if entry == nil {
		return
	}
	if err := protoapi.Validate(update); err != nil {
		panic(fmt.Sprintf("publish invalid canonical runtime read-model update: %+v: %v", update, err))
	}
	entry.publicationMu.Lock()
	defer entry.publicationMu.Unlock()
	current := entry.mainView.Load()
	if current != nil &&
		(protoapi.ReadModelVersionsEqual(current.Version, update.Version) || protoapi.ReadModelVersionNewerThan(current.Version, update.Version)) {
		return
	}
	publicationErr := r.publishRuntimeMainViewLocked(entry, update.Version, update.Activity)
	if publicationErr != nil {
		log.Printf("publish Runtime Main View for Session %q: %v", strings.TrimSpace(sessionID), publicationErr)
	}
	entry.sessionFeed.PublishRuntimeReadModel(update)
	r.updateAggregateRuntimeActivityForAuthority(sessionID, entry, protoapi.RuntimeActivityActiveForControl(update.Activity))
}

func (r *RuntimeRegistry) PublishWorktreeTransitionOutcome(sessionID string, outcome clientui.WorktreeTransitionOutcome) {
	if r == nil {
		return
	}
	if err := outcome.Validate(); err != nil {
		panic(fmt.Sprintf("publish invalid worktree transition outcome for session %q: %v", strings.TrimSpace(sessionID), err))
	}
	entry := r.authorityEntryBySession(sessionID)
	if entry == nil {
		return
	}
	transcriptOutcome := &transcriptpb.WorktreeTransitionOutcome{
		OperationId: outcome.OperationID.String(),
	}
	switch outcome.Transition {
	case clientui.WorktreeTransitionEnter:
		transcriptOutcome.Transition = transcriptpb.WorktreeTransitionKind_WORKTREE_TRANSITION_KIND_ENTER
	case clientui.WorktreeTransitionLeave:
		transcriptOutcome.Transition = transcriptpb.WorktreeTransitionKind_WORKTREE_TRANSITION_KIND_LEAVE
	case clientui.WorktreeTransitionDelete:
		transcriptOutcome.Transition = transcriptpb.WorktreeTransitionKind_WORKTREE_TRANSITION_KIND_DELETE
	}
	switch outcome.State {
	case clientui.WorktreeTransitionCompleted:
		transcriptOutcome.State = transcriptpb.WorktreeTransitionState_WORKTREE_TRANSITION_STATE_COMPLETED
	case clientui.WorktreeTransitionFailed:
		transcriptOutcome.State = transcriptpb.WorktreeTransitionState_WORKTREE_TRANSITION_STATE_FAILED
	}
	if outcome.Failure != nil {
		if outcome.Failure.SelectorError != nil {
			transcriptOutcome.FailureDetail = &transcriptpb.WorktreeTransitionOutcome_SelectorError{SelectorError: outcome.Failure.SelectorError}
		} else {
			transcriptOutcome.FailureDetail = &transcriptpb.WorktreeTransitionOutcome_Failure{Failure: &transcriptpb.Diagnostic{
				Code:   "worktree_transition_failed",
				Detail: outcome.Failure.Diagnostic,
			}}
		}
		if outcome.Failure.DeletePrecondition != nil {
			native := outcome.Failure.DeletePrecondition
			dirtyState := &worktreepb.DirtyState{UnknownCause: native.UnknownCause}
			switch native.Kind {
			case clientui.WorktreeDirtyStateClean:
				dirtyState.Kind = worktreepb.DirtyStateKind_DIRTY_STATE_CLEAN
			case clientui.WorktreeDirtyStateDirty:
				dirtyState.Kind = worktreepb.DirtyStateKind_DIRTY_STATE_DIRTY
			case clientui.WorktreeDirtyStateUnknown:
				dirtyState.Kind = worktreepb.DirtyStateKind_DIRTY_STATE_UNKNOWN
			}
			if native.DirtyFileCount != nil {
				count, err := protoapi.Int32(*native.DirtyFileCount, "dirty file count")
				if err != nil {
					if closeErr := entry.sessionFeed.CloseContractViolation(err); closeErr != nil {
						log.Printf("publish Worktree transition for Session %q: %v", sessionID, closeErr)
					}
					return
				}
				dirtyState.DirtyFileCount = &count
			}
			transcriptOutcome.DeletePrecondition = &worktreepb.DeletePreconditionDetails{DirtyState: dirtyState}
		}
	}
	entry.sessionFeed.Publish([]*transcriptpb.Event{{Payload: &transcriptpb.Event_WorktreeTransitionOutcome{WorktreeTransitionOutcome: transcriptOutcome}}})
}

func (r *RuntimeRegistry) SubscribeSessionTranscript(ctx context.Context, req *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime registry is required")
	}
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	id := strings.TrimSpace(req.SessionId)
	for {
		authorityEntry, authorityChanged := r.authorityEntryAndChange(id)
		if authorityEntry != nil {
			subscription, err := r.subscribeAuthorityTranscript(ctx, id, authorityEntry)
			if err == nil || !errors.Is(err, serverapi.ErrStreamUnavailable) {
				return subscription, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		case <-authorityChanged:
		}
	}
}

func (r *RuntimeRegistry) subscribeAuthorityTranscript(ctx context.Context, id string, entry *authorityRuntimeEntry) (serverapi.TranscriptSubscription, error) {
	if !entry.transcriptAttachable() {
		return nil, fmt.Errorf("authority runtime subscription is not ready: %w", serverapi.ErrStreamUnavailable)
	}
	var sub *transcriptSubscription
	err := entry.engine.WithTranscriptHydrationSnapshot(func(snapshot runtime.TranscriptHydrationSnapshot) error {
		tailPage, pageErr := entry.engine.TranscriptNewestSegmentPage()
		if pageErr != nil {
			return fmt.Errorf("read transcript hydration tail segment: %w", pageErr)
		}
		var subscribeErr error
		sub, subscribeErr = entry.sessionFeed.Subscribe(func() (*transcriptpb.Hydration, error) {
			return r.composeTranscriptHydration(ctx, id, entry, snapshot, tailPage)
		})
		return subscribeErr
	})
	if err != nil {
		if !entry.transcriptAttachable() {
			return nil, fmt.Errorf("authority runtime subscription became unavailable: %w", serverapi.ErrStreamUnavailable)
		}
		return nil, err
	}
	return sub, nil
}

func (r *RuntimeRegistry) PromptPendingScope(scope sessionruntime.ExecutionScope, req askquestion.AskQuestionRequest, createdAt time.Time) error {
	if r == nil {
		return nil
	}
	resource, ok := scope.Resource()
	if !ok {
		panic(fmt.Sprintf("workflow prompt scope %s has no session resource", scope.ID()))
	}
	id := resource.SessionID().String()
	entry := r.authorityEntryByRef(resource)
	if entry == nil {
		return fmt.Errorf(
			"publish pending prompt %q for session %s generation %d: %w",
			req.ToolCallID,
			resource.SessionID(),
			resource.Generation(),
			serverapi.ErrStreamUnavailable,
		)
	}
	var snapshot PendingPromptSnapshot
	var wakeErr error
	entry.publicationMu.Lock()
	projected := r.withCurrentAuthorityEntry(resource, func(_ *authorityRuntimeEntry) bool {
		var admitted bool
		snapshot, admitted = r.pendingPrompts.Begin(id, resource, scope.ID(), req, createdAt)
		return admitted
	})
	if projected {
		publishPendingPrompt(entry.sessionFeed, id, snapshot, pendingPromptEventPending)
		r.publishAttentionPending(id, snapshot)
		wakeErr = r.publishTaskQuestionWaitingForScope(scope, snapshot)
	}
	entry.publicationMu.Unlock()
	if !projected {
		return fmt.Errorf(
			"publish pending prompt %q for session %s generation %d: %w",
			req.ToolCallID,
			resource.SessionID(),
			resource.Generation(),
			serverapi.ErrStreamUnavailable,
		)
	}
	r.publishCurrentRuntimeActivity(id)
	if wakeErr != nil {
		return wakeErr
	}
	return nil
}

func (r *RuntimeRegistry) PromptResolvedScope(scope sessionruntime.ExecutionScope, requestID string) error {
	if r == nil {
		return nil
	}
	resource, ok := scope.Resource()
	if !ok {
		panic(fmt.Sprintf("workflow prompt scope %s has no session resource", scope.ID()))
	}
	id := resource.SessionID().String()
	var snapshot PendingPromptSnapshot
	entry := r.authorityEntryByRef(resource)
	resolved := r.withCurrentAuthorityEntry(resource, func(_ *authorityRuntimeEntry) bool {
		var ok bool
		snapshot, ok = r.pendingPrompts.Complete(id, resource, scope.ID(), requestID)
		return ok
	})
	if resolved {
		if entry != nil {
			publishPendingPrompt(entry.sessionFeed, id, snapshot, pendingPromptEventResolved)
		}
		r.publishAttentionResolved(id, snapshot)
		r.publishCurrentRuntimeActivity(id)
		return r.publishTaskQuestionCleared(id, snapshot)
	}
	return nil
}

func (r *RuntimeRegistry) publishPromptResolution(entry *authorityRuntimeEntry, sessionID string, snapshot PendingPromptSnapshot) {
	if r == nil || entry == nil {
		return
	}
	publishPendingPrompt(entry.sessionFeed, sessionID, snapshot, pendingPromptEventResolved)
	r.publishAttentionResolved(sessionID, snapshot)
	if err := r.publishTaskQuestionCleared(sessionID, snapshot); err != nil {
		logAttentionNotificationOperationFailure(
			"publish workflow prompt resolution event",
			sessionID,
			snapshot.Request.ToolCallID,
			err,
		)
	}
}

func (r *RuntimeRegistry) ListPendingPrompts(sessionID string) []PendingPromptSnapshot {
	return r.pendingPrompts.List(sessionID)
}

func (r *RuntimeRegistry) SetSleepObserver(observer func(active bool)) {
	if r == nil {
		return
	}
	r.sleepObserverMu.Lock()
	r.sleepObserver = observer
	r.sleepObserverMu.Unlock()
}

func (r *RuntimeRegistry) updateAggregateRuntimeActivityForAuthority(sessionID string, entry *authorityRuntimeEntry, activeForControl bool) bool {
	if r == nil || entry == nil {
		return false
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return false
	}
	entry.mu.Lock()
	lifecycle := entry.lifecycle
	entry.mu.Unlock()
	if lifecycle == authorityRuntimeEntryRetired && activeForControl {
		return false
	}
	r.authorityMu.Lock()
	current := r.authorityEntryBySession(id)
	if current != entry && (activeForControl || current != nil) {
		r.authorityMu.Unlock()
		return false
	}
	r.updateAggregateRuntimeActivityState(id, activeForControl)
	r.authorityMu.Unlock()
	return true
}

func (r *RuntimeRegistry) updateAggregateRuntimeActivityState(sessionID string, activeForControl bool) {
	r.runStateMu.Lock()
	wasActive := len(r.blockingActivitySessions) > 0
	if activeForControl {
		r.blockingActivitySessions[sessionID] = true
	} else {
		delete(r.blockingActivitySessions, sessionID)
	}
	active := len(r.blockingActivitySessions) > 0
	if wasActive == active {
		r.runStateMu.Unlock()
		return
	}
	r.sleepObserverMu.Lock()
	observer := r.sleepObserver
	r.runStateMu.Unlock()
	defer r.sleepObserverMu.Unlock()
	if observer != nil {
		observer(active)
	}
}
