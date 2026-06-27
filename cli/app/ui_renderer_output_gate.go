package app

import (
	"io"
	"sync"

	xansi "github.com/charmbracelet/x/ansi"
)

type uiRendererOutputGateState struct {
	mu                      sync.Mutex
	suppressRendererWrites  bool
	physicalAltScreenActive bool
}

func newUIRendererOutputGateState() *uiRendererOutputGateState {
	return &uiRendererOutputGateState{}
}

func (s *uiRendererOutputGateState) SetSuppressRendererWrites(suppress bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suppressRendererWrites = suppress
}

func (s *uiRendererOutputGateState) PhysicalAltScreenActive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.physicalAltScreenActive
}

func (s *uiRendererOutputGateState) shouldDrop(payload []byte) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.suppressRendererWrites &&
		!s.physicalAltScreenActive &&
		!rendererOutputGateAllowsSuppressedControlWrite(payload)
}

func (s *uiRendererOutputGateState) observeWrittenPayload(payload []byte) {
	if s == nil || len(payload) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if string(payload) == xansi.SetModeAltScreenSaveCursor {
		s.physicalAltScreenActive = true
		return
	}
	if string(payload) == xansi.ResetModeAltScreenSaveCursor {
		s.physicalAltScreenActive = false
	}
}

type uiRendererOutputGateWriter struct {
	out   io.Writer
	state *uiRendererOutputGateState
}

type uiRendererOutputGateFileWriter struct {
	uiRendererOutputGateWriter
	file terminalCursorFile
}

func newUIRendererOutputGateWriter(out io.Writer, state *uiRendererOutputGateState) io.Writer {
	if out == nil || state == nil {
		return out
	}
	writer := uiRendererOutputGateWriter{out: out, state: state}
	if file, ok := out.(terminalCursorFile); ok {
		return uiRendererOutputGateFileWriter{uiRendererOutputGateWriter: writer, file: file}
	}
	return writer
}

func (w uiRendererOutputGateWriter) Write(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	if w.state != nil && w.state.shouldDrop(payload) {
		return len(payload), nil
	}
	n, err := w.out.Write(payload)
	if err == nil && n == len(payload) {
		w.state.observeWrittenPayload(payload)
	}
	return n, err
}

func (w uiRendererOutputGateFileWriter) Read(payload []byte) (int, error) {
	return w.file.Read(payload)
}

func (w uiRendererOutputGateFileWriter) Close() error {
	return nil
}

func (w uiRendererOutputGateFileWriter) Fd() uintptr {
	return w.file.Fd()
}

func rendererOutputGateAllowsSuppressedControlWrite(payload []byte) bool {
	if len(payload) == 0 {
		return true
	}
	parser := xansi.GetParser()
	defer xansi.PutParser(parser)

	input := string(payload)
	state := byte(0)
	for len(input) > 0 {
		_, width, n, newState := xansi.GraphemeWidth.DecodeSequenceInString(input, state, parser)
		if n <= 0 || width > 0 {
			return false
		}
		sequence := input[:n]
		state = newState
		input = input[n:]
		if rendererOutputGateControlSequenceIsRendererPositioningOrClear(sequence, parser) {
			return false
		}
	}
	return true
}

func rendererOutputGateControlSequenceIsRendererPositioningOrClear(sequence string, parser *xansi.Parser) bool {
	if sequence == "\r" || sequence == "\n" {
		return true
	}
	switch xansi.Cmd(parser.Command()).Final() {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'J', 'K', 'f':
		return true
	default:
		return false
	}
}
