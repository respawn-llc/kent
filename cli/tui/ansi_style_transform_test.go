package tui

import (
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

func extractForegroundTrueColors(text string) []rgbColor {
	parser := xansi.GetParser()
	defer xansi.PutParser(parser)

	state := byte(0)
	input := text
	colors := make([]rgbColor, 0, 8)
	for len(input) > 0 {
		_, width, n, newState := xansi.GraphemeWidth.DecodeSequenceInString(input, state, parser)
		if n <= 0 {
			break
		}
		state = newState
		input = input[n:]
		if width > 0 || xansi.Cmd(parser.Command()).Final() != 'm' {
			continue
		}
		params := parser.Params()
		for idx := 0; idx < len(params); {
			param, _, ok := params.Param(idx, 0)
			if !ok {
				break
			}
			if param == 38 {
				color, consumed, ok := parseANSIForegroundColor(params, idx)
				if ok {
					colors = append(colors, color)
					idx += consumed
					continue
				}
			}
			idx++
		}
	}
	return colors
}

func containsColor(colors []rgbColor, target rgbColor) bool {
	return countColor(colors, target) > 0
}

func countColor(colors []rgbColor, target rgbColor) int {
	count := 0
	for _, color := range colors {
		if color == target {
			count++
		}
	}
	return count
}

func containsNonPreviewColor(colors []rgbColor, preview rgbColor) bool {
	for _, color := range colors {
		if color != preview {
			return true
		}
	}
	return false
}

func TestApplyANSIStyleIntentsSupportsPrimaryForeground(t *testing.T) {
	primary := rgbColor{r: 1, g: 2, b: 3}
	got := applyANSIStyleIntents("goal", ansiIntentPalette{PrimaryForeground: primary}, PrimaryForeground)
	if !containsColor(extractForegroundTrueColors(got), primary) {
		t.Fatalf("expected primary foreground color %+v, got %q", primary, got)
	}
}
