package sessionruntime

import (
	"context"
	"core/server/agentruntime"
	"core/server/runlog"
	"core/server/runtimewire"
	"core/server/sessionstore"
	"core/server/tools"
	shelltool "core/server/tools/shell"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

type AgentResourceState uint8

var ErrSessionRunActive = errors.New("session has an active run")

var ErrSessionStartsBlocked = errors.New("session starts are blocked")

var ErrSessionWorkflowActivationActive = errors.New("session has a retained workflow activation")

const (
	AgentResourceBuilding AgentResourceState = iota + 1
	AgentResourceReady
	AgentResourceDraining
	AgentResourceClosed
)

type AgentResourceDescriptor struct {
	Ref   runtimeids.SessionResourceRef
	State AgentResourceState
}

type AgentResourceEventFeed func(runtimeids.SessionResourceRef, runtime.Event)

type AgentResourceObserver func() (io.Closer, error)

type AgentResourceLifecycle interface {
	ResourceReady(context.Context, AgentResourceDescriptor, *runtime.Engine, AgentResourceObserver) error
	ResourceDraining(context.Context, AgentResourceDescriptor) error
}

type AgentResourceStepLifecycle interface {
	StepBegan(context.Context, AgentResourceDescriptor, runtime.StepLifecycleSnapshot) error
	StepEnded(context.Context, AgentResourceDescriptor, runtime.StepLifecycleSnapshot) error
}

type resourceObservation struct {
	resource *agentResource
	once     sync.Once
	err      error
}

func (r *resourceObservation) Close() error {
	r.once.Do(func() {
		resource := r.resource
		resource.mu.Lock()
		defer resource.mu.Unlock()
		if resource.observers == 0 {
			r.err = resource.authority.invariant("close observation", errors.New("Session Runtime observation underflow"))
			return
		}
		resource.observers--
		resource.updateIdleLocked(false)
		resource.signalLocked()
	})
	return r.err
}

type agentResource struct {
	authority *Authority
	ref       runtimeids.SessionResourceRef
	ctx       context.Context
	cancel    context.CancelFunc

	// Turn admission and completion share this lock. Resource replacement
	// uses the Session gate and may wait for execution completion.
	turnMu             sync.Mutex
	mu                 sync.Mutex
	changed            chan struct{}
	state              AgentResourceState
	store              *session.Store
	engine             *runtime.Engine
	logger             *runlog.RunLogger
	localTools         *runtimewire.LocalToolRegistryBinding
	askBroker          *tools.AskQuestionBroker
	close              func() error
	backgroundLimit    int
	backgroundMode     shelltool.BackgroundOutputMode
	current            *execution
	eventMu            sync.Mutex
	eventObservers     []*resourceEventObserver
	pins               int
	callbacks          int
	steps              int
	lifecycleReady     bool
	lifecycleDraining  bool
	idleDeadline       *time.Time
	pendingWorkChanged chan struct{}
	observers          int
}

func (r *agentResource) descriptorLocked() AgentResourceDescriptor {
	return AgentResourceDescriptor{
		Ref:   r.ref,
		State: r.state,
	}
}

func (r *agentResource) descriptor() AgentResourceDescriptor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.descriptorLocked()
}

func (r *agentResource) signalLocked() {
	close(r.changed)
	r.changed = make(chan struct{})
}

func (r *agentResource) currentExecution() (ExecutionHandle, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == nil {
		return nil, false
	}
	return executionHandle{execution: r.current}, true
}

func (r *agentResource) pinLocked() error {
	if r.rejectsNewUseLocked() {
		return errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is unavailable", r.ref.SessionID(), r.ref.Generation()),
		)
	}
	r.pins++
	r.signalLocked()
	return nil
}

func (r *agentResource) releasePin() error {
	r.mu.Lock()
	if r.pins <= 0 {
		panic(fmt.Sprintf("agent resource %s generation %d pin underflow", r.ref.SessionID(), r.ref.Generation()))
	}
	r.pins--
	r.signalLocked()
	r.mu.Unlock()
	return nil
}

