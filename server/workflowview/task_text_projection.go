package workflowview

import (
	"strings"

	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

func bodyPreview(body string) string {
	trimmed := strings.TrimSpace(body)
	const limit = 96
	if len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit]
}

func markdownPreview(body string) *taskpb.MarkdownPreview {
	trimmed := strings.TrimSpace(body)
	const codePointLimit = 512
	codePointCount := 0
	for byteIndex := range trimmed {
		if codePointCount == codePointLimit {
			return &taskpb.MarkdownPreview{Markdown: trimmed[:byteIndex], Truncated: true}
		}
		codePointCount++
	}
	return &taskpb.MarkdownPreview{Markdown: trimmed}
}
