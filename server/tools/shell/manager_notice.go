package shell

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"core/server/tools/shell/postprocess"
	xansi "github.com/charmbracelet/x/ansi"
)

const noOutputText = "No output"

func NormalizeBackgroundOutputMode(raw string) BackgroundOutputMode {
	switch BackgroundOutputMode(strings.ToLower(strings.TrimSpace(raw))) {
	case BackgroundOutputVerbose:
		return BackgroundOutputVerbose
	case BackgroundOutputConcise:
		return BackgroundOutputConcise
	default:
		return BackgroundOutputDefault
	}
}

func SummarizeBackgroundEvent(evt Event, opts BackgroundNoticeOptions) BackgroundNoticeSummary {
	maxChars := opts.MaxChars
	if maxChars <= 0 {
		maxChars = defaultOutputTokenCap * 4
	}
	mode := effectiveBackgroundOutputMode(evt.Snapshot.ExitCode, opts.SuccessOutputMode)
	preview, lineCount, truncated := backgroundNoticePreview(evt, maxChars, mode)
	state := strings.TrimSpace(evt.Snapshot.State)
	if state == "" {
		state = strings.TrimSpace(string(evt.Type))
	}
	if state == "" {
		state = "completed"
	}
	detail := []string{fmt.Sprintf("Background shell %s %s.", evt.Snapshot.ID, state)}
	if evt.Snapshot.ExitCode != nil {
		detail = append(detail, fmt.Sprintf("Exit code: %d", *evt.Snapshot.ExitCode))
	}
	if strings.TrimSpace(evt.Snapshot.LogPath) != "" && lineCount > 0 {
		lineCountText := fmt.Sprintf("%d lines", lineCount)
		if lineCount == 1 {
			lineCountText = "1 line"
		}
		detail = append(detail, fmt.Sprintf("Output file (%s): %s", lineCountText, evt.Snapshot.LogPath))
	}
	if mode != BackgroundOutputConcise {
		if strings.TrimSpace(preview) == "" {
			detail = append(detail, noOutputText)
		} else {
			detail = append(detail, "Output:")
			detail = append(detail, preview)
		}
	}
	ongoing := fmt.Sprintf("Background shell %s %s", evt.Snapshot.ID, state)
	if evt.Snapshot.ExitCode != nil {
		ongoing = fmt.Sprintf("%s (exit %d)", ongoing, *evt.Snapshot.ExitCode)
	}
	return BackgroundNoticeSummary{
		DetailText:  strings.Join(detail, "\n"),
		CondensedText: ongoing,
		LineCount:   lineCount,
		Truncated:   truncated,
		LogPath:     evt.Snapshot.LogPath,
	}
}

func effectiveBackgroundOutputMode(exitCode *int, successMode BackgroundOutputMode) BackgroundOutputMode {
	mode := NormalizeBackgroundOutputMode(string(successMode))
	if exitCode == nil {
		return BackgroundOutputDefault
	}
	if *exitCode == 0 {
		return mode
	}
	if mode == BackgroundOutputVerbose {
		return BackgroundOutputVerbose
	}
	return BackgroundOutputDefault
}

func backgroundNoticePreview(evt Event, maxChars int, mode BackgroundOutputMode) (string, int, bool) {
	if evt.PreviewProcessed {
		if mode == BackgroundOutputConcise {
			return "", 0, false
		}
		preview := strings.TrimSpace(evt.Preview)
		if preview == "" {
			return "", 0, false
		}
		if mode == BackgroundOutputVerbose {
			return preview, countOutputLines(preview), false
		}
		display, truncated, _ := truncateWithTemplate(preview, maxChars, backgroundTruncationBannerTemplate)
		return display, countOutputLines(preview), truncated
	}
	if strings.TrimSpace(evt.Snapshot.LogPath) != "" {
		preview, lineCount, truncated, err := readBackgroundSummaryFromFile(evt.Snapshot.LogPath, maxChars, mode, !evt.Snapshot.RawOutput)
		if err == nil {
			return preview, lineCount, truncated
		}
	}
	if mode == BackgroundOutputConcise {
		return "", 0, false
	}
	preview := evt.Preview
	if !evt.Snapshot.RawOutput {
		preview = postprocess.SanitizeOutput(preview)
	}
	truncated := evt.Removed > 0
	if strings.TrimSpace(preview) == "" {
		return "", 0, truncated
	}
	return preview, countOutputLines(preview), truncated
}

