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
	altScreenParser         rendererOutputGateAltScreenParser
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
	_, transition := s.altScreenParser.preview(payload)
	if transition.entered {
		return false
	}
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
	transition := s.altScreenParser.apply(payload)
	switch transition.state {
	case rendererOutputGateAltScreenActive:
		s.physicalAltScreenActive = true
	case rendererOutputGateAltScreenInactive:
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
	return w.file.Close()
}

func (w uiRendererOutputGateFileWriter) Fd() uintptr {
	return w.file.Fd()
}

type rendererOutputGateAltScreenMode uint8

const (
	rendererOutputGateAltScreenUnchanged rendererOutputGateAltScreenMode = iota
	rendererOutputGateAltScreenActive
	rendererOutputGateAltScreenInactive
)

type rendererOutputGateAltScreenTransition struct {
	entered bool
	state   rendererOutputGateAltScreenMode
}

type rendererOutputGateAltScreenParser struct {
	state       rendererOutputGateAltScreenParserState
	private     bool
	param       int
	paramSet    bool
	hasMode1049 bool
}

type rendererOutputGateAltScreenParserState uint8

const (
	rendererOutputGateAltScreenParserGround rendererOutputGateAltScreenParserState = iota
	rendererOutputGateAltScreenParserEscape
	rendererOutputGateAltScreenParserCSI
)

func (p rendererOutputGateAltScreenParser) preview(payload []byte) (rendererOutputGateAltScreenParser, rendererOutputGateAltScreenTransition) {
	transition := rendererOutputGateAltScreenTransition{}
	for _, b := range payload {
		if state := p.advance(b); state != rendererOutputGateAltScreenUnchanged {
			transition.state = state
			if state == rendererOutputGateAltScreenActive {
				transition.entered = true
			}
		}
	}
	return p, transition
}

func (p *rendererOutputGateAltScreenParser) apply(payload []byte) rendererOutputGateAltScreenTransition {
	next, transition := p.preview(payload)
	*p = next
	return transition
}

func (p *rendererOutputGateAltScreenParser) advance(b byte) rendererOutputGateAltScreenMode {
	switch p.state {
	case rendererOutputGateAltScreenParserGround:
		if b == '\x1b' {
			p.state = rendererOutputGateAltScreenParserEscape
		} else if b == 0x9b {
			p.resetCSI()
		}
	case rendererOutputGateAltScreenParserEscape:
		switch b {
		case '[':
			p.resetCSI()
		case '\x1b':
			p.state = rendererOutputGateAltScreenParserEscape
		default:
			p.reset()
		}
	case rendererOutputGateAltScreenParserCSI:
		return p.advanceCSI(b)
	}
	return rendererOutputGateAltScreenUnchanged
}

func (p *rendererOutputGateAltScreenParser) advanceCSI(b byte) rendererOutputGateAltScreenMode {
	switch {
	case b == '?' && !p.private && !p.paramSet && !p.hasMode1049:
		p.private = true
	case b >= '0' && b <= '9':
		p.paramSet = true
		p.param = p.param*10 + int(b-'0')
	case b == ';':
		p.finishParam()
	case b >= 0x40 && b <= 0x7e:
		p.finishParam()
		state := rendererOutputGateAltScreenUnchanged
		if p.private && p.hasMode1049 {
			if b == 'h' {
				state = rendererOutputGateAltScreenActive
			} else if b == 'l' {
				state = rendererOutputGateAltScreenInactive
			}
		}
		p.reset()
		return state
	case b == '\x1b':
		p.state = rendererOutputGateAltScreenParserEscape
	default:
		if b < 0x20 {
			return rendererOutputGateAltScreenUnchanged
		}
	}
	return rendererOutputGateAltScreenUnchanged
}

func (p *rendererOutputGateAltScreenParser) finishParam() {
	if p.paramSet && p.param == 1049 {
		p.hasMode1049 = true
	}
	p.param = 0
	p.paramSet = false
}

func (p *rendererOutputGateAltScreenParser) resetCSI() {
	p.state = rendererOutputGateAltScreenParserCSI
	p.private = false
	p.param = 0
	p.paramSet = false
	p.hasMode1049 = false
}

func (p *rendererOutputGateAltScreenParser) reset() {
	*p = rendererOutputGateAltScreenParser{}
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
