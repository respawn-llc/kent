package app

import (
	"io"
	"sync"

	xansi "github.com/charmbracelet/x/ansi"
)

type uiInputFieldCursor struct {
	Visible  bool
	Row      int
	Col      int
	Absolute bool
}

type uiTerminalCursorPlacement struct {
	Visible   bool
	CursorRow int
	CursorCol int
	AnchorRow int
	AltScreen bool
}

type uiTerminalCursorState struct {
	mu       sync.Mutex
	writeMu  sync.Mutex
	latest   uiTerminalCursorPlacement
	previous uiTerminalCursorPlacement
	placed   bool
}

func newUITerminalCursorState() *uiTerminalCursorState {
	return &uiTerminalCursorState{}
}

func (s *uiTerminalCursorState) Set(placement uiTerminalCursorPlacement) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = sanitizeTerminalCursorPlacement(placement)
}

func (s *uiTerminalCursorState) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = uiTerminalCursorPlacement{}
}

func (s *uiTerminalCursorState) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = uiTerminalCursorPlacement{}
	s.previous = uiTerminalCursorPlacement{}
	s.placed = false
}

func (s *uiTerminalCursorState) Snapshot() (uiTerminalCursorPlacement, bool) {
	if s == nil {
		return uiTerminalCursorPlacement{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.latest.Visible {
		return s.latest, false
	}
	return s.latest, true
}

func (s *uiTerminalCursorState) hasPlacement() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.placed
}

func (s *uiTerminalCursorState) restoreRendererAnchor() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.placed {
		return ""
	}
	return terminalCursorRestoreSequence(s.previous)
}

func (s *uiTerminalCursorState) placeCursorPlan() (uiTerminalCursorPlacement, string) {
	if s == nil {
		return uiTerminalCursorPlacement{}, ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	placement := sanitizeTerminalCursorPlacement(s.latest)
	if !placement.Visible {
		return placement, ""
	}
	return placement, terminalCursorPlaceSequence(placement)
}

func (s *uiTerminalCursorState) commitPlacedCursor(placement uiTerminalCursorPlacement) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	placement = sanitizeTerminalCursorPlacement(placement)
	s.previous = placement
	s.placed = placement.Visible
}

func sanitizeTerminalCursorPlacement(placement uiTerminalCursorPlacement) uiTerminalCursorPlacement {
	if placement.CursorRow < 0 {
		placement.CursorRow = 0
	}
	if placement.CursorCol < 0 {
		placement.CursorCol = 0
	}
	if placement.AnchorRow < 0 {
		placement.AnchorRow = 0
	}
	if !placement.AltScreen && placement.AnchorRow < placement.CursorRow {
		placement.AnchorRow = placement.CursorRow
	}
	return placement
}

func terminalCursorRestoreSequence(placement uiTerminalCursorPlacement) string {
	placement = sanitizeTerminalCursorPlacement(placement)
	if placement.AltScreen {
		return xansi.CursorPosition(1, placement.AnchorRow+1)
	}
	rowsDown := placement.AnchorRow - placement.CursorRow
	if rowsDown < 0 {
		rowsDown = 0
	}
	if rowsDown == 0 {
		return "\r"
	}
	return xansi.CursorDown(rowsDown) + "\r"
}

func terminalCursorPlaceSequence(placement uiTerminalCursorPlacement) string {
	placement = sanitizeTerminalCursorPlacement(placement)
	if !placement.Visible {
		return ansiHideCursor
	}
	if placement.AltScreen {
		return xansi.ShowCursor + xansi.CursorPosition(placement.CursorCol+1, placement.CursorRow+1)
	}
	rowsUp := placement.AnchorRow - placement.CursorRow
	if rowsUp < 0 {
		rowsUp = 0
	}
	sequence := xansi.ShowCursor
	if rowsUp > 0 {
		sequence += xansi.CursorUp(rowsUp)
	}
	if placement.CursorCol > 0 {
		sequence += xansi.CursorForward(placement.CursorCol)
	}
	return sequence
}

type uiTerminalCursorWriter struct {
	out   io.Writer
	state *uiTerminalCursorState
}

type uiTerminalCursorFileWriter struct {
	uiTerminalCursorWriter
	file terminalCursorFile
}

type terminalCursorControlWritePlan struct {
	passthrough          bool
	invalidatesPlacement bool
	restoreAnchorBefore  bool
}

type terminalCursorFile interface {
	io.ReadWriteCloser
	Fd() uintptr
}

func newUITerminalCursorWriter(out io.Writer, state *uiTerminalCursorState) io.Writer {
	if out == nil || state == nil {
		return out
	}
	writer := uiTerminalCursorWriter{out: out, state: state}
	if file, ok := out.(terminalCursorFile); ok {
		return uiTerminalCursorFileWriter{uiTerminalCursorWriter: writer, file: file}
	}
	return writer
}

