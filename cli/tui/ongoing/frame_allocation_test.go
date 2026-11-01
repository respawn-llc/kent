package ongoing

import (
	"bytes"
	"testing"
)

func TestFrameAllocationExposesInputLossWithoutWriting(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	frame := FrameInput{
		Size: Size{Width: 80, Height: 10},
		Sections: []FrameSection{
			{Kind: FrameSectionInput, Lines: make([]string, 9)},
			{Kind: FrameSectionStatus, Lines: []string{"status"}},
		},
	}
	if rows := surface.AllocateFrame(frame).SectionRows(FrameSectionInput); len(rows) != 9 {
		t.Fatalf("exact-fit input coverage = %v", rows)
	}
	frame.Sections = append([]FrameSection{{Kind: FrameSectionPendingTools, Lines: []string{"tool"}}}, frame.Sections...)
	rows := surface.AllocateFrame(frame).SectionRows(FrameSectionInput)
	if len(rows) >= 9 || len(rows) == 0 || rows[len(rows)-1] != 9 {
		t.Fatalf("competing-section input coverage = %v, want retained input tail", rows)
	}
	if output.Len() != 0 {
		t.Fatal("allocation query wrote terminal output")
	}
}
