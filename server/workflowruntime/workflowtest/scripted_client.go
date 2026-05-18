package workflowtest

import (
	"context"
	"errors"
	"sync"

	"builder/server/llm"
)

var ErrScriptedRuntime = errors.New("scripted runtime: no steps remaining")

type Step struct {
	Response llm.Response
	Err      error
	Cancel   bool
}

type ScriptedClient struct {
	mu    sync.Mutex
	steps []Step
	calls []llm.Request
	caps  llm.ProviderCapabilities
}

func NewScriptedClient(caps llm.ProviderCapabilities, steps ...Step) *ScriptedClient {
	return &ScriptedClient{caps: caps, steps: append([]Step(nil), steps...)}
}

func FinalAnswer(content string) Step {
	return Step{Response: llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: content, Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}
}

func ToolBatch(content string, calls ...llm.ToolCall) Step {
	return Step{Response: llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: content, Phase: llm.MessagePhaseCommentary},
		ToolCalls: append([]llm.ToolCall(nil), calls...),
		Usage:     llm.Usage{WindowTokens: 200000},
	}}
}

func AskQuestion(callID string, input []byte) Step {
	return ToolBatch("question", llm.ToolCall{ID: callID, Name: "ask_question", Input: input})
}

func RuntimeError(err error) Step {
	return Step{Err: err}
}

func Cancellation() Step {
	return Step{Cancel: true}
}

func (c *ScriptedClient) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, req)
	if len(c.steps) == 0 {
		return llm.Response{}, ErrScriptedRuntime
	}
	step := c.steps[0]
	c.steps = c.steps[1:]
	if step.Cancel {
		if err := ctx.Err(); err != nil {
			return llm.Response{}, err
		}
		return llm.Response{}, context.Canceled
	}
	if step.Err != nil {
		return llm.Response{}, step.Err
	}
	return step.Response, nil
}

func (c *ScriptedClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.caps.ProviderID == "" {
		return llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true}, nil
	}
	return c.caps, nil
}

func (c *ScriptedClient) Requests() []llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]llm.Request(nil), c.calls...)
}

func (c *ScriptedClient) RemainingSteps() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.steps)
}

var _ llm.Client = (*ScriptedClient)(nil)
var _ llm.ProviderCapabilitiesClient = (*ScriptedClient)(nil)
