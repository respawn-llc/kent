package tui

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"testing"
)

func TestExpandingDetailEntryPreservesCameraPosition(t *testing.T) {
	m := NewModel()
	m.mode = ModeDetail
	m.viewportLines = 3
	rows := []*transcriptpb.CommittedRow{
		detailExpansionNotice("one", "one"),
		detailExpansionNotice("two", "two"),
		detailExpansionNotice("preview", "preview\nbelow\nbelow\nbelow\nbelow"),
		detailExpansionNotice("after", "after"),
		detailExpansionNotice("later", "later"),
	}
	m.detailProjection.replaceSnapshot(rows, m.detailContentWidth(), m.theme, nil)
	m.detailPageLoaded = true
	m.detailScroll = 2
	m.setSelectedDetailIndex(2)

	beforeLines := len(m.detailProjection.lines)
	m.toggleSelectedDetailEntry()

	if got := len(m.detailProjection.lines); got <= beforeLines {
		t.Fatalf("detail entry did not expand: before=%d after=%d", beforeLines, got)
	}
	if got := m.detailScroll; got != 2 {
		t.Fatalf("detail scroll after expansion = %d, want preserved camera position", got)
	}
	viewport := m.detailProjectedCameraViewport()
	if len(viewport.Lines) == 0 || viewport.Lines[0].EntryIndex != 2 {
		t.Fatalf("expanded entry did not remain at the camera anchor: %+v", viewport.Lines)
	}
}

func detailExpansionNotice(compact, full string) *transcriptpb.CommittedRow {
	return &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			Reason:        transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE,
			Severity:      transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
			LegacyText:    &full,
			CondensedText: &compact,
		}},
	}
}
