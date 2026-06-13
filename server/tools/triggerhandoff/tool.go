package triggerhandoff

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"core/server/llm"
	"core/server/tools"
)

type Controller interface {
	TriggerHandoff(ctx context.Context, stepID string, activeCall llm.ToolCall, summarizerPrompt string, futureAgentMessage string) (string, bool, error)
}

type ResultPayload struct {
	Summary                 string `json:"summary"`
	FutureAgentMessageAdded bool   `json:"future_agent_message_added,omitempty"`
}

type input struct {
	SummarizerPrompt   string `json:"summarizer_prompt,omitempty"`
	FutureAgentMessage string `json:"future_agent_message,omitempty"`
}

type Tool struct {
	getController func() Controller
}

func New(getController func() Controller) *Tool {
	return &Tool{getController: getController}
}

func (t *Tool) Call(ctx context.Context, c tools.Call) (tools.Result, error) {
	if t == nil || t.getController == nil {
		return tools.ErrorResult(c, "trigger_handoff controller is unavailable"), nil
	}
	controller := t.getController()
	if controller == nil {
		return tools.ErrorResult(c, "trigger_handoff controller is unavailable"), nil
	}

	var in input
	if len(c.Input) > 0 {
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return tools.ErrorResult(c, fmt.Sprintf("invalid trigger_handoff input: %v. Provide an object with optional string fields `summarizer_prompt` and `future_agent_message`.", err)), nil
		}
	}

	summary, futureAgentMessageAdded, err := controller.TriggerHandoff(
		ctx,
		c.StepID,
		llm.ToolCall{ID: c.ID, Name: string(c.Name), Input: append(json.RawMessage(nil), c.Input...)},
		strings.TrimSpace(in.SummarizerPrompt),
		strings.TrimSpace(in.FutureAgentMessage),
	)
	if err != nil {
		message := strings.TrimSpace(err.Error())
		if message == "" {
			message = "trigger_handoff failed"
		} else {
			message = "trigger_handoff failed: " + message
		}
		return tools.ErrorResult(c, message+". Retry only after the developer compaction reminder is present, or keep working if the user disabled handoff."), nil
	}
	payload := ResultPayload{Summary: summary, FutureAgentMessageAdded: futureAgentMessageAdded}
	if strings.TrimSpace(payload.Summary) == "" {
		payload.Summary = "Handoff triggered."
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{CallID: c.ID, Name: c.Name, Output: body}, nil
}