func (w uiTerminalCursorWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.state != nil {
		w.state.writeMu.Lock()
		defer w.state.writeMu.Unlock()
	}
	_, err := w.writePayload(p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w uiTerminalCursorWriter) writePayload(p []byte) (int, error) {
	if control := terminalCursorWriterControlWrite(p); control.passthrough {
		if control.restoreAnchorBefore {
			// Alt-screen enter saves the terminal cursor position. Our real cursor
			// usually sits in the input field, so restore Bubble's frame anchor first
			// or exiting detail mode appends the ongoing chrome from the input row.
			if prefix := w.state.restoreRendererAnchor(); prefix != "" {
				if err := writeTerminalCursorString(w.out, prefix); err != nil {
					return 0, err
				}
			}
		}
		if control.invalidatesPlacement {
			n, err := writeTerminalCursorBytes(w.out, p)
			if err != nil {
				return n, err
			}
			w.state.discardPlacedCursor()
			return n, nil
		}
		return writeTerminalCursorBytes(w.out, p)
	}
	shouldPreserveCursor := w.state.hasPlacement()
	if shouldPreserveCursor {
		if prefix := w.state.restoreRendererAnchor(); prefix != "" {
			if err := writeTerminalCursorString(w.out, prefix); err != nil {
				return 0, err
			}
		}
	}
	n, err := writeTerminalCursorBytes(w.out, p)
	if err != nil {
		return n, err
	}
	if shouldPreserveCursor || len(p) > 0 {
		placement, suffix := w.state.placeCursorPlan()
		if suffix != "" {
			if err := writeTerminalCursorString(w.out, suffix); err != nil {
				return n, err
			}
		}
		w.state.commitPlacedCursor(placement)
	}
	return n, nil
}

func writeTerminalCursorBytes(out io.Writer, p []byte) (int, error) {
	n, err := out.Write(p)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func writeTerminalCursorString(out io.Writer, value string) error {
	n, err := io.WriteString(out, value)
	if err != nil {
		return err
	}
	if n != len(value) {
		return io.ErrShortWrite
	}
	return nil
}

func (w uiTerminalCursorFileWriter) Read(p []byte) (int, error) {
	return w.file.Read(p)
}

func (w uiTerminalCursorFileWriter) Close() error {
	// Bubble Tea only needs Fd() for output TTY detection. Closing stdout/stderr
	// through this adapter would be surprising, so keep ownership with caller.
	return nil
}

func (w uiTerminalCursorFileWriter) Fd() uintptr {
	return w.file.Fd()
}

func (s *uiTerminalCursorState) discardPlacedCursor() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.previous = uiTerminalCursorPlacement{}
	s.placed = false
}

func terminalCursorWriterControlWrite(p []byte) terminalCursorControlWritePlan {
	if len(p) == 0 {
		return terminalCursorControlWritePlan{}
	}
	parser := xansi.GetParser()
	defer xansi.PutParser(parser)

	input := string(p)
	state := byte(0)
	invalidatesPlacement := false
	restoreAnchorBefore := false
	for len(input) > 0 {
		_, width, n, newState := xansi.GraphemeWidth.DecodeSequenceInString(input, state, parser)
		if n <= 0 {
			return terminalCursorControlWritePlan{}
		}
		sequence := input[:n]
		state = newState
		input = input[n:]
		if width > 0 {
			return terminalCursorControlWritePlan{}
		}
		if terminalCursorControlSequenceInvalidatesPlacement(sequence, parser) {
			invalidatesPlacement = true
		}
		if terminalCursorControlSequenceNeedsAnchorBeforeWrite(sequence, parser) {
			restoreAnchorBefore = true
		}
	}
	return terminalCursorControlWritePlan{
		passthrough:          true,
		invalidatesPlacement: invalidatesPlacement,
		restoreAnchorBefore:  restoreAnchorBefore,
	}
}

func terminalCursorControlSequenceInvalidatesPlacement(sequence string, parser *xansi.Parser) bool {
	if sequence == "\r" || sequence == "\n" {
		return true
	}
	command := xansi.Cmd(parser.Command())
	switch command.Final() {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'J', 'f':
		return true
	case 'h', 'l':
		return sequence == xansi.SetModeAltScreenSaveCursor || sequence == xansi.ResetModeAltScreenSaveCursor
	default:
		return false
	}
}

func terminalCursorControlSequenceNeedsAnchorBeforeWrite(sequence string, parser *xansi.Parser) bool {
	command := xansi.Cmd(parser.Command())
	if command.Final() != 'h' {
		return false
	}
	return sequence == xansi.SetModeAltScreenSaveCursor
}
