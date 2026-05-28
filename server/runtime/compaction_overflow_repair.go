package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"builder/server/llm"
	"builder/shared/toolspec"
)

const (
	compactionOverflowRepairAttemptTokens = 20_000
	compactionOverflowCollapsedText       = "<collapsed>"
)

type compactionOverflowRepairStats struct {
	ShellOutputsCollapsed int
	PatchInputsCollapsed  int
	EstimatedSavedTokens  int
}

func (s compactionOverflowRepairStats) Collapsed() bool {
	return s.ShellOutputsCollapsed > 0 || s.PatchInputsCollapsed > 0 || s.EstimatedSavedTokens > 0
}

func (s compactionOverflowRepairStats) Add(other compactionOverflowRepairStats) compactionOverflowRepairStats {
	return compactionOverflowRepairStats{
		ShellOutputsCollapsed: s.ShellOutputsCollapsed + other.ShellOutputsCollapsed,
		PatchInputsCollapsed:  s.PatchInputsCollapsed + other.PatchInputsCollapsed,
		EstimatedSavedTokens:  s.EstimatedSavedTokens + other.EstimatedSavedTokens,
	}
}

func compactionOverflowRepairDiagnosticText(stats compactionOverflowRepairStats) string {
	return fmt.Sprintf(
		"Context compaction succeeded after collapsing tool payloads: %d shell outputs, %d patch inputs, ~%d tokens omitted. Full original tool payloads remain in pre-compaction transcript history but are omitted from the compacted model context.",
		stats.ShellOutputsCollapsed,
		stats.PatchInputsCollapsed,
		stats.EstimatedSavedTokens,
	)
}

func collapseCompactionOverflowToolPayloads(items []llm.ResponseItem, attemptNumber int) ([]llm.ResponseItem, compactionOverflowRepairStats) {
	return collapseCompactionOverflowToolPayloadsAfterSavings(items, attemptNumber, 0)
}

func collapseCompactionOverflowToolPayloadsAfterSavings(items []llm.ResponseItem, attemptNumber int, existingSavedTokens int) ([]llm.ResponseItem, compactionOverflowRepairStats) {
	out := llm.CloneResponseItems(items)
	if attemptNumber <= 0 || len(out) == 0 {
		return out, compactionOverflowRepairStats{}
	}
	targetSavedTokens := attemptNumber * compactionOverflowRepairAttemptTokens
	currentSavedTokens := existingSavedTokens
	if currentSavedTokens >= targetSavedTokens {
		return out, compactionOverflowRepairStats{}
	}

	callTools := compactionOverflowRepairCallTools(out)
	stats := compactionOverflowRepairStats{}
	for idx := range out {
		if currentSavedTokens >= targetSavedTokens {
			break
		}
		item := out[idx]
		switch item.Type {
		case llm.ResponseItemTypeFunctionCallOutput:
			if !isCompactionOverflowRepairShellOutputTool(compactionOverflowRepairToolID(item, callTools)) || isCollapsedCompactionOverflowShellOutput(item.Output) {
				continue
			}
			replacement, saved := collapsedCompactionOverflowShellOutput(item.Output)
			if saved <= 0 {
				continue
			}
			out[idx].Output = replacement
			stats.ShellOutputsCollapsed++
			stats.EstimatedSavedTokens += saved
			currentSavedTokens += saved
		case llm.ResponseItemTypeCustomToolCall:
			if compactionOverflowRepairToolID(item, callTools) != toolspec.ToolPatch || isCollapsedCompactionOverflowPatchInput(item.CustomInput) {
				continue
			}
			replacement, saved := collapsedCompactionOverflowPatchInput(item.CustomInput)
			if saved <= 0 {
				continue
			}
			out[idx].CustomInput = replacement
			stats.PatchInputsCollapsed++
			stats.EstimatedSavedTokens += saved
			currentSavedTokens += saved
		}
	}
	return out, stats
}

func compactionOverflowRepairCallTools(items []llm.ResponseItem) map[string]toolspec.ID {
	out := make(map[string]toolspec.ID, len(items))
	for _, item := range items {
		if item.Type != llm.ResponseItemTypeFunctionCall && item.Type != llm.ResponseItemTypeCustomToolCall {
			continue
		}
		id := compactionOverflowRepairCallID(item)
		toolID, ok := toolspec.ParseID(item.Name)
		if id == "" || !ok {
			continue
		}
		out[id] = toolID
	}
	return out
}

func compactionOverflowRepairCallID(item llm.ResponseItem) string {
	if callID := strings.TrimSpace(item.CallID); callID != "" {
		return callID
	}
	return strings.TrimSpace(item.ID)
}

func compactionOverflowRepairToolID(item llm.ResponseItem, callTools map[string]toolspec.ID) toolspec.ID {
	if toolID, ok := toolspec.ParseID(item.Name); ok {
		return toolID
	}
	if callTools == nil {
		return ""
	}
	return callTools[compactionOverflowRepairCallID(item)]
}

func isCompactionOverflowRepairShellOutputTool(toolID toolspec.ID) bool {
	return toolID == toolspec.ToolExecCommand || toolID == toolspec.ToolWriteStdin
}

func collapsedCompactionOverflowShellOutput(output json.RawMessage) (json.RawMessage, int) {
	before := estimateTextTokens(string(output))
	if outputTokens, ok := estimateStructuredOutputTokens(output); ok {
		before = outputTokens
	}
	replacement := shellOutputCollapsePayload()
	after := estimateTextTokens(string(replacement))
	saved := before - after
	if saved <= 0 {
		return nil, 0
	}
	return replacement, saved
}

func shellOutputCollapsePayload() json.RawMessage {
	data, err := json.Marshal(compactionOverflowCollapsedText)
	if err != nil {
		return json.RawMessage(`"<collapsed>"`)
	}
	return data
}

func isCollapsedCompactionOverflowShellOutput(output json.RawMessage) bool {
	var payload string
	return json.Unmarshal(output, &payload) == nil && payload == compactionOverflowCollapsedText
}

func collapsedCompactionOverflowPatchInput(input string) (string, int) {
	before := estimateTextTokens(input)
	replacement := patchInputCollapsePayload()
	after := estimateTextTokens(replacement)
	saved := before - after
	if saved <= 0 {
		return "", 0
	}
	return replacement, saved
}

func patchInputCollapsePayload() string {
	return compactionOverflowCollapsedText
}

func isCollapsedCompactionOverflowPatchInput(input string) bool {
	return input == compactionOverflowCollapsedText
}
