package runtime

import (
	"encoding/json"
	"fmt"

	"core/server/tools"
	"core/shared/toolspec"
	"core/shared/transcript"
)

// missingToolOutputRepairWarningTemplate is the operator-facing notice appended
// when the repair closes one or more interrupted tool calls.
const missingToolOutputRepairWarningTemplate = "Closed %d interrupted tool call(s) with a synthetic result to repair the transcript after a provider error"

// missingToolOutputInterruptedOutput is the honest result recorded for a tool
// call that was left unanswered (typically interrupted) and can no longer be
// re-executed. It tells the model the call never produced a result rather than
// fabricating a successful one or silently erasing the call from history.
var missingToolOutputInterruptedOutput = json.RawMessage(`{"error":"Tool execution was interrupted before a result was produced. No output is available for this call."}`)

// danglingToolCall identifies a persisted tool call that lacks an output.
type danglingToolCall struct {
	callID string
	name   string
}

// repairMissingToolOutputsByAppending closes any tool calls in the live
// projection that lack an output by appending an honest synthetic tool
// completion for each, plus one operator-facing warning. It returns the number
// of calls repaired.
//
// This is append-only: it persists new tool_completed events through the normal
// steering/completion path and never rewrites or removes existing history, so
// the prompt-cache prefix through each repaired call stays intact. The provider
// output item materialized for each completion automatically matches the
// original call kind (function vs custom) via the projection.
//
// It is a fallback for the resume path, which re-executes interrupted tool calls
// to obtain real outputs; when there are still pending tool-call starts to
// re-execute, this no-ops so it never pre-empts a real result.
func (e *Engine) repairMissingToolOutputsByAppending(stepID string) (int, error) {
	if e == nil || e.store == nil {
		return 0, nil
	}
	if e.pendingToolCallStartStore().Len() > 0 {
		return 0, nil
	}
	chat := e.transcriptRuntimeState().chatProjection()
	if chat == nil {
		return 0, nil
	}
	dangling := chat.danglingToolCalls()
	if len(dangling) == 0 {
		return 0, nil
	}
	intents := make([]steeringIntent, 0, len(dangling)+1)
	for _, call := range dangling {
		intents = append(intents, steerToolCompletionIntent(tools.Result{
			CallID:  call.callID,
			Name:    toolspec.ID(call.name),
			IsError: true,
			Output:  append(json.RawMessage(nil), missingToolOutputInterruptedOutput...),
		}))
	}
	intents = append(intents, steerLocalEntryIntent(storedLocalEntry{
		Visibility: transcript.EntryVisibilityAll,
		Role:       string(transcript.EntryRoleDeveloperErrorFeedback),
		Text:       fmt.Sprintf(missingToolOutputRepairWarningTemplate, len(dangling)),
	}))
	if err := e.steer(stepID, intents...); err != nil {
		return 0, err
	}
	return len(dangling), nil
}
