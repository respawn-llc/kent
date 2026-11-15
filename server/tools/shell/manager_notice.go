package shell

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"core/server/tools/shell/postprocess"
	xansi "github.com/charmbracelet/x/ansi"
)

const noOutputText = "No output"

type execPresentationKind uint8

const (
	execPresentationRunning execPresentationKind = iota
	execPresentationForegroundCompleted
	execPresentationBackgroundCompleted
)

type visibleShellOutput struct {
	command string
	warning postprocess.Warning
}

func newVisibleShellOutput(command string, warning postprocess.Warning) visibleShellOutput {
	return visibleShellOutput{command: command, warning: warning}
}

func (o visibleShellOutput) Content() string {
	sections := make([]string, 0, 2)
	if o.warning != nil {
		sections = append(sections, o.warning.Text())
	}
	if text := strings.TrimSpace(o.command); text != "" {
		sections = append(sections, text)
	}
	return strings.Join(sections, "\n")
}

func (o visibleShellOutput) HasVisibleContent() bool {
	return strings.TrimSpace(o.Content()) != ""
}

func (o visibleShellOutput) HasCommandContent() bool {
	return strings.TrimSpace(o.command) != ""
}

func (o visibleShellOutput) Warning() postprocess.Warning {
	return o.warning
}

type execPresentation struct {
	kind              execPresentationKind
	sessionID         string
	wallTime          time.Duration
	outputPath        string
	output            visibleShellOutput
	exitCode          int
	truncated         bool
	backgrounded      bool
	movedToBackground bool
}

func projectExecResult(result ExecResult) execPresentation {
	presentation := execPresentation{
		sessionID:         strings.TrimSpace(result.SessionID),
		wallTime:          result.WallTime,
		outputPath:        strings.TrimSpace(result.OutputPath),
		output:            newVisibleShellOutput(result.Output, result.Warning),
		backgrounded:      result.Backgrounded,
		movedToBackground: result.MovedToBackground,
		truncated:         result.Truncated,
	}
	if result.ExitCode == nil {
		presentation.kind = execPresentationRunning
		return presentation
	}
	presentation.exitCode = *result.ExitCode
	if result.Backgrounded {
		presentation.kind = execPresentationBackgroundCompleted
		return presentation
	}
	presentation.kind = execPresentationForegroundCompleted
	return presentation
}

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

type InvalidBackgroundEventError struct {
	EventType EventType
	ProcessID string
	State     string
}

func (e *InvalidBackgroundEventError) Error() string {
	return fmt.Sprintf("terminal background event %q for process %q in state %q is missing completion output", e.EventType, e.ProcessID, e.State)
}

type backgroundInlinePreview struct {
	text string
}

type backgroundNoticeOutput struct {
	visible        visibleShellOutput
	inlinePreview  *backgroundInlinePreview
	truncated      bool
	previewRemoved int
}

func (s BackgroundNoticeSummary) RuntimePreview() (string, int) {
	if s.output.inlinePreview == nil {
		return "", s.output.previewRemoved
	}
	return s.output.inlinePreview.text, s.output.previewRemoved
}

