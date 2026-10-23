package workflowreactivator

import (
	"context"

	"core/server/sessionruntime"
	"core/shared/runtimeids"
)

type Func func(context.Context, runtimeids.SessionID) (sessionruntime.ExecutionHandle, error)

func (f Func) ReactivateWorkflowSession(
	ctx context.Context,
	sessionID runtimeids.SessionID,
) (sessionruntime.ExecutionHandle, error) {
	return f(ctx, sessionID)
}
