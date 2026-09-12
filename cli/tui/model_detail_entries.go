package tui

import (
	"fmt"

	"core/cli/tui/transcriptrender"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

type detailEntry struct {
	rowData          *transcriptpb.CommittedRow
	presentationData transcriptrender.DetailPresentation
}

type detailProjection struct {
	entries          []detailEntry
	lines            []detailProjectedLine
	ranges           []detailLineRange
	linkPresentation transcriptrender.MarkdownLinkPresentation
	compiler         transcriptrender.DetailCompiler
}

func newDetailProjection(
	contentWidth int,
	themeName string,
	linkPresentation transcriptrender.MarkdownLinkPresentation,
) detailProjection {
	return detailProjection{
		linkPresentation: linkPresentation,
		compiler: transcriptrender.NewDetailCompilerWithLinkPresentation(
			contentWidth,
			themeName,
			linkPresentation,
		),
	}
}

func detailEntryFromCommittedRow(
	row *transcriptpb.CommittedRow,
	compiler transcriptrender.DetailCompiler,
) (detailEntry, bool) {
	if !detailCommittedRowVisible(row) {
		return detailEntry{}, false
	}
	return newDetailEntry(row, compiler.Compile(row)), true
}

func detailCommittedRowVisible(row *transcriptpb.CommittedRow) bool {
	switch row.Visibility {
	case transcriptpb.EntryVisibility_ENTRY_VISIBILITY_HIDDEN:
		return false
	case transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL:
	default:
		panic(fmt.Sprintf("detail received committed row with unresolved visibility %q", row.Visibility))
	}
	return true
}

func newDetailEntry(
	row *transcriptpb.CommittedRow,
	presentation transcriptrender.DetailPresentation,
) detailEntry {
	return detailEntry{
		rowData:          row,
		presentationData: presentation,
	}
}

func (entry detailEntry) row() *transcriptpb.CommittedRow {
	return entry.rowData
}

func (entry detailEntry) presentation() transcriptrender.DetailPresentation {
	return entry.presentationData
}

func (p detailProjection) indexOfRow(row *transcriptpb.CommittedRow) (int, bool) {
	match := 0
	found := false
	for index, entry := range p.entries {
		if !TranscriptCommittedRowEqual(entry.row(), row) {
			continue
		}
		if found {
			return 0, false
		}
		match = index
		found = true
	}
	return match, found
}

func sameDetailGroup(left, right detailEntry) bool {
	return transcriptrender.GroupForRow(left.rowData) == transcriptrender.GroupForRow(right.rowData)
}

func (p *detailProjection) replaceSnapshot(
	rows []*transcriptpb.CommittedRow,
	contentWidth int,
	themeName string,
	expanded map[int]struct{},
) {
	if p == nil {
		return
	}
	compiler := p.compiler
	if !compiler.Matches(contentWidth, themeName) {
		compiler = transcriptrender.NewDetailCompilerWithLinkPresentation(
			contentWidth,
			themeName,
			p.linkPresentation,
		)
	}
	entries := make([]detailEntry, 0, len(rows))
	for _, row := range rows {
		if entry, ok := detailEntryFromCommittedRow(row, compiler); ok {
			entries = append(entries, entry)
		}
	}
	p.compiler = compiler
	p.entries = entries
	p.rebuildLines(contentWidth, expanded)
}

func (p *detailProjection) recompile(
	contentWidth int,
	themeName string,
	expanded map[int]struct{},
) {
	if p == nil {
		return
	}
	if p.compiler.Matches(contentWidth, themeName) {
		return
	}
	compiler := transcriptrender.NewDetailCompilerWithLinkPresentation(
		contentWidth,
		themeName,
		p.linkPresentation,
	)
	entries := make([]detailEntry, 0, len(p.entries))
	for _, entry := range p.entries {
		entries = append(entries, newDetailEntry(entry.row(), compiler.Compile(entry.row())))
	}
	p.compiler = compiler
	p.entries = entries
	p.rebuildLines(contentWidth, expanded)
}

func (p *detailProjection) rebuildLines(contentWidth int, expanded map[int]struct{}) {
	if p == nil {
		return
	}
	targetWidth := maxInt(1, contentWidth+1)
	contentWidth = maxInt(0, contentWidth)
	renderWidth := maxInt(1, contentWidth)
	lines := make([]detailProjectedLine, 0, (len(p.entries)+1)*2)
	ranges := make([]detailLineRange, len(p.entries))
	for entryIndex, entry := range p.entries {
		if entryIndex > 0 && !sameDetailGroup(p.entries[entryIndex-1], entry) {
			lines = append(lines, detailProjectedLine{
				Kind:         detailLineGroupSpacer,
				Rail:         detailRailBlank,
				TargetWidth:  targetWidth,
				ContentWidth: contentWidth,
			})
		}
		presentation := entry.presentation()
		entryLines := presentation.Collapsed
		if _, isExpanded := expanded[entryIndex]; isExpanded && presentation.Expandable {
			entryLines = presentation.Expanded
		}
		first := len(lines)
		for _, line := range entryLines {
			lines = append(lines, detailProjectedLine{
				Content:      transcriptrender.TruncateLine(line, renderWidth, false),
				EntryIndex:   entryIndex,
				Kind:         detailLineContent,
				Rail:         detailRailBlank,
				TargetWidth:  targetWidth,
				ContentWidth: contentWidth,
			})
		}
		ranges[entryIndex] = detailLineRange{first: first, last: len(lines) - 1}
	}
	p.lines = lines
	p.ranges = ranges
}

func (p *detailProjection) clear(contentWidth int, themeName string) {
	if p == nil {
		return
	}
	*p = newDetailProjection(
		contentWidth,
		themeName,
		p.linkPresentation,
	)
}
