package sessionruntime

import (
	"context"
	"log/slog"
	"time"
)

const runtimeIdleInterval = 3 * time.Minute

func (r *agentResource) acceptRuntimeWrite() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != AgentResourceReady {
		return
	}
	if r.current != nil {
		r.current.admission = executionAccepted
	}
	r.refreshIdleLocked()
	r.updateIdleLocked(false)
}

func (r *agentResource) refreshIdleLocked() {
	deadline := time.Now().Add(runtimeIdleInterval)
	r.idleDeadline = &deadline
	r.signalLocked()
}

func (r *agentResource) runIdleRetirement(ctx context.Context) {
	timer := time.NewTimer(runtimeIdleInterval)
	defer timer.Stop()
	for {
		r.mu.Lock()
		if r.state != AgentResourceReady {
			r.mu.Unlock()
			return
		}
		changed := r.changed
		engine := r.engine
		r.mu.Unlock()
		pending, err := engine.PendingWorkSnapshot()
		if err != nil {
			slog.Error("read Session Runtime pending work", "session_id", r.ref.SessionID(), "error", err)
			return
		}
		r.mu.Lock()
		if r.changed != changed {
			r.mu.Unlock()
			continue
		}
		r.updateIdleLocked(len(pending.Items) != 0)
		var expiry <-chan time.Time
		if r.idleDeadline != nil && r.current == nil && r.pins == 0 && r.callbacks == 0 && r.steps == 0 {
			timer.Reset(time.Until(*r.idleDeadline))
			expiry = timer.C
		} else {
			timer.Stop()
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-r.ctx.Done():
			return
		case <-changed:
		case <-r.pendingWorkChanged:
		case <-expiry:
			if err := r.authority.closeIdleResource(ctx, r); err != nil {
				slog.Error("retire idle Session Runtime", "session_id", r.ref.SessionID(), "error", err)
				return
			}
		}
	}
}

func (r *agentResource) updateIdleLocked(pending bool) {
	if pending || r.observers != 0 || r.steps != 0 || (r.current != nil && r.current.admission == executionAccepted) {
		r.idleDeadline = nil
	} else if r.idleDeadline == nil {
		r.refreshIdleLocked()
	}
}

func (a *Authority) closeIdleResource(ctx context.Context, resource *agentResource) error {
	gate, releaseGate := a.gateFor(resource.ref.SessionID())
	defer releaseGate()
	if err := gate.lock.LockContext(ctx); err != nil {
		return err
	}
	defer gate.lock.Unlock()

	a.mu.Lock()
	current := a.resources[resource.ref.SessionID()] == resource
	a.mu.Unlock()
	pending, err := resource.engine.PendingWorkSnapshot()
	if err != nil {
		return err
	}
	resource.mu.Lock()
	if !current || resource.state != AgentResourceReady ||
		resource.idleDeadline == nil || time.Now().Before(*resource.idleDeadline) ||
		resource.current != nil || resource.pins != 0 || resource.callbacks != 0 || resource.steps != 0 || resource.observers != 0 || len(pending.Items) != 0 {
		resource.mu.Unlock()
		return nil
	}
	_, err = a.closeAdmittedResourceLocked(ctx, resource)
	return err
}
