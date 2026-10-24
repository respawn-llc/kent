package llm

import (
	"context"
	"strings"

	"core/shared/textutil"
	"github.com/openai/openai-go/v3/responses"
)

const compactionDiagnosticItemLimit = 32

func lenOptionalString(value *string) int {
	if value == nil {
		return 0
	}
	return len(*value)
}

// Retain only bounded metadata, never provider text, reasoning, or raw payloads.
type compactionOutputItemDiagnostic struct {
	Type      string  `json:"type"`
	Phase     *string `json:"phase"`
	TextBytes int     `json:"text_bytes"`
}

type compactionOutputDiagnostic struct {
	terminalEvent    string
	status           *string
	incompleteReason *string
	itemCount        int
	items            []compactionOutputItemDiagnostic
	textBytes        int
	refusalPresent   bool
}

func describeCompactionOutput(eventType string, response responses.Response) *compactionOutputDiagnostic {
	output := &compactionOutputDiagnostic{
		terminalEvent: boundedDiagnosticMetadata(eventType),
		itemCount:     len(response.Output),
		items:         make([]compactionOutputItemDiagnostic, 0, min(len(response.Output), compactionDiagnosticItemLimit)),
	}
	if response.JSON.Status.Valid() {
		output.status = textutil.Value(boundedDiagnosticMetadata(string(response.Status)))
	}
	if response.JSON.IncompleteDetails.Valid() && response.IncompleteDetails.JSON.Reason.Valid() {
		output.incompleteReason = textutil.Value(boundedDiagnosticMetadata(response.IncompleteDetails.Reason))
	}
	for index, item := range response.Output {
		detail := compactionOutputItemDiagnostic{Type: boundedDiagnosticMetadata(item.Type)}
		if item.JSON.Phase.Valid() {
			detail.Phase = textutil.Value(boundedDiagnosticMetadata(string(item.Phase)))
		}
		for _, part := range item.Content {
			switch part.Type {
			case "output_text":
				detail.TextBytes += len(part.Text)
			case "refusal":
				output.refusalPresent = true
			}
		}
		output.textBytes += detail.TextBytes
		if index < compactionDiagnosticItemLimit {
			output.items = append(output.items, detail)
		}
	}
	return output
}

func (a *providerAttemptDiagnostic) diagnoseEmptyLocalCompaction(ctx context.Context, provider ResponsesResponse, normalized Response) {
	if a == nil || !a.localCompaction || a.compactionOutput == nil || len(normalized.ToolCalls) > 0 {
		return
	}
	summaryBytes := 0
	if normalized.Assistant.Content != nil {
		summaryBytes = len(strings.TrimSpace(*normalized.Assistant.Content))
	}
	if summaryBytes > 0 {
		return
	}
	items := make([]compactionOutputItemDiagnostic, 0, min(len(normalized.OutputItems), compactionDiagnosticItemLimit))
	for _, item := range normalized.OutputItems[:min(len(normalized.OutputItems), compactionDiagnosticItemLimit)] {
		var phase *string
		if item.Phase != nil {
			phase = textutil.Value(string(*item.Phase))
		}
		items = append(items, compactionOutputItemDiagnostic{
			Type: string(item.Type), Phase: phase, TextBytes: lenOptionalString(item.Content),
		})
	}
	output := a.compactionOutput
	usage := Usage{}
	if a.usage != nil {
		usage = *a.usage
	}
	a.logger.WarnContext(ctx, "empty local compaction diagnostic",
		"kind", "empty_local_compaction",
		"response_id", diagnosticOptional(a.responseID),
		"provider_terminal_event", output.terminalEvent,
		"provider_status", diagnosticOptional(output.status),
		"provider_incomplete_reason", diagnosticOptional(output.incompleteReason),
		"provider_output_count", output.itemCount,
		"provider_output_items", output.items,
		"provider_output_truncated", output.itemCount > len(output.items),
		"provider_text_bytes", output.textBytes,
		"provider_refusal_present", output.refusalPresent,
		"transport_assistant_bytes", lenOptionalString(provider.AssistantText),
		"normalized_output_count", len(normalized.OutputItems),
		"normalized_output_items", items,
		"normalized_output_truncated", len(normalized.OutputItems) > len(items),
		"normalized_assistant_bytes", lenOptionalString(normalized.Assistant.Content),
		"extracted_summary_bytes", summaryBytes,
		"usage_reported", a.usage != nil,
		"input_tokens", diagnosticOptional(usage.InputTokens),
		"cached_input_tokens", diagnosticOptional(usage.CachedInputTokens),
		"output_tokens", diagnosticOptional(usage.OutputTokens),
		"reasoning_tokens", diagnosticOptional(a.reasoningTokens),
		"total_tokens", diagnosticOptional(a.totalTokens),
	)
}

func boundedDiagnosticMetadata(value string) string {
	return value[:min(len(value), 64)]
}
