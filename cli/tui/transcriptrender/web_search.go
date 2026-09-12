package transcriptrender

import (
	"fmt"
	"strings"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

const webSearchImageLabel = "image"

func webSearchDetailLines(detail *transcriptpb.WebSearchDetail, width int, presentation MarkdownLinkPresentation) []Line {
	var lines []Line
	appendItem := func(label string, destination *string) {
		style := markdownInlineStyle{}
		if destination != nil {
			style.Underline = true
			style.Hyperlink = markdownHyperlink(*destination)
		}
		builder := markdownLineBuilder{role: StyleRoleToolWebSearch, linkPresentation: presentation}
		for index, text := range strings.Split(safeTranscriptText(label), "\n") {
			if index > 0 {
				builder.breakLine()
			}
			builder.append(text, style)
		}
		if destination != nil && label != *destination {
			appendMarkdownLinkDestination(&builder, markdownInlineStyle{}, style)
		}
		var item []Line
		for _, line := range builder.finish() {
			item = append(item, wrapStyledLine(line.Spans, max(1, width-2))...)
		}
		lines = append(lines, prefixMarkdownLines(item, "• ", StyleRoleToolWebSearch, false)...)
	}
	switch action := detail.Action.(type) {
	case *transcriptpb.WebSearchDetail_Search:
		for _, query := range action.Search.Queries {
			appendItem(query, nil)
		}
	case *transcriptpb.WebSearchDetail_OpenPage:
		if action.OpenPage.Url != nil {
			appendItem(*action.OpenPage.Url, action.OpenPage.Url)
		}
	case *transcriptpb.WebSearchDetail_FindInPage:
		if action.FindInPage.Url != nil {
			appendItem(*action.FindInPage.Url, action.FindInPage.Url)
		}
		if action.FindInPage.Pattern != nil {
			appendItem(*action.FindInPage.Pattern, nil)
		}
	default:
		panic(fmt.Sprintf("render unsupported web search action %T", action))
	}
	for _, result := range detail.Results {
		var label string
		switch result.Kind {
		case transcriptpb.WebSearchResultKind_WEB_SEARCH_RESULT_KIND_IMAGE:
			label = webSearchImageLabel
		case transcriptpb.WebSearchResultKind_WEB_SEARCH_RESULT_KIND_LINK:
			if result.Title != nil {
				label = *result.Title
			} else {
				label = result.GetDestination()
			}
		default:
			panic(fmt.Sprintf("render unsupported web search result kind %v", result.Kind))
		}
		appendItem(label, result.Destination)
	}
	for _, source := range detail.Sources {
		appendItem(source, &source)
	}
	return lines
}
