package transcriptrender

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"strings"
	"testing"
	"unicode"
)

func TestUserAndAssistantRowsAlwaysRenderTheirFullSource(t *testing.T) {
	condensed := "forbidden compact preview"
	source := "Complete first paragraph with enough words to wrap.\n\nComplete second paragraph."
	rows := []*transcriptpb.CommittedRow{
		{Row: &transcriptpb.CommittedRow_User{User: &transcriptpb.UserRow{
			Text:          source,
			CondensedText: &condensed,
		}}},
		{Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{
			Text:          source,
			CondensedText: &condensed,
			Phase:         transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
		}}},
	}
	modes := []Mode{
		ModeOngoing,
		ModeOngoingCollapsed,
		ModeOngoingFull,
		ModeOngoingStable,
		ModeDetailCollapsed,
		ModeDetailExpanded,
	}

	for _, row := range rows {
		role := StyleRoleUser
		if GroupForRow(row) == GroupAssistant {
			role = StyleRoleAssistant
		}
		for _, mode := range modes {
			rendered := RenderCommittedRow(row, 24, "dark", mode)
			text := strings.Join(PlainLines(rendered.Lines), "\n")
			var sourceText strings.Builder
			for _, line := range rendered.Lines {
				for _, span := range line.Spans {
					spanRole, ok := span.Style.Role()
					if ok && spanRole == role {
						sourceText.WriteString(span.Text)
					}
				}
			}
			compactText := strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, sourceText.String())
			compactSource := strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, source)
			if !strings.Contains(compactText, compactSource) {
				t.Fatalf("group=%d mode=%d omitted source content: %q", GroupForRow(row), mode, text)
			}
			if strings.Contains(text, condensed) || strings.Contains(text, "…") {
				t.Fatalf("group=%d mode=%d used compact or ellipsized content: %q", GroupForRow(row), mode, text)
			}
		}
	}
}
