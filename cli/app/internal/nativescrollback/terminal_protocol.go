package nativescrollback

import (
	"strconv"
	"strings"
)

const (
	terminalWriteStartPrefix = "\x1b]777;KentNativeScrollback="
	terminalWriteMarkerEnd   = "\x07"
	terminalWriteMaxHeader   = 128
	TerminalWriteMaxPayload  = 128 * 1024
)

type TerminalWriteFrame struct {
	Sequence Sequence
	Length   int
	Token    string
}

func EncodeTerminalWrite(write TerminalWrite, token string) string {
	if write.Sequence == 0 {
		return write.Text
	}
	if len(write.Text) > TerminalWriteMaxPayload {
		return write.Text
	}
	header := strconv.FormatUint(uint64(write.Sequence), 10) + ":" + strconv.Itoa(len(write.Text)) + ":" + token
	return terminalWriteStartPrefix + header + terminalWriteMarkerEnd + write.Text
}

type TerminalWriteStripper struct {
	pending string
}

// Strip accepts any well-formed native frame and is intended for tests and
// diagnostics. Production terminal writers must use StripExpected so arbitrary
// renderer output cannot spoof a native write acknowledgement.
func (s *TerminalWriteStripper) Strip(payload string) (string, []Sequence) {
	if s == nil {
		return StripTerminalWriteMarkers(payload)
	}
	if s.pending != "" {
		payload = s.pending + payload
		s.pending = ""
	}
	clean, sequences, pending, _ := stripTerminalWriteMarkers(payload, nil, false)
	s.pending = pending
	return clean, sequences
}

func (s *TerminalWriteStripper) StripExpected(payload string, expected []TerminalWriteFrame) (string, []Sequence, []TerminalWriteFrame) {
	if s == nil {
		clean, sequences := StripTerminalWriteMarkers(payload)
		return clean, sequences, expected
	}
	if s.pending != "" {
		payload = s.pending + payload
		s.pending = ""
	}
	clean, sequences, pending, remaining := stripTerminalWriteMarkers(payload, expected, true)
	s.pending = pending
	return clean, sequences, remaining
}

// StripTerminalWriteMarkers accepts any well-formed native frame and is intended
// for tests and diagnostics. Production terminal writers must use StripExpected
// with state-registered frame capabilities.
func StripTerminalWriteMarkers(payload string) (string, []Sequence) {
	clean, sequences, _, _ := stripTerminalWriteMarkers(payload, nil, false)
	return clean, sequences
}

func stripTerminalWriteMarkers(payload string, expected []TerminalWriteFrame, requireExpected bool) (string, []Sequence, string, []TerminalWriteFrame) {
	if payload == "" {
		return "", nil, "", expected
	}
	var out strings.Builder
	sequences := make([]Sequence, 0, 1)
	remaining := payload
	for {
		start := strings.Index(remaining, terminalWriteStartPrefix)
		if start < 0 {
			hold := terminalWriteMarkerPrefixSuffixLen(remaining)
			if hold > 0 {
				out.WriteString(remaining[:len(remaining)-hold])
				return out.String(), sequences, remaining[len(remaining)-hold:], expected
			}
			out.WriteString(remaining)
			break
		}
		out.WriteString(remaining[:start])
		afterStartPrefix := remaining[start+len(terminalWriteStartPrefix):]
		headerEnd := strings.Index(afterStartPrefix, terminalWriteMarkerEnd)
		if headerEnd < 0 {
			if len(afterStartPrefix) > terminalWriteMaxHeader {
				out.WriteString(remaining[start : start+1])
				remaining = remaining[start+1:]
				continue
			}
			return out.String(), sequences, remaining[start:], expected
		}
		frame, ok := parseTerminalWriteHeader(afterStartPrefix[:headerEnd])
		if !ok || frame.Length > TerminalWriteMaxPayload {
			// Preserve marker-like user output unless it is a well-formed native
			// scrollback frame produced by EncodeTerminalWrite.
			malformedHeaderEnd := start + len(terminalWriteStartPrefix) + headerEnd + len(terminalWriteMarkerEnd)
			out.WriteString(remaining[start:malformedHeaderEnd])
			remaining = remaining[malformedHeaderEnd:]
			continue
		}
		expectedIndex := -1
		if requireExpected {
			expectedIndex = terminalWriteExpectedFrameIndex(expected, frame)
			if expectedIndex < 0 {
				headerEnd := start + len(terminalWriteStartPrefix) + headerEnd + len(terminalWriteMarkerEnd)
				out.WriteString(remaining[start:headerEnd])
				remaining = remaining[headerEnd:]
				continue
			}
		}
		bodyStart := start + len(terminalWriteStartPrefix) + headerEnd + len(terminalWriteMarkerEnd)
		bodyEnd := bodyStart + frame.Length
		if bodyEnd > len(remaining) {
			return out.String(), sequences, remaining[start:], expected
		}
		out.WriteString(remaining[bodyStart:bodyEnd])
		sequences = append(sequences, frame.Sequence)
		if requireExpected {
			expected = append(append([]TerminalWriteFrame(nil), expected[:expectedIndex]...), expected[expectedIndex+1:]...)
		}
		remaining = remaining[bodyEnd:]
	}
	return out.String(), sequences, "", expected
}

func terminalWriteMarkerPrefixSuffixLen(value string) int {
	maxLen := min(len(value), len(terminalWriteStartPrefix)-1)
	for length := maxLen; length > 0; length-- {
		if strings.HasPrefix(terminalWriteStartPrefix, value[len(value)-length:]) {
			return length
		}
	}
	return 0
}

func parseTerminalWriteHeader(value string) (TerminalWriteFrame, bool) {
	sequenceText, rest, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok {
		return TerminalWriteFrame{}, false
	}
	lengthText, token, ok := strings.Cut(rest, ":")
	if !ok || strings.TrimSpace(token) == "" {
		return TerminalWriteFrame{}, false
	}
	parsedSequence, err := strconv.ParseUint(strings.TrimSpace(sequenceText), 10, 64)
	if err != nil || parsedSequence == 0 {
		return TerminalWriteFrame{}, false
	}
	parsedLength, err := strconv.Atoi(strings.TrimSpace(lengthText))
	if err != nil || parsedLength < 0 {
		return TerminalWriteFrame{}, false
	}
	return TerminalWriteFrame{Sequence: Sequence(parsedSequence), Length: parsedLength, Token: token}, true
}

func terminalWriteExpectedFrameIndex(expected []TerminalWriteFrame, frame TerminalWriteFrame) int {
	for idx, candidate := range expected {
		if candidate.Sequence == frame.Sequence && candidate.Length == frame.Length && candidate.Token == frame.Token {
			return idx
		}
	}
	return -1
}
