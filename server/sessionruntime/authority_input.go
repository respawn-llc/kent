package sessionruntime

import (
	"context"
	"errors"

	"core/server/agentruntime"
	"core/server/sessionstore"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
)

func (a *Authority) prepareSessionExecution(ctx context.Context, sessionID runtimeids.SessionID) (ExecutionHandle, error) {
	a.mu.Lock()
	owner := a.workflowSessions
	a.mu.Unlock()
	if owner == nil {
		return nil, nil
	}
	// Workflow admission may start an execution through this Authority. It must
	// never run while we hold the Session admission gate.
	return owner.EnsureSessionExecution(ctx, sessionID)
}

func (a *Authority) SubmitShellCommand(ctx context.Context, descriptor session.SessionDescriptor, command string, accept runtime.CommandAcceptance) error {
	return a.runIdleAgentTurn(ctx, descriptor, accept, func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
		_, err := engine.SubmitUserShellCommandWithAcceptance(ctx, command, accept)
		return err
	})
}

func (a *Authority) CompactSession(ctx context.Context, descriptor session.SessionDescriptor, admission runtimeinput.ManualCompactionAdmission, accept runtime.CommandAcceptance) error {
	return a.runIdleAgentTurn(ctx, descriptor, accept, func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
		_, err := engine.CompactContextAdmissionWithAcceptance(ctx, admission, accept)
		return err
	})
}

func (a *Authority) queuedSessionExecution(ctx context.Context, sessionID runtimeids.SessionID) (ExecutionHandle, error) {
	a.mu.Lock()
	owner := a.workflowSessions
	a.mu.Unlock()
	if owner == nil {
		return nil, nil
	}
	binding, err := owner.ResolveSessionWorkflow(ctx, sessionID)
	if err != nil || binding == nil {
		return nil, err
	}
	handle, active := a.SessionExecution(sessionID)
	if !active {
		return nil, ErrSessionWorkflowActivationActive
	}
	return a.ValidateLiveWorkflowAgentExecution(handle, sessionID, *binding)
}

func (a *Authority) SubmitAgentInput(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	input runtime.QueuedUserInput,
	accept runtime.CommandAcceptance,
) (runtime.QueuedUserMessage, error) {
	return a.submitAgentInput(ctx, descriptor, accept, a.prepareSessionExecution,
		func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) (runtime.QueuedUserMessage, error) {
			return engine.SteerInput(ctx, input, accept)
		})
}

func (a *Authority) QueueAgentInput(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	input runtime.QueuedUserInput,
	accept runtime.CommandAcceptance,
) (runtime.QueuedUserMessage, error) {
	return a.submitAgentInput(ctx, descriptor, accept, a.queuedSessionExecution,
		func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) (runtime.QueuedUserMessage, error) {
			return engine.QueueUserInputWithAcceptance(ctx, input, accept)
		})
}

func (a *Authority) submitAgentInput(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	accept runtime.CommandAcceptance,
	prepare func(context.Context, runtimeids.SessionID) (ExecutionHandle, error),
	submit func(context.Context, *runtime.Engine, runtime.CommandAcceptance) (runtime.QueuedUserMessage, error),
) (runtime.QueuedUserMessage, error) {
	var result runtime.QueuedUserMessage
	if a == nil {
		return result, errors.New("session runtime authority is required")
	}
	if err := descriptor.Validate(); err != nil {
		return result, err
	}
	for {
		handle, err := prepare(ctx, descriptor.SessionID())
		if errors.Is(err, ErrExecutionNoLongerLive) {
			continue
		}
		if err != nil {
			return result, err
		}
		var expected *runtimeids.ExecutionScopeID
		if handle != nil {
			id := handle.Scope().ID()
			expected = &id
		}
		err = a.runCurrentTurn(ctx, descriptor, accept, expected, func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
			var err error
			result, err = submit(ctx, engine, accept)
			return err
		})
		if errors.Is(err, ErrExecutionNoLongerLive) {
			continue
		}
		return result, err
	}
}
