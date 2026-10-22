package llm

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// partitionReasoningStatus keeps Thinking Status transient: its Markdown span
// must never reach either the Go TUI or Desktop transcript as a Reasoning Trace.
// Unmatched streaming markup remains trace text until the parser recognizes it;
// consumers replace the trace snapshot at the same coordinate, including empty text.
func partitionReasoningStatus(markdownText string) (string, *ReasoningStatus) {
	source := []byte(markdownText)
	document := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))

	var status *ReasoningStatus
	trace := markdownText
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		emphasis, ok := node.(*ast.Emphasis)
		if !ok || emphasis.Level != 2 || emphasis.Parent() == nil || emphasis.Parent().Type() != ast.TypeBlock {
			return ast.WalkContinue, nil
		}
		content, ok := emphasis.FirstChild().(*ast.Text)
		if !ok || content.NextSibling() != nil {
			return ast.WalkContinue, nil
		}
		value := strings.TrimSpace(string(content.Value(source)))
		if value == "" {
			return ast.WalkContinue, nil
		}
		status = &ReasoningStatus{Text: value}
		// This eligible emphasis has exactly one text child. Its source segment
		// excludes the paired delimiters consumed by Goldmark.
		start := content.Segment.Start - emphasis.Level
		stop := content.Segment.Stop + emphasis.Level
		trace = markdownText[:start] + markdownText[stop:]
		return ast.WalkStop, nil
	})
	return trace, status
}
