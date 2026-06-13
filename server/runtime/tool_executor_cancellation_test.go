package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/shared/toolspec"
)

type cancelAwareTool struct {
	name    toolspec.ID
	started chan struct{}
}

func (t cancelAwareTool) Call(ctx context.Context, c tools.Call) (tools.Result, error) {
	select {
	case <-t.started:
	default:
		close(t.started)
	}
	<-ctx.Done()
	out, _ := json.Marshal(map[string]any{"error": ctx.Err().Error()})
	return tools.Result{CallID: c.ID, Name: c.Name, IsError: true, Output: out, Summary: ctx.Err().Error()}, ctx.Err()
}

func TestExecuteToolCallsPropagatesContextCancellation(t *testing.T) {
	store := mustCreateTestSession(t)
	started := make(chan struct{})
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: cancelAwareTool{name: toolspec.ToolExecCommand, started: started}}), Config{Model: "gpt-5"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := eng.executeToolCalls(ctx, "step-1", []llm.ToolCall{{ID: "call-1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{}`)}})
		done <- err
	}()

	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("executeToolCalls error=%v, want context.Canceled", err)
	}
	if _, ok := eng.transcriptRuntimeState().ToolCompletionSnapshot("call-1"); !ok {
		t.Fatal("expected canceled tool completion to be persisted before returning")
	}
}
