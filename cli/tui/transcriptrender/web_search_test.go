package transcriptrender

import (
	"slices"
	"strings"
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/toolspec"
	"core/shared/transcriptmodel"
	"google.golang.org/protobuf/proto"
)

func TestWebOpenPreviewUsesActionURL(t *testing.T) {
	const target = "https://github.com/respawn-llc/kent/issues/1071"
	row := &transcriptpb.CommittedRow{
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			ToolName: proto.String(string(toolspec.ToolWebSearch)),
			Presentation: &transcriptpb.ToolPresentation{
				Command: proto.String("unrelated metadata"),
			},
			WebSearch: &transcriptpb.WebSearchDetail{
				Action: &transcriptpb.WebSearchDetail_OpenPage{
					OpenPage: &transcriptpb.WebSearchOpenPage{Url: proto.String(target)},
				},
			},
		}},
	}
	for _, mode := range []Mode{ModeOngoing, ModeOngoingCollapsed, ModeDetailCollapsed} {
		rendered := RenderCommittedRow(row, 160, "dark", mode)
		var words []string
		for _, line := range rendered.Lines {
			for _, span := range line.Spans {
				words = append(words, strings.Fields(span.Text)...)
			}
		}
		if !slices.Contains(words, target) {
			t.Fatalf("mode %v lost the opened URL: %v", mode, words)
		}
	}
}

func TestWebSearchDisplayTextPreservesQueryQuotes(t *testing.T) {
	query := `site:ghostty.org/docs/config/reference "cmd" keybind Ghostty`
	text, ok := webSearchDisplayText(toolMeta{
		ToolCallMeta: transcript.ToolCallMeta{
			ToolName: string(toolspec.ToolWebSearch),
			Command:  query,
		},
	})
	if !ok {
		t.Fatal("web search display text not recognized")
	}

	want := webSearchDisplayPrefix + `"` + query + `"`
	if text != want {
		t.Fatalf("web search display text = %q, want unescaped query quotes", text)
	}
}

func TestWebPreviewUsesCompletedSearchAndFindTargets(t *testing.T) {
	const target = "https://example.com/reference"
	for _, tc := range []struct {
		name   string
		detail *transcriptpb.WebSearchDetail
		want   []string
	}{
		{
			name: "search",
			detail: &transcriptpb.WebSearchDetail{
				Action: &transcriptpb.WebSearchDetail_Search{
					Search: &transcriptpb.WebSearchSearch{Queries: []string{"first", "second"}},
				},
			},
			want: []string{`"first;`, `second"`},
		},
		{
			name: "find",
			detail: &transcriptpb.WebSearchDetail{
				Action: &transcriptpb.WebSearchDetail_FindInPage{
					FindInPage: &transcriptpb.WebSearchFindInPage{
						Url: proto.String(target), Pattern: proto.String("needle"),
					},
				},
			},
			want: []string{target, `"needle"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := &transcriptpb.ToolRow{WebSearch: tc.detail}
			meta := toolMeta{ToolCallMeta: transcript.ToolCallMeta{
				ToolName: string(toolspec.ToolWebSearch),
				Command:  "stale",
			}}
			for _, mode := range []Mode{ModeOngoing, ModeOngoingCollapsed, ModeDetailCollapsed} {
				words := strings.Fields(toolDisplayText(row, meta, mode).Text)
				for _, target := range tc.want {
					if !slices.Contains(words, target) {
						t.Fatalf("mode %v lost completed action target %q: %v", mode, target, words)
					}
				}
			}
		})
	}
}