func SummarizeBackgroundEvent(evt Event, opts BackgroundNoticeOptions) (BackgroundNoticeSummary, error) {
	if (evt.Type == EventCompleted || evt.Type == EventKilled) && evt.completion == nil {
		return BackgroundNoticeSummary{}, &InvalidBackgroundEventError{
			EventType: evt.Type,
			ProcessID: evt.Snapshot.ID,
			State:     evt.Snapshot.State,
		}
	}
	maxChars := opts.MaxChars
	if maxChars <= 0 {
		maxChars = defaultOutputTokenCap * 4
	}
	mode := effectiveBackgroundOutputMode(evt.Snapshot.ExitCode, opts.SuccessOutputMode)
	output, err := projectBackgroundNoticeOutput(evt, maxChars, mode)
	if err != nil {
		return BackgroundNoticeSummary{}, err
	}
	state := strings.TrimSpace(evt.Snapshot.State)
	if state == "" {
		state = strings.TrimSpace(string(evt.Type))
	}
	if state == "" {
		state = "completed"
	}
	var detail string
	if evt.Snapshot.ExitCode != nil {
		commandOutput := output.visible.command
		if output.inlinePreview != nil {
			commandOutput = output.inlinePreview.text
		}
		presentation := execPresentation{
			kind:       execPresentationBackgroundCompleted,
			sessionID:  strings.TrimSpace(evt.Snapshot.ID),
			outputPath: strings.TrimSpace(evt.Snapshot.LogPath),
			output:     newVisibleShellOutput(commandOutput, output.visible.Warning()),
			exitCode:   *evt.Snapshot.ExitCode,
			truncated:  output.truncated,
		}
		detail = renderCompletedShellResult(presentation, completedShellRenderOptions{
			hideCommandOutput: mode == BackgroundOutputConcise,
		})
	} else {
		sections := []string{fmt.Sprintf("Background shell %s %s.", evt.Snapshot.ID, state)}
		if warning := output.visible.Warning(); warning != nil {
			sections = append(sections, warning.Text())
		}
		if output.inlinePreview != nil {
			sections = append(sections, output.inlinePreview.text)
		}
		detail = strings.Join(sections, "\n")
	}
	summary := fmt.Sprintf("Background shell %s %s", evt.Snapshot.ID, state)
	if evt.Snapshot.ExitCode != nil {
		summary = fmt.Sprintf("%s (exit %d)", summary, *evt.Snapshot.ExitCode)
	}
	return BackgroundNoticeSummary{
		DetailText:    detail,
		CondensedText: summary,
		Truncated:     output.truncated,
		LogPath:       evt.Snapshot.LogPath,
		output:        output,
	}, nil
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

func projectBackgroundNoticeOutput(evt Event, maxChars int, mode BackgroundOutputMode) (backgroundNoticeOutput, error) {
	if evt.completion == nil {
		return backgroundNoticeOutput{}, nil
	}
	completion := evt.completion
	output := backgroundNoticeOutput{
		visible:        completion.output,
		truncated:      completion.removed > 0,
		previewRemoved: completion.removed,
	}
	path := strings.TrimSpace(evt.Snapshot.LogPath)
	if completion.source == completionOutputFallback && path != "" && mode != BackgroundOutputConcise {
		preview, truncated, err := readBackgroundSummaryFromFile(path, maxChars, mode, !evt.Snapshot.RawOutput)
		if err != nil {
			warning, warningErr := postprocess.NewWarning(fmt.Sprintf("failed to read output preview: %v", err))
			if warningErr != nil {
				return backgroundNoticeOutput{}, warningErr
			}
			output.visible.warning = postprocess.MergeWarnings(output.visible.warning, warning)
		} else {
			output.visible.command = limitModelVisibleFallbackOutput(preview, evt.Snapshot.RawOutput)
			output.truncated = output.truncated || truncated
			if truncated && output.previewRemoved == 0 {
				output.previewRemoved = 1
			}
		}
	}
	if mode == BackgroundOutputConcise {
		return output, nil
	}
	command := output.visible.command
	if strings.TrimSpace(command) == "" {
		return output, nil
	}
	if mode == BackgroundOutputVerbose {
		output.inlinePreview = &backgroundInlinePreview{text: strings.TrimSpace(command)}
		return output, nil
	}
	preview, truncated, _ := truncateWithTemplate(strings.TrimSpace(command), maxChars, backgroundTruncationBannerTemplate)
	output.inlinePreview = &backgroundInlinePreview{text: preview}
	output.truncated = output.truncated || truncated
	if truncated && output.previewRemoved == 0 {
		output.previewRemoved = 1
	}
	return output, nil
}

func InvariantFailureBackgroundNotice(evt Event, cause error) BackgroundNoticeSummary {
	state := strings.TrimSpace(evt.Snapshot.State)
	if state == "" {
		state = strings.TrimSpace(string(evt.Type))
	}
	message := fmt.Sprintf("Background shell completion internal error: %v", cause)
	detail := []string{fmt.Sprintf("Background shell %s %s.", evt.Snapshot.ID, state)}
	if evt.Snapshot.ExitCode != nil {
		detail = append(detail, fmt.Sprintf("Exit code: %d", *evt.Snapshot.ExitCode))
	}
	detail = append(detail, message)
	summary := fmt.Sprintf("Background shell %s %s", evt.Snapshot.ID, state)
	if evt.Snapshot.ExitCode != nil {
		summary = fmt.Sprintf("%s (exit %d)", summary, *evt.Snapshot.ExitCode)
	}
	return BackgroundNoticeSummary{
		DetailText:    strings.Join(detail, "\n"),
		CondensedText: summary,
		LogPath:       evt.Snapshot.LogPath,
		output:        backgroundNoticeOutput{},
	}
}

func readBackgroundSummaryFromFile(path string, maxChars int, mode BackgroundOutputMode, sanitize bool) (string, bool, error) {
	fp, err := os.Open(path)
	if err != nil {
		return "", false, err
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
			return builder.Preview(), builder.Truncated(), nil
		}
		return "", false, readErr
	}
}

type backgroundPreviewBuilder struct {
	maxChars          int
	mode              BackgroundOutputMode
	sanitize          bool
	carry             []byte
	prevCR            bool
	totalBytes        int
	hasVisibleContent bool
	fullMode          bool
	full              []byte
	head              []byte
	tail              []byte
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
	if !b.hasVisibleContent && strings.TrimSpace(string(data)) != "" {
		b.hasVisibleContent = true
	}
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
}

