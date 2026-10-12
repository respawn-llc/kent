package app

import (
	"core/cli/app/internal/status"
	"testing"
)

func TestStatusSubscriptionRemainingClampsUsedPercent(t *testing.T) {
	tests := []struct {
		name string
		used float64
		want float64
	}{
		{name: "negative used", used: -10, want: 100},
		{name: "partial used", used: 25, want: 75},
		{name: "over used", used: 150, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusSubscriptionRemaining(tt.used)
			if got != tt.want {
				t.Fatalf("remaining = %.1f, want %.1f", got, tt.want)
			}
		})
	}
}

func TestStatusSubscriptionBarWidthForLine(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		label    string
		leftText string
		metaText string
		want     int
	}{
		{name: "wide clamps to max", width: 120, label: "weekly", leftText: "75% left", want: statusSubscriptionBarMaxWidth},
		{name: "narrow keeps minimum", width: 20, label: "weekly", leftText: "75% left", want: 4},
		{name: "metadata consumes space", width: 40, label: "weekly", leftText: "75% left", metaText: "resets soon", want: 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusSubscriptionBarWidthForLine(tt.width, tt.label, tt.leftText, tt.metaText)
			if got != tt.want {
				t.Fatalf("bar width = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStatusSubscriptionBarWidthBounds(t *testing.T) {
	if got := statusSubscriptionBarWidth(3); got != 4 {
		t.Fatalf("small width = %d, want 4", got)
	}
	if got := statusSubscriptionBarWidth(500); got != statusSubscriptionBarMaxWidth {
		t.Fatalf("wide width = %d, want %d", got, statusSubscriptionBarMaxWidth)
	}
}

func TestGrokSubscriptionBarSegmentsUseTotalAndCompleteContributions(t *testing.T) {
	floatPointer := func(value float64) *float64 { return &value }
	tests := []struct {
		name   string
		window uiStatusSubscriptionWindow
		want   grokSubscriptionBarSegments
	}{
		{
			name: "complete breakdown",
			window: uiStatusSubscriptionWindow{
				UsedPercent: 86,
				GrokContributions: &status.GrokUsageContribution{
					BuildUsedPercent: floatPointer(67),
					ChatUsedPercent:  floatPointer(19),
				},
			},
			want: grokSubscriptionBarSegments{BuildOrTotalCells: 12, ChatCells: 3, EmptyCells: 3, Stacked: true},
		},
		{
			name:   "missing breakdown",
			window: uiStatusSubscriptionWindow{UsedPercent: 86},
			want:   grokSubscriptionBarSegments{BuildOrTotalCells: 15, EmptyCells: 3},
		},
		{
			name: "incomplete breakdown",
			window: uiStatusSubscriptionWindow{
				UsedPercent: 86,
				GrokContributions: &status.GrokUsageContribution{
					BuildUsedPercent: floatPointer(67),
				},
			},
			want: grokSubscriptionBarSegments{BuildOrTotalCells: 15, EmptyCells: 3},
		},
		{
			name: "contributions do not account for total",
			window: uiStatusSubscriptionWindow{
				UsedPercent: 86,
				GrokContributions: &status.GrokUsageContribution{
					BuildUsedPercent: floatPointer(66),
					ChatUsedPercent:  floatPointer(19),
				},
			},
			want: grokSubscriptionBarSegments{BuildOrTotalCells: 15, EmptyCells: 3},
		},
		{
			name: "explicit zero usage",
			window: uiStatusSubscriptionWindow{
				UsedPercent: 0,
				GrokContributions: &status.GrokUsageContribution{
					BuildUsedPercent: floatPointer(0),
					ChatUsedPercent:  floatPointer(0),
				},
			},
			want: grokSubscriptionBarSegments{EmptyCells: statusSubscriptionBarMaxWidth, Stacked: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := grokSubscriptionBarSegmentsForWindow(statusSubscriptionBarMaxWidth, tt.window)
			if got != tt.want {
				t.Fatalf("segments = %+v, want %+v", got, tt.want)
			}
		})
	}
}
