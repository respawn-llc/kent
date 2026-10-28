package ongoing_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"core/cli/tui/ongoing"

	"github.com/charmbracelet/x/ansi"
)

func TestMutableBandRepaintKeepsNativePromptContiguous(t *testing.T) {
	for _, initialHeight := range []int{3, 6} {
		t.Run(strconv.Itoa(initialHeight)+" initial rows", func(t *testing.T) {
			testMutableBandPrompt(t, initialHeight)
		})
	}
}

func testMutableBandPrompt(t *testing.T, initialHeight int) {
	t.Helper()
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	lines := []string{"one", "two", "three", "four", "five", "six"}
	frame := ongoing.FrameInput{
		Size: ongoing.Size{Width: 80, Height: 18},
		Sections: []ongoing.FrameSection{{
			Kind:  ongoing.FrameSectionInput,
			Lines: lines[:initialHeight],
		}},
		Cursor: ongoing.Cursor{Visible: true, Row: 16, Column: 3},
	}
	if _, err := surface.Render(frame); err != nil {
		t.Fatal(err)
	}
	frame.Sections[0].Lines = lines
	if _, err := surface.Render(frame); err != nil {
		t.Fatal(err)
	}
	frame.Sections[0].Lines = lines[:3]
	if _, err := surface.Render(frame); err != nil {
		t.Fatal(err)
	}

	type promptKind uint8
	const (
		primary promptKind = iota + 1
		secondary
		outputStart
	)
	row := 1
	marks := make(map[int]promptKind)
	parser := ansi.NewParser()
	parser.SetHandler(ansi.Handler{
		HandleCsi: func(command ansi.Cmd, params ansi.Params) {
			switch command.Final() {
			case 'H', 'f':
				row, _, _ = params.Param(0, 1)
			case 'J':
				mode, _, _ := params.Param(0, 0)
				if mode == 0 {
					for marked := range marks {
						if marked > row {
							delete(marks, marked)
						}
					}
				}
			}
		},
		HandleOsc: func(command int, data []byte) {
			if command != 133 {
				return
			}
			fields := strings.Split(string(data), ";")
			if len(fields) < 2 {
				t.Fatal("invalid semantic prompt command")
			}
			switch fields[1] {
			case "A":
				marks[row] = primary
				for _, option := range fields[2:] {
					key, value, present := strings.Cut(option, "=")
					if present && key == "k" && value == "s" {
						marks[row] = secondary
					}
				}
			case "C":
				marks[row] = outputStart
			}
		},
	})
	parser.Parse(output.Bytes())

	const retainedStart = 13
	for cursor := retainedStart; cursor <= frame.Size.Height; cursor++ {
		found := false
		for row := cursor; row >= retainedStart; row-- {
			if kind, present := marks[row]; present {
				if row != retainedStart || kind != primary {
					t.Fatalf("native resize scan from row %d stops inside the live band at row %d (kind=%v)", cursor, row, kind)
				}
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("native resize scan from row %d cannot find the live band", cursor)
		}
	}
	for row, kind := range marks {
		if row < retainedStart && (kind == primary || kind == secondary) {
			t.Fatalf("immutable row %d belongs to the redrawable prompt", row)
		}
	}
}