func (r *agentResource) withEngine(ctx context.Context, ref runtimeids.SessionResourceRef, callback func(context.Context, *runtime.Engine) error) (err error) {
	if callback == nil {
		return errors.New("agent resource callback is required")
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.ref != ref || r.rejectsNewUseLocked() || r.engine == nil {
		r.mu.Unlock()
		return errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is unavailable", ref.SessionID(), ref.Generation()),
		)
	}
	engine := r.engine
	r.callbacks++
	r.signalLocked()
	r.mu.Unlock()
	defer func() {
		if releaseErr := r.releaseCallback(); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
	}()
	return callback(ctx, engine)
}

// withStoreUnderAdmission runs while the caller owns the Session admission
// gate. Resource retirement must be attempted only after that gate is
// released, because retirement acquires the same gate.
func (r *agentResource) withStoreUnderAdmission(ctx context.Context, callback func(context.Context, *session.Store) error) error {
	r.mu.Lock()
	if r.rejectsNewUseLocked() || r.store == nil {
		r.mu.Unlock()
		return fmt.Errorf("agent resource %s generation %d is unavailable", r.ref.SessionID(), r.ref.Generation())
	}
	store := r.store
	r.callbacks++
	r.signalLocked()
	r.mu.Unlock()
	defer func() {
		r.releaseCallbackCount()
	}()
	return callback(ctx, store)
}

func (r *agentResource) withEngineUnderAdmission(
	ctx context.Context,
	callback func(context.Context, *runtime.Engine) error,
) error {
	if callback == nil {
		return errors.New("agent resource callback is required")
	}
	engine, err := r.beginEngineCallbackUnderAdmission()
	if err != nil {
		return err
	}
	defer r.releaseCallbackCount()
	return callback(ctx, engine)
}

func (r *agentResource) beginEngineCallbackUnderAdmission() (*runtime.Engine, error) {
	r.mu.Lock()
	if r.rejectsNewUseLocked() || r.engine == nil {
		ref := r.ref
		r.mu.Unlock()
		return nil, errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is unavailable", ref.SessionID(), ref.Generation()),
		)
	}
	engine := r.engine
	r.callbacks++
	r.signalLocked()
	r.mu.Unlock()
	return engine, nil
}

func (r *agentResource) releaseCallbackCount() {
	r.mu.Lock()
	if r.callbacks <= 0 {
		panic(fmt.Sprintf("agent resource %s generation %d callback underflow", r.ref.SessionID(), r.ref.Generation()))
	}
	r.callbacks--
	r.signalLocked()
	r.mu.Unlock()
}

func (r *agentResource) releaseCallback() error {
	r.releaseCallbackCount()
	return nil
}

func (r *agentResource) publishReady(ctx context.Context) error {
	if r.authority.options.resourceLifecycle == nil {
		return nil
	}
	r.mu.Lock()
	if r.state != AgentResourceReady || r.engine == nil {
		r.mu.Unlock()
		return fmt.Errorf("agent resource %s generation %d is not ready", r.ref.SessionID(), r.ref.Generation())
	}
	if r.lifecycleReady {
		r.mu.Unlock()
		return nil
	}
	r.lifecycleReady = true
	descriptor := r.descriptorLocked()
	engine := r.engine
	r.mu.Unlock()
	if err := r.authority.options.resourceLifecycle.ResourceReady(ctx, descriptor, engine, func() (io.Closer, error) {
		return r.observe()
	}); err != nil {
		return err
	}
	return engine.PublishConnectionReplacement()
}

