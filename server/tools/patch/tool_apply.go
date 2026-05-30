package patch

import (
	"fmt"
	"strconv"
	"strings"

	"builder/shared/textutil"
	patchformat "builder/shared/transcript/patchformat"
)

func splitLines(s string) []string {
	s = textutil.NormalizeCRLF(s)
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func applyEdit(original []string, changes []patchformat.ChangeLine) ([]string, error) {
	hunks, err := parseEditHunks(changes)
	if err != nil {
		return nil, err
	}

	current := append([]string(nil), original...)
	cumulativeOffset := 0
	searchFloor := 0

	for idx, h := range hunks {
		expected := -1
		if h.header.hasPosition {
			expected = h.header.oldStart - 1 + cumulativeOffset
		}
		anchor, err := findHunkAnchor(current, h.changes, expected, searchFloor, h.header.hasPosition, h.header.context, h.endOfFile)
		if err != nil {
			return nil, attachFailureReasonContext(err, fmt.Sprintf("hunk %d", idx+1))
		}
		next, oldCount, newCount, err := applyHunkAt(current, h.changes, anchor)
		if err != nil {
			return nil, attachFailureReasonContext(err, fmt.Sprintf("hunk %d", idx+1))
		}
		if h.header.hasPosition {
			if oldCount != h.header.oldCount || newCount != h.header.newCount {
				return nil, malformedFailure(fmt.Sprintf(
					"hunk %d header count mismatch: expected old/new %d/%d, applied %d/%d",
					idx+1,
					h.header.oldCount,
					h.header.newCount,
					oldCount,
					newCount,
				))
			}
		}
		current = next
		cumulativeOffset += newCount - oldCount
		searchFloor = anchor + newCount
	}
	return current, nil
}

func parseEditHunks(changes []patchformat.ChangeLine) ([]editHunk, error) {
	if len(changes) == 0 {
		return nil, nil
	}

	hunks := make([]editHunk, 0, 4)
	current := editHunk{}

	flush := func() error {
		if len(current.changes) == 0 {
			if current.endOfFile {
				hunks = append(hunks, current)
				current = editHunk{}
				return nil
			}
			if current.header.hasPosition {
				return malformedFailure("hunk header without changes")
			}
			return nil
		}
		hunks = append(hunks, current)
		current = editHunk{}
		return nil
	}

	for _, ch := range changes {
		if ch.EndOfFile {
			current.endOfFile = true
			continue
		}
		switch ch.Kind {
		case '@':
			if err := flush(); err != nil {
				return nil, err
			}
			header, err := parseHunkHeader("@" + ch.Content)
			if err != nil {
				return nil, err
			}
			current.header = header
		case ' ', '+', '-':
			current.changes = append(current.changes, ch)
		default:
			return nil, malformedFailure(fmt.Sprintf("unknown change line prefix %q", string(ch.Kind)))
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return hunks, nil
}

func parseHunkHeader(line string) (hunkHeader, error) {
	line = strings.TrimSpace(line)
	if line == "@@" {
		return hunkHeader{}, nil
	}

	m := unifiedHunkHeaderPattern.FindStringSubmatch(line)
	if len(m) == 0 {
		if strings.HasPrefix(line, "@@ ") {
			return hunkHeader{context: strings.TrimPrefix(line, "@@ ")}, nil
		}
		return hunkHeader{}, malformedFailure(fmt.Sprintf("invalid hunk header %q", line))
	}

	oldStart, err := strconv.Atoi(m[1])
	if err != nil {
		return hunkHeader{}, malformedFailure(fmt.Sprintf("invalid hunk old start %q", m[1]))
	}
	oldCount := 1
	if strings.TrimSpace(m[2]) != "" {
		oldCount, err = strconv.Atoi(m[2])
		if err != nil {
			return hunkHeader{}, malformedFailure(fmt.Sprintf("invalid hunk old count %q", m[2]))
		}
	}
	newStart, err := strconv.Atoi(m[3])
	if err != nil {
		return hunkHeader{}, malformedFailure(fmt.Sprintf("invalid hunk new start %q", m[3]))
	}
	newCount := 1
	if strings.TrimSpace(m[4]) != "" {
		newCount, err = strconv.Atoi(m[4])
		if err != nil {
			return hunkHeader{}, malformedFailure(fmt.Sprintf("invalid hunk new count %q", m[4]))
		}
	}

	return hunkHeader{
		hasPosition: true,
		oldStart:    oldStart,
		oldCount:    oldCount,
		newStart:    newStart,
		newCount:    newCount,
	}, nil
}

func findHunkAnchor(lines []string, changes []patchformat.ChangeLine, expected, floor int, anchored bool, context string, endOfFile bool) (int, error) {
	if floor < 0 {
		floor = 0
	}
	maxStart := len(lines)
	if floor > maxStart {
		floor = maxStart
	}

	matchAt := func(start int) bool {
		_, _, _, err := applyHunkAt(lines, changes, start)
		return err == nil
	}

	if anchored {
		if expected > maxStart {
			return -1, outOfBoundsFailure(expected+1, fmt.Sprintf("patch references line %d, but file has %d lines", expected+1, len(lines)))
		}
		if expected >= floor && expected <= maxStart && matchAt(expected) {
			return expected, nil
		}
		for fuzz := 1; fuzz <= hunkMaxFuzz; fuzz++ {
			up := expected - fuzz
			if up >= floor && up <= maxStart && matchAt(up) {
				return up, nil
			}
			down := expected + fuzz
			if down >= floor && down <= maxStart && matchAt(down) {
				return down, nil
			}
		}
		return -1, contentMismatchFailure(expected+1, true, fmt.Sprintf("patch hunk did not match within %d lines of the expected location", hunkMaxFuzz))
	}

	if strings.TrimSpace(context) != "" {
		for start := floor; start < len(lines); start++ {
			if lineMatches(lines[start], context) {
				contextStart := start + 1
				for candidate := contextStart; candidate <= maxStart; candidate++ {
					if endOfFile && candidate+oldLineCount(changes) != len(lines) {
						continue
					}
					if matchAt(candidate) {
						return candidate, nil
					}
				}
			}
		}
		return -1, contentMismatchFailure(0, false, fmt.Sprintf("failed to find context %q", context))
	}

	for start := floor; start <= maxStart; start++ {
		if endOfFile && start+oldLineCount(changes) != len(lines) {
			continue
		}
		if matchAt(start) {
			return start, nil
		}
	}
	return -1, contentMismatchFailure(0, false, "patch hunk did not match file content")
}

func oldLineCount(changes []patchformat.ChangeLine) int {
	count := 0
	for _, ch := range changes {
		switch ch.Kind {
		case ' ', '-':
			count++
		}
	}
	return count
}

func lineMatches(actual, expected string) bool {
	return actual == expected || strings.TrimRight(actual, " \t") == strings.TrimRight(expected, " \t") || strings.TrimSpace(actual) == strings.TrimSpace(expected)
}

func applyHunkAt(lines []string, changes []patchformat.ChangeLine, start int) ([]string, int, int, error) {
	if start < 0 || start > len(lines) {
		return nil, 0, 0, outOfBoundsFailure(start+1, fmt.Sprintf("invalid hunk start %d", start))
	}

	out := make([]string, 0, len(lines)+len(changes))
	out = append(out, lines[:start]...)

	cursor := start
	oldCount := 0
	newCount := 0
	for _, ch := range changes {
		switch ch.Kind {
		case ' ':
			if cursor >= len(lines) || lines[cursor] != ch.Content {
				return nil, 0, 0, contentMismatchFailure(cursor+1, false, fmt.Sprintf("expected context line %q", ch.Content))
			}
			out = append(out, lines[cursor])
			cursor++
			oldCount++
			newCount++
		case '-':
			if cursor >= len(lines) || lines[cursor] != ch.Content {
				return nil, 0, 0, contentMismatchFailure(cursor+1, false, fmt.Sprintf("expected deleted line %q", ch.Content))
			}
			cursor++
			oldCount++
		case '+':
			out = append(out, ch.Content)
			newCount++
		default:
			return nil, 0, 0, malformedFailure(fmt.Sprintf("unknown change line prefix %q", string(ch.Kind)))
		}
	}

	out = append(out, lines[cursor:]...)
	return out, oldCount, newCount, nil
}