func readBackgroundSummaryFromFile(path string, maxChars int, mode BackgroundOutputMode, sanitize bool) (string, int, bool, error) {
	fp, err := os.Open(path)
	if err != nil {
		return "", 0, false, err
	}
	defer fp.Close()
	builder := newBackgroundPreviewBuilder(maxChars, mode, sanitize)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := fp.Read(buf)
		if n > 0 {
			builder.WriteRaw(buf[:n])
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			builder.Finish()
			return builder.Preview(), builder.LineCount(), builder.Truncated(), nil
		}
		return "", 0, false, readErr
	}
}

type backgroundPreviewBuilder struct {
	maxChars    int
	mode        BackgroundOutputMode
	sanitize    bool
	carry       []byte
	prevCR      bool
	totalBytes  int
	lineCount   int
	hasContent  bool
	lastNewline bool
	fullMode    bool
	full        []byte
	head        []byte
	tail        []byte
}

func newBackgroundPreviewBuilder(maxChars int, mode BackgroundOutputMode, sanitize bool) *backgroundPreviewBuilder {
	if maxChars <= 0 {
		maxChars = defaultOutputTokenCap * 4
	}
	mode = NormalizeBackgroundOutputMode(string(mode))
	return &backgroundPreviewBuilder{
		maxChars: maxChars,
		mode:     mode,
		sanitize: sanitize,
		fullMode: mode == BackgroundOutputVerbose,
		full:     make([]byte, 0, min(maxChars, 4096)),
		head:     make([]byte, 0, headTailSize),
		tail:     make([]byte, 0, headTailSize),
	}
}

func (b *backgroundPreviewBuilder) WriteRaw(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	if !b.sanitize {
		b.emitBytes(chunk)
		return
	}
	data := append(append([]byte(nil), b.carry...), chunk...)
	processUpTo := len(data)
	if start, ok := trailingIncompleteANSIStart(data); ok {
		processUpTo = start
		b.carry = append(b.carry[:0], data[start:]...)
	} else {
		b.carry = b.carry[:0]
	}
	if processUpTo == 0 {
		return
	}
	b.writeSanitized(xansi.Strip(string(data[:processUpTo])))
}

func (b *backgroundPreviewBuilder) Finish() {
	if len(b.carry) == 0 {
		return
	}
	if !b.sanitize {
		b.emitBytes(b.carry)
		b.carry = b.carry[:0]
		return
	}
	b.writeSanitized(xansi.Strip(string(b.carry)))
	b.carry = b.carry[:0]
}

func (b *backgroundPreviewBuilder) writeSanitized(text string) {
	if text == "" {
		return
	}
	for _, r := range text {
		switch {
		case r == '\r':
			b.emitBytes([]byte{'\n'})
			b.prevCR = true
		case r == '\n':
			if b.prevCR {
				b.prevCR = false
				continue
			}
			b.emitBytes([]byte{'\n'})
		case r == '\t' || !unicode.IsControl(r):
			b.prevCR = false
			var buf [4]byte
			n := utf8EncodeRune(buf[:], r)
			b.emitBytes(buf[:n])
		default:
			b.prevCR = false
		}
	}
}

