package transcriptrender

import (
	"fmt"
	"slices"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/theme"
)

type DetailPresentation struct {
	Collapsed  []Line
	Expanded   []Line
	Expandable bool
}

type DetailCompiler struct {
	width            int
	themeName        string
	linkPresentation MarkdownLinkPresentation
	syntax           syntaxProjector
}

func NewDetailCompiler(width int, themeName string) DetailCompiler {
	return NewDetailCompilerWithLinkPresentation(width, themeName, MarkdownLinkLabelOnly)
}

func NewDetailCompilerWithLinkPresentation(
	width int,
	themeName string,
	linkPresentation MarkdownLinkPresentation,
) DetailCompiler {
	if !linkPresentation.Valid() {
		panic(fmt.Sprintf("create detail compiler with invalid Markdown link presentation %d", linkPresentation))
	}
	resolvedTheme := theme.Resolve(themeName)
	return DetailCompiler{
		width:            max(0, width),
		themeName:        resolvedTheme,
		linkPresentation: linkPresentation,
		syntax:           newSyntaxProjector(resolvedTheme),
	}
}

func (c DetailCompiler) Matches(width int, themeName string) bool {
	return c.width == max(0, width) && c.themeName == theme.Resolve(themeName)
}

func (c DetailCompiler) Compile(row *transcriptpb.CommittedRow) DetailPresentation {
	return renderDetailPresentation(
		row,
		max(1, c.width),
		c.syntax,
		c.linkPresentation,
	)
}

func RenderDetailPresentation(row *transcriptpb.CommittedRow, width int, themeName string) DetailPresentation {
	return NewDetailCompiler(width, themeName).Compile(row)
}

func renderDetailPresentation(
	row *transcriptpb.CommittedRow,
	width int,
	syntax syntaxProjector,
	linkPresentation MarkdownLinkPresentation,
) DetailPresentation {
	switch row.Integrity {
	case transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		transcriptpb.RowIntegrity_ROW_INTEGRITY_RECOVERABLE_MALFORMED,
		transcriptpb.RowIntegrity_ROW_INTEGRITY_UNRECOVERABLE_MALFORMED:
	default:
		panic(fmt.Sprintf("render detail presentation with invalid integrity classification: %d", row.Integrity))
	}
	collapsed := renderCommittedRow(
		row,
		width,
		ModeDetailCollapsed,
		&syntax,
		linkPresentation,
	).Lines
	expanded := renderCommittedRow(
		row,
		width,
		ModeDetailExpanded,
		&syntax,
		linkPresentation,
	).Lines
	expandable := !detailLinesEqual(collapsed, expanded)
	if row.Integrity == transcriptpb.RowIntegrity_ROW_INTEGRITY_RECOVERABLE_MALFORMED {
		expandable = true
	}
	if row.Integrity == transcriptpb.RowIntegrity_ROW_INTEGRITY_UNRECOVERABLE_MALFORMED {
		expandable = false
	}
	return DetailPresentation{
		Collapsed:  collapsed,
		Expanded:   expanded,
		Expandable: expandable,
	}
}

func detailLinesEqual(left, right []Line) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !detailLineEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

func detailLineEqual(left, right Line) bool {
	if left.Background != right.Background {
		return false
	}
	if (left.LeadingSymbol == nil) != (right.LeadingSymbol == nil) {
		return false
	}
	if left.LeadingSymbol != nil && *left.LeadingSymbol != *right.LeadingSymbol {
		return false
	}
	return slices.Equal(left.Spans, right.Spans)
}