func (b *backgroundPreviewBuilder) Preview() string {
	if b.mode == BackgroundOutputConcise {
		return ""
	}
	if !b.hasVisibleContent {
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
	return renderExecPresentation(projectExecResult(result))
}

type completedShellRenderOptions struct {
	includePollMetadata bool
	hideCommandOutput   bool
}

func renderCompletedShellResult(presentation execPresentation, opts completedShellRenderOptions) string {
	output := presentation.output.Content()
	if opts.hideCommandOutput {
		output = ""
		if warning := presentation.output.Warning(); warning != nil {
			output = warning.Text()
		}
	}
	hasOutput := presentation.output.HasVisibleContent()
	if opts.hideCommandOutput && presentation.output.HasCommandContent() {
		hasOutput = true
	}
	showOutputPath := presentation.truncated && hasOutput
	if opts.hideCommandOutput && presentation.output.HasCommandContent() {
		showOutputPath = true
	}

	sections := make([]string, 0, 3)
	if opts.includePollMetadata {
		sections = appendPollMetadata(sections, presentation)
	}
	if !hasOutput {
		sections = append(sections, fmt.Sprintf("Shell %s exited with code %d and no output", presentation.sessionID, presentation.exitCode))
		return strings.Join(sections, "\n")
	}

	switch presentation.kind {
	case execPresentationForegroundCompleted:
		if presentation.exitCode == 0 {
			if showOutputPath {
				sections = append(sections, fmt.Sprintf("Shell %s output written to %s:\n%s", presentation.sessionID, presentation.outputPath, output))
			} else {
				sections = append(sections, output)
			}
		} else {
			sections = append(sections, formatCompletedOutputHeader(presentation, showOutputPath, opts.includePollMetadata))
			if output != "" {
				sections = append(sections, output)
			}
		}
	case execPresentationBackgroundCompleted:
		sections = append(sections, formatCompletedOutputHeader(presentation, showOutputPath, opts.includePollMetadata))
		if output != "" {
			sections = append(sections, output)
		}
	default:
		panic(fmt.Sprintf("unknown completed exec presentation kind %d", presentation.kind))
	}
	return strings.Join(sections, "\n")
}

func formatCompletedOutputHeader(presentation execPresentation, showOutputPath bool, pathAlreadyShown bool) string {
	if showOutputPath && !pathAlreadyShown && presentation.outputPath != "" {
		return fmt.Sprintf("Shell %s exited with code %d, output written to %s:", presentation.sessionID, presentation.exitCode, presentation.outputPath)
	}
	return fmt.Sprintf("Shell %s exited with code %d, output:", presentation.sessionID, presentation.exitCode)
}

func appendPollMetadata(sections []string, presentation execPresentation) []string {
	sections = append(sections, fmt.Sprintf("Wall time: %.4f seconds", presentation.wallTime.Seconds()))
	if presentation.outputPath != "" {
		sections = append(sections, fmt.Sprintf("Output file: %s", presentation.outputPath))
	}
	return sections
}

func renderExecPresentation(presentation execPresentation) string {
	output := presentation.output.Content()
	switch presentation.kind {
	case execPresentationRunning:
		sections := make([]string, 0, 2)
		if presentation.movedToBackground {
			sections = append(sections, formatBackgroundTransitionLine(presentation.sessionID, presentation.wallTime))
			if presentation.outputPath != "" {
				sections = append(sections, fmt.Sprintf("Output file: %s", presentation.outputPath))
			}
			if warning := presentation.output.Warning(); warning != nil {
				sections = append(sections, warning.Text())
			}
			return strings.Join(sections, "\n")
		}
		if presentation.backgrounded {
			sections = appendPollMetadata(sections, presentation)
		}
		if presentation.sessionID != "" {
			sections = append(sections, fmt.Sprintf("Process running with session ID %s", presentation.sessionID))
		}
		if output == "" {
			sections = append(sections, noOutputText)
		} else {
			sections = append(sections, output)
		}
		return strings.Join(sections, "\n")
	case execPresentationForegroundCompleted:
		return renderCompletedShellResult(presentation, completedShellRenderOptions{})
	case execPresentationBackgroundCompleted:
		return renderCompletedShellResult(presentation, completedShellRenderOptions{includePollMetadata: true})
	default:
		panic(fmt.Sprintf("unknown exec presentation kind %d", presentation.kind))
	}
}

func formatToolError(warning postprocess.Warning, toolError string) string {
	sections := make([]string, 0, 2)
	if warning != nil {
		sections = append(sections, warning.Text())
	}
	if text := strings.TrimSpace(toolError); text != "" {
		sections = append(sections, text)
	}
	return strings.Join(sections, "\n")
}

func formatBackgroundTransitionLine(sessionID string, wallTime time.Duration) string {
	sessionID = strings.TrimSpace(sessionID)
	return fmt.Sprintf("Shell moved to background with ID %s after %.1fs", sessionID, wallTime.Seconds())
}