func (b *backgroundPreviewBuilder) emitBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	b.hasContent = true
	b.totalBytes += len(data)
	if b.fullMode {
		b.full = append(b.full, data...)
	} else if len(b.full) < b.maxChars {
		remaining := b.maxChars - len(b.full)
		if remaining > len(data) {
			remaining = len(data)
		}
		b.full = append(b.full, data[:remaining]...)
	}
	if len(b.head) < headTailSize {
		remaining := headTailSize - len(b.head)
		if remaining > len(data) {
			remaining = len(data)
		}
		b.head = append(b.head, data[:remaining]...)
	}
	b.tail = append(b.tail, data...)
	if len(b.tail) > headTailSize {
		b.tail = append([]byte(nil), b.tail[len(b.tail)-headTailSize:]...)
	}
	for _, v := range data {
		if v == '\n' {
			b.lineCount++
			b.lastNewline = true
			continue
		}
		b.lastNewline = false
	}
}

func (b *backgroundPreviewBuilder) Preview() string {
	if b.mode == BackgroundOutputConcise {
		return ""
	}
	if b.fullMode {
		return string(b.full)
	}
	if b.totalBytes <= b.maxChars {
		return string(b.full)
	}
	headLen, tailLen := truncationSegmentLengths(b.totalBytes, b.maxChars)
	removed := b.totalBytes - headLen - tailLen
	head := string(b.head[:headLen])
	tail := string(b.tail[len(b.tail)-tailLen:])
	return fmt.Sprintf("%s%s%s", head, fmt.Sprintf(backgroundTruncationBannerTemplate, removed), tail)
}

func (b *backgroundPreviewBuilder) LineCount() int {
	if !b.hasContent {
		return 0
	}
	if b.lastNewline {
		return b.lineCount
	}
	return b.lineCount + 1
}

func (b *backgroundPreviewBuilder) Truncated() bool {
	if b.mode == BackgroundOutputConcise || b.fullMode {
		return false
	}
	return b.totalBytes > b.maxChars
}

func trailingIncompleteANSIStart(data []byte) (int, bool) {
	lastESC := bytes.LastIndexByte(data, 0x1b)
	if lastESC < 0 {
		return 0, false
	}
	for i := lastESC + 1; i < len(data); i++ {
		if data[i] == 0x07 || data[i] >= 0x40 && data[i] <= 0x7e {
			return 0, false
		}
	}
	return lastESC, true
}

func utf8EncodeRune(dst []byte, r rune) int {
	if r < 0x80 {
		dst[0] = byte(r)
		return 1
	}
	return copy(dst, []byte(string(r)))
}

func formatExecResponse(result ExecResult) string {
	sections := make([]string, 0, 6)
	output := strings.TrimSpace(result.Output)
	if strings.TrimSpace(result.Warning) != "" {
		sections = append(sections, result.Warning)
	}
	if result.MovedToBackground {
		sections = append(sections, formatBackgroundTransitionLine(result.SessionID, output != ""))
	}
	if result.ExitCode != nil && *result.ExitCode != 0 {
		sections = append(sections, fmt.Sprintf("Exit code %d, output:", *result.ExitCode))
	} else if result.Running && strings.TrimSpace(result.SessionID) != "" && !result.MovedToBackground {
		sections = append(sections, fmt.Sprintf("Process running with session ID %s", result.SessionID))
	}
	if result.Backgrounded && result.ExitCode != nil {
		sections = append(sections, fmt.Sprintf("Wall time: %.4f seconds", result.WallTime.Seconds()))
	}
	if result.Backgrounded && result.ExitCode != nil && strings.TrimSpace(result.OutputPath) != "" {
		sections = append(sections, fmt.Sprintf("Log file: %s", result.OutputPath))
	}
	if output == "" {
		sections = append(sections, noOutputText)
	} else {
		sections = append(sections, output)
	}
	return strings.Join(sections, "\n")
}

func formatBackgroundTransitionLine(sessionID string, hasOutput bool) string {
	sessionID = strings.TrimSpace(sessionID)
	switch {
	case sessionID != "" && hasOutput:
		return fmt.Sprintf("Process moved to background with ID %s. Output:", sessionID)
	case sessionID != "":
		return fmt.Sprintf("Process moved to background with ID %s.", sessionID)
	case hasOutput:
		return "Process moved to background. Output:"
	default:
		return "Process moved to background."
	}
}
