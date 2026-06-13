package app

import (
	"encoding/binary"
	"hash/fnv"
	"strings"

	"core/cli/tui"
)

func renderNativePendingToolSnapshot(entries []tui.TranscriptEntry, theme string, width int, spinnerFrame int) string {
	pending := tui.PendingToolEntries(entries)
	if len(pending) == 0 {
		return ""
	}
	return renderNativePendingOngoingSnapshot(pending, theme, width, spinnerFrame)
}

func renderNativePendingOngoingSnapshot(entries []tui.TranscriptEntry, theme string, width int, spinnerFrame int) string {
	if len(entries) == 0 {
		return ""
	}
	return renderStyledNativeProjectionLines(tui.RenderPendingOngoingSnapshotLinesWithSpinnerFrames(entries, theme, width, func(entry tui.TranscriptEntry, entryIndex int) string {
		return pendingToolSpinnerFrame(pendingToolSpinnerFrameForEntry(spinnerFrame, entry, entryIndex))
	}), theme, width)
}

func pendingToolSpinnerFrameForEntry(baseFrame int, entry tui.TranscriptEntry, entryIndex int) int {
	frameCount := len(pendingToolSpinner.Frames)
	if frameCount <= 1 {
		return 0
	}
	offset := pendingToolSpinnerFrameOffset(entry, frameCount)
	frame := (baseFrame + offset) % frameCount
	if frame < 0 {
		return 0
	}
	return frame
}

func pendingToolSpinnerFrameOffset(entry tui.TranscriptEntry, frameCount int) int {
	if frameCount <= 1 {
		return 0
	}
	hasher := fnv.New32a()
	writePendingToolSpinnerSeed(hasher, entry)
	return int(hasher.Sum32() % uint32(frameCount))
}

func writePendingToolSpinnerSeed(hasher interface{ Write([]byte) (int, error) }, entry tui.TranscriptEntry) {
	if id := strings.TrimSpace(entry.ToolCallID); id != "" {
		writePendingToolSpinnerSeedPart(hasher, "tool_call_id")
		writePendingToolSpinnerSeedPart(hasher, id)
		return
	}
	writePendingToolSpinnerSeedPart(hasher, "tool_call_fallback")
	writePendingToolSpinnerSeedPart(hasher, string(entry.Role))
	writePendingToolSpinnerSeedPart(hasher, strings.TrimSpace(entry.Text))
	writePendingToolSpinnerSeedPart(hasher, strings.TrimSpace(entry.SourcePath))
	writePendingToolSpinnerSeedPart(hasher, strings.TrimSpace(entry.CompactLabel))
	if entry.ToolCall != nil {
		writePendingToolSpinnerSeedPart(hasher, strings.TrimSpace(entry.ToolCall.ToolName))
		writePendingToolSpinnerSeedPart(hasher, string(entry.ToolCall.Presentation))
		writePendingToolSpinnerSeedPart(hasher, string(entry.ToolCall.RenderBehavior))
		writePendingToolSpinnerSeedPart(hasher, strings.TrimSpace(entry.ToolCall.Command))
		writePendingToolSpinnerSeedPart(hasher, strings.TrimSpace(entry.ToolCall.CompactText))
		writePendingToolSpinnerSeedPart(hasher, strings.TrimSpace(entry.ToolCall.Question))
		writePendingToolSpinnerSeedPart(hasher, strings.TrimSpace(entry.ToolCall.InlineMeta))
	}
}

func writePendingToolSpinnerSeedPart(hasher interface{ Write([]byte) (int, error) }, part string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(part)))
	_, _ = hasher.Write(length[:])
	_, _ = hasher.Write([]byte(part))
}