func (r *agentResource) closeResource(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	switch r.state {
	case AgentResourceClosed:
		r.mu.Unlock()
		return nil
	case AgentResourceBuilding:
		r.mu.Unlock()
		return fmt.Errorf("agent resource %s generation %d is still building", r.ref.SessionID(), r.ref.Generation())
	}
	r.state = AgentResourceDraining
	r.cancel()
	r.signalLocked()
	notifyDraining := r.lifecycleReady && !r.lifecycleDraining && r.authority.options.resourceLifecycle != nil
	if notifyDraining {
		r.lifecycleDraining = true
	}
	descriptor := r.descriptorLocked()
	engine := r.engine
	r.mu.Unlock()
	var interruptErr error
	if engine != nil {
		interruptErr = engine.Interrupt()
	}
	var lifecycleErr error
	if notifyDraining {
		lifecycleErr = r.authority.options.resourceLifecycle.ResourceDraining(ctx, descriptor)
	}
	r.mu.Lock()
	for r.pins != 0 || r.callbacks != 0 || r.steps != 0 || r.current != nil {
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
		r.mu.Lock()
	}
	// Another close caller may have finished while this caller released the lock.
	if r.state == AgentResourceClosed {
		r.mu.Unlock()
		return errors.Join(lifecycleErr, interruptErr)
	}
	closeEngine := r.close
	r.state = AgentResourceClosed
	r.signalLocked()
	r.mu.Unlock()
	if closeEngine == nil {
		return lifecycleErr
	}
	return errors.Join(lifecycleErr, interruptErr, closeEngine())
}

func (r *agentResource) StepBegan(ctx context.Context, snapshot runtime.StepLifecycleSnapshot) error {
	r.mu.Lock()
	if r.rejectsNewStepLocked() {
		r.mu.Unlock()
		return errors.Join(
			runtime.ErrEngineClosed,
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is retiring", r.ref.SessionID(), r.ref.Generation()),
		)
	}
	if r.steps != 0 {
		r.mu.Unlock()
		panic(fmt.Sprintf("agent resource %s generation %d admitted overlapping engine steps", r.ref.SessionID(), r.ref.Generation()))
	}
	r.steps++
	r.idleDeadline = nil
	if r.current != nil {
		r.current.admission = executionAccepted
	}
	r.signalLocked()
	descriptor := r.descriptorLocked()
	r.mu.Unlock()
	if r.authority.options.stepLifecycle == nil {
		return nil
	}
	return r.authority.options.stepLifecycle.StepBegan(ctx, descriptor, snapshot)
}

func (r *agentResource) StepEnded(ctx context.Context, snapshot runtime.StepLifecycleSnapshot) error {
	r.mu.Lock()
	if r.steps == 0 {
		rejected := r.rejectsNewStepLocked()
		r.mu.Unlock()
		if rejected {
			return nil
		}
		panic(fmt.Sprintf("agent resource %s generation %d engine step underflow", r.ref.SessionID(), r.ref.Generation()))
	}
	descriptor := r.descriptorLocked()
	r.mu.Unlock()
	var publishErr error
	if r.authority.options.stepLifecycle != nil {
		publishErr = r.authority.options.stepLifecycle.StepEnded(ctx, descriptor, snapshot)
	}
	pending, pendingErr := r.engine.PendingWorkSnapshot()
	r.mu.Lock()
	if r.steps != 1 {
		r.mu.Unlock()
		panic(fmt.Sprintf("agent resource %s generation %d engine step count changed during completion", r.ref.SessionID(), r.ref.Generation()))
	}
	r.steps--
	r.updateIdleLocked(len(pending.Items) != 0)
	r.signalLocked()
	r.mu.Unlock()
	return errors.Join(publishErr, pendingErr)
}

func (r *agentResource) rejectsNewUseLocked() bool {
	return r.state != AgentResourceReady
}

func (r *agentResource) rejectsNewStepLocked() bool {
	return r.state != AgentResourceReady
}

func (r *agentResource) observe() (io.Closer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != AgentResourceReady {
		return nil, serverapi.ErrRuntimeUnavailable
	}
	r.observers++
	r.idleDeadline = nil
	r.signalLocked()
	return &resourceObservation{resource: r}, nil
}
