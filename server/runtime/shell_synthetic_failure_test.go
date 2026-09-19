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

func assertSyntheticFailureOutput(t *testing.T, output json.RawMessage, tool string, wantMessage string) {
	t.Helper()
	var message string
	switch toolspec.ID(tool) {
	case toolspec.ToolExecCommand, toolspec.ToolWriteStdin:
		if err := json.Unmarshal(output, &message); err != nil {
			t.Fatalf("decode plaintext shell error: %v", err)
		}
	default:
		var result struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("decode structured tool error: %v", err)
		}
		message = result.Error
	}
	if message != wantMessage {
		t.Fatalf("synthetic error disposition = %q, want %q", message, wantMessage)
	}
}

func TestShellRepairOutputsArePlaintext(t *testing.T) {
	t.Parallel()
	for _, tool := range []toolspec.ID{toolspec.ToolExecCommand, toolspec.ToolWriteStdin} {
		for _, fresh := range []bool{false, true} {
			name := string(tool) + "/live"
			if fresh {
				name = string(tool) + "/fresh"
			}
			t.Run(name, func(t *testing.T) {
				store := mustCreateTestSession(t)
				client := &fakeClient{responses: []llm.Response{finalTextResponse("done")}}
				call := llm.ToolCall{ID: "missing-shell", Name: string(tool), Input: json.RawMessage(`{}`)}
				if fresh {
					mustAppendTestEvent(t, store, "step", llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}})
					store = mustOpenTestSession(t, store.Dir())
				} else {
					client.errors = []error{&llm.ProviderAPIError{ProviderID: "openai", StatusCode: 400, Code: llm.UnifiedErrorCodeUnknown}}
				}
				engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-5"})
				if !fresh {
					steerDanglingToolCall(t, engine, "step", call)
				}
				if _, err := engine.SubmitUserMessage(context.Background(), "continue"); err != nil {
					t.Fatal(err)
				}
				_, completion := repairCompletionRecord(t, store, call.ID)
				if !completion.IsError {
					t.Fatal("repair did not record an error")
				}
				assertPlaintextShellFailure(t, completion.Output)
				request := client.calls[len(client.calls)-1]
				found := false
				for _, item := range request.Items {
					if item.Type == llm.ResponseItemTypeFunctionCallOutput && item.CallID != nil && *item.CallID == call.ID {
						assertPlaintextShellFailure(t, item.Output)
						found = true
					}
				}
				if !found {
					t.Fatal("model request omitted repaired shell output")
				}
				if !fresh && repairRequestHasToolOutput(client.calls[0].Items, call.ID) {
					t.Fatal("repair mutated the previously sent request")
				}
			})
		}
	}
}

func assertPlaintextShellFailure(t *testing.T, output json.RawMessage) {
	t.Helper()
	var message string
	if err := json.Unmarshal(output, &message); err != nil {
		t.Fatalf("shell failure must be a JSON-encoded plaintext string: %s: %v", output, err)
	}
	if message == "" {
		t.Fatal("shell failure omitted its explanation")
	}
}

func TestInterruptedShellOutputsArePlaintext(t *testing.T) {
	t.Parallel()
	for _, tool := range []toolspec.ID{toolspec.ToolExecCommand, toolspec.ToolWriteStdin} {
		t.Run(string(tool), func(t *testing.T) {
			store := mustCreateTestSession(t)
			started := make(chan struct{})
			engine := mustNewTestEngine(t, store, &fakeClient{}, newTestToolRegistry(t, tools.HandlerRegistration{
				ID: tool, Handler: interruptedShellTool{started: started},
			}), Config{Model: "gpt-5"})
			stepID := runtimeTestStepID("interrupted-shell")
			restore := setTestActiveStep(engine, stepID)
			defer restore()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			input := json.RawMessage(`{"cmd":"true"}`)
			if tool == toolspec.ToolWriteStdin {
				input = json.RawMessage(`{"session_id":1}`)
			}
			go func() {
				_, err := engine.executeToolCalls(ctx, stepID, []llm.ToolCall{
					{ID: "started", Name: string(tool), Input: input},
				})
				done <- err
			}()
			select {
			case <-started:
			case err := <-done:
				t.Fatalf("execution ended before handler started: %v", err)
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("interrupted execution error = %v", err)
			}
			_, completion := repairCompletionRecord(t, store, "started")
			if !completion.IsError {
				t.Fatal("interruption is not an error")
			}
			assertSyntheticFailureOutput(t, completion.Output, completion.Name, missingToolOutputInterruptedMessage)
		})
	}
}

type interruptedShellTool struct {
	started chan struct{}
}

func (h interruptedShellTool) Call(ctx context.Context, _ tools.Call) (tools.Result, error) {
	close(h.started)
	<-ctx.Done()
	return tools.Result{}, ctx.Err()
}
