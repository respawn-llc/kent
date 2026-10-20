package lifecyclehook

import (
	"sync"

	"core/shared/lifecyclecontract"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

type EventContext struct {
	mu      sync.RWMutex
	context lifecyclecontract.Context
}

func InitialContext(sessionID string, sessionTitle *string) (lifecyclecontract.Context, error) {
	parsed := runtimeids.SessionIDFromProto(sessionID)
	context := lifecyclecontract.Context{
		SessionID:    &parsed,
		SessionTitle: textutil.Pointer(sessionTitle),
	}
	return context, nil
}

func NewEventContext(initial lifecyclecontract.Context) *EventContext {
	return &EventContext{context: cloneContext(initial)}
}

func (c *EventContext) AcceptSessionIdentity(identity *transcriptpb.SessionIdentity) {
	sessionID := runtimeids.SessionIDFromProto(identity.SessionId)
	c.mu.Lock()
	c.context.SessionID = &sessionID
	c.context.SessionTitle = textutil.Pointer(identity.SessionName)
	c.mu.Unlock()
}

func (c *EventContext) AcceptSessionStatus(status *transcriptpb.SessionStatus) {
	c.mu.Lock()
	if status.Workflow == nil {
		c.context.WorkflowTaskID = nil
	} else {
		taskID := lifecyclecontract.WorkflowTaskID(status.Workflow.TaskId)
		c.context.WorkflowTaskID = &taskID
	}
	c.mu.Unlock()
}

func (c *EventContext) Snapshot() lifecyclecontract.Context {
	if c == nil {
		return lifecyclecontract.Context{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneContext(c.context)
}

func cloneContext(context lifecyclecontract.Context) lifecyclecontract.Context {
	cloned := context
	cloned.SessionID = textutil.Pointer(context.SessionID)
	cloned.SessionTitle = textutil.Pointer(context.SessionTitle)
	cloned.WorkflowTaskID = textutil.Pointer(context.WorkflowTaskID)
	return cloned
}
