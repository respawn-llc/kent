package sessionruntime

import (
	"context"
	"testing"

	"core/server/agentruntime"
	"core/server/llm"
	"core/server/workflow"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

type preparedWorkflowSession struct {
	reference workflow.CurrentNodeReference
	ensure    func(context.Context, runtimeids.SessionID) (ExecutionHandle, error)
}

func (s preparedWorkflowSession) ResolveSessionWorkflow(context.Context, runtimeids.SessionID) (*workflow.CurrentNodeReference, error) {
	return &s.reference, nil
}

func (s preparedWorkflowSession) EnsureSessionExecution(ctx context.Context, id runtimeids.SessionID) (ExecutionHandle, error) {
	return s.ensure(ctx, id)
}

func TestAgentInputResolvesOwnershipAgainAfterPreparedExecutionRetires(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	id := lifecycleSessionID(t, fixture)
	client := &sessionRuntimeTestLLMClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done"), Phase: textutil.Value(llm.MessagePhaseFinal)},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	plan := authorityTestRuntimePlan(t, fixture, client)
	openLifecycleRuntime(t, fixture.authority, id, &plan)
	reference := workflowExecutionRefForTest(t, "prepared-task", "prepared-node", nil)
	descriptor := mustOpenSessionDescriptor(t, id)
	request := AgentExecutionRequest{
		Descriptor: descriptor, Resource: CurrentAgentResource{},
		Workflow: &WorkflowAgentExecution{Reference: reference},
		Runner:   func(context.Context, ExecutionScope, AgentRuntimeBridge) error { return nil },
	}
	retired, err := fixture.authority.StartAgentExecution(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retired.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	prepared := retired
	var replacement ExecutionHandle
	request.Runner = func(ctx context.Context, _ ExecutionScope, _ AgentRuntimeBridge) error {
		<-ctx.Done()
		return context.Cause(ctx)
	}
	fixture.authority.SetWorkflowSessionExecutionOwner(preparedWorkflowSession{
		reference: reference.CurrentNode,
		ensure: func(ctx context.Context, selected runtimeids.SessionID) (ExecutionHandle, error) {
			if selected != id {
				t.Fatalf("resolved Session = %s, want %s", selected, id)
			}
			if prepared != nil {
				result := prepared
				prepared = nil
				return result, nil
			}
			var err error
			replacement, err = fixture.authority.StartAgentExecution(ctx, request)
			return replacement, err
		},
	})
	inputID := runtimeids.NewQueueItemID().String()
	admitted, err := fixture.authority.SubmitAgentInput(t.Context(), descriptor, runtime.QueuedUserInput{
		ID: inputID, ExecutionText: "continue", CanonicalPresentation: "continue",
	}, func(commit func() (bool, error)) (bool, error) { return commit() })
	if err != nil || admitted.ID != inputID {
		t.Fatalf("input admission = %+v, %v", admitted, err)
	}
	current, active := fixture.authority.SessionExecution(id)
	if !active || replacement == nil || current.Scope().ID() != replacement.Scope().ID() {
		t.Fatal("input did not join the freshly resolved Workflow execution")
	}
	owner, scoped := current.Scope().Workflow()
	if !scoped || !owner.CurrentNode.Equal(reference.CurrentNode) || current.Scope().ID() == retired.Scope().ID() {
		t.Fatal("input used the retired scope or an ordinary execution")
	}
}
