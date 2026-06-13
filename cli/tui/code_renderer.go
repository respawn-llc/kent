package tui

import (
	"core/shared/theme"
	"bytes"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"core/shared/transcript"
	patchformat "core/shared/transcript/patchformat"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	xansi "github.com/charmbracelet/x/ansi"
)

const codeCacheLimit = 512

const (
	diffBlockMaxLines = 400
	diffBlockMaxBytes = 64 * 1024
)

type codeRenderer struct {
	theme          string
	baseForeground rgbColor
	styles         rendererStyleAdapter
	cache          map[string]string
	diffCache      map[string][]diffRenderedLine
	formatter      chroma.Formatter
}

type diffRenderKind string

const (
	diffRenderMeta    diffRenderKind = "meta"
	diffRenderAdd     diffRenderKind = "add"
	diffRenderRemove  diffRenderKind = "remove"
	diffRenderContext diffRenderKind = "context"
)

type diffRenderedLine struct {
	Kind diffRenderKind
	Text string
}

func newCodeRenderer(themeName string) *codeRenderer {
	return &codeRenderer{
		theme:          themeName,
		baseForeground: rgbColorFromHex(theme.ResolvePalette(themeName).Transcript.Foreground.TrueColor),
		styles:         newRendererStyleAdapter(themeName),
		cache:          make(map[string]string, 128),
		diffCache:      make(map[string][]diffRenderedLine, 64),
		formatter:      formatters.TTY256,
	}
}

func (r *codeRenderer) render(hint *transcript.ToolRenderHint, text string) (string, bool) {
	if hint == nil || !hint.Valid() || strings.TrimSpace(text) == "" {
		return "", false
	}
	if hint.Kind == transcript.ToolRenderKindDiff {
		return "", false
	}
	key := fmt.Sprintf("%s|%s|%s|%t|%x", r.theme, hint.Kind, hint.Path, hint.ResultOnly, hashString(text))
	if cached, ok := r.cache[key]; ok {
		return cached, true
	}

	lexer := r.resolveLexer(hint, text)
	if lexer == nil {
		return "", false
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return "", false
	}

	var out bytes.Buffer
	if err := r.formatter.Format(&out, withTransparentChromaBackgrounds(r.styles.baseChromaStyle(), chroma.MustParseColour(theme.ResolvePalette(r.styles.theme).Transcript.Foreground.TrueColor)), iterator); err != nil {
		return "", false
	}
	rendered := strings.TrimRight(out.String(), "\n")
	if strings.TrimSpace(rendered) == "" {
		return "", false
	}
	rendered = applyANSIStyleIntents(rendered, ansiIntentPalette{ThemeForeground: r.baseForeground}, ThemeForeground)

	if len(r.cache) >= codeCacheLimit {
		r.cache = make(map[string]string, 128)
	}
	r.cache[key] = rendered
	return rendered, true
}

func (r *codeRenderer) renderDiffLines(renderedPatch *patchformat.RenderedPatch, width int) ([]diffRenderedLine, bool) {
	if renderedPatch == nil || len(renderedPatch.DetailLines) == 0 {
		return nil, false
	}
	if width < 1 {
		width = 1
	}
	text := renderedPatch.DetailText()
	key := fmt.Sprintf("%s|diff|w=%d|%x", r.theme, width, hashString(text))
	if cached, ok := r.diffCache[key]; ok {
		return append([]diffRenderedLine(nil), cached...), true
	}
	lines := renderedPatch.DetailLines
	out := make([]diffRenderedLine, 0, len(lines))
	var currentLexer chroma.Lexer
	var inferredLexer chroma.Lexer
	type pendingCodeLine struct {
		kind diffRenderKind
		text string
	}
	pending := make([]pendingCodeLine, 0, 16)
	pendingBytes := 0
	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		plainLines := make([]string, 0, len(pending))
		for _, line := range pending {
			plainLines = append(plainLines, line.text)
		}
		source := strings.Join(plainLines, "\n")
		lexer := currentLexer
		if lexer == nil {
			if inferredLexer == nil {
				inferredLexer = lexers.Analyse(source)
			}
			lexer = inferredLexer
		}
		highlightedLines := r.highlightCodeBlock(lexer, source)
		if len(highlightedLines) != len(pending) {
			highlightedLines = plainLines
		}
		for idx, line := range pending {
			marker := " "
			switch line.kind {
			case diffRenderAdd:
				marker = "+"
			case diffRenderRemove:
				marker = "-"
			}
			content := highlightedLines[idx]
			wrapped := splitLines(wrapTextForViewport(content, max(1, width-1)))
			for chunkIdx, chunk := range wrapped {
				prefix := marker
				if chunkIdx > 0 {
					prefix = " "
				}
				out = append(out, diffRenderedLine{Kind: line.kind, Text: prefix + chunk})
			}
		}
		pending = pending[:0]
		pendingBytes = 0
	}
	appendPending := func(kind diffRenderKind, text string) {
		pending = append(pending, pendingCodeLine{kind: kind, text: text})
		pendingBytes += len(text) + 1
		if len(pending) >= diffBlockMaxLines || pendingBytes >= diffBlockMaxBytes {
			flushPending()
		}
	}
	for _, line := range lines {
		if line.Kind == patchformat.RenderedLineKindFile {
			flushPending()
			if lexer := lexers.Match(strings.TrimSpace(line.Path)); lexer != nil {
				currentLexer = lexer
				inferredLexer = nil
			}
			for _, chunk := range splitLines(wrapTextForViewport(line.Text, width)) {
				out = append(out, diffRenderedLine{Kind: diffRenderMeta, Text: applyANSIStyleIntents(chunk, ansiIntentPalette{ThemeForeground: r.baseForeground}, ThemeForeground)})
			}
			continue
		}
		if line.Kind == patchformat.RenderedLineKindDiff && strings.HasPrefix(line.Text, "+") && !strings.HasPrefix(line.Text, "+++") {
			appendPending(diffRenderAdd, line.Text[1:])
			continue
		}
		if line.Kind == patchformat.RenderedLineKindDiff && strings.HasPrefix(line.Text, "-") && !strings.HasPrefix(line.Text, "---") {
			appendPending(diffRenderRemove, line.Text[1:])
			continue
		}
		if line.Kind == patchformat.RenderedLineKindDiff && strings.HasPrefix(line.Text, " ") {
			appendPending(diffRenderContext, line.Text[1:])
			continue
		}
		flushPending()
		for _, chunk := range splitLines(wrapTextForViewport(line.Text, width)) {
			out = append(out, diffRenderedLine{Kind: diffRenderMeta, Text: applyANSIStyleIntents(chunk, ansiIntentPalette{ThemeForeground: r.baseForeground}, ThemeForeground)})
		}
	}
	flushPending()
	serialized := make([]string, 0, len(out))
	for _, line := range out {
		serialized = append(serialized, line.Text)
	}
	rendered := strings.TrimRight(strings.Join(serialized, "\n"), "\n")
	if strings.TrimSpace(rendered) == "" {
		return nil, false
	}
	if len(r.diffCache) >= codeCacheLimit {
		r.diffCache = make(map[string][]diffRenderedLine, 64)
	}
	r.diffCache[key] = append([]diffRenderedLine(nil), out...)
	return append([]diffRenderedLine(nil), out...), true
}

func (r *codeRenderer) resolveLexer(hint *transcript.ToolRenderHint, text string) chroma.Lexer {
	switch hint.Kind {
	case transcript.ToolRenderKindShell:
		return r.resolveShellLexer(hint)
	case transcript.ToolRenderKindDiff:
		return lexers.Get("diff")
	case transcript.ToolRenderKindSource:
		if pathHint := strings.TrimSpace(hint.Path); pathHint != "" {
			if lexer := lexers.Match(pathHint); lexer != nil {
				return lexer
			}
		}
		return lexers.Analyse(text)
	default:
		return nil
	}
}

func (r *codeRenderer) resolveShellLexer(hint *transcript.ToolRenderHint) chroma.Lexer {
	dialect := transcript.ToolShellDialectPosix
	if hint != nil {
		dialect = hint.ShellDialect
	}
	switch dialect {
	case transcript.ToolShellDialectPowerShell:
		return lexers.Get("powershell")
	case transcript.ToolShellDialectWindowsCommand:
		return lexers.Get("batch")
	case transcript.ToolShellDialectPosix:
		return lexers.Get("bash")
	default:
		if runtime.GOOS == "windows" {
			return lexers.Get("batch")
		}
		return lexers.Get("bash")
	}
}

func (r *codeRenderer) highlightCodeBlock(lexer chroma.Lexer, source string) []string {
	sourceLines := splitLines(source)
	if lexer == nil || source == "" {
		return r.applyDefaultForegroundToLines(sourceLines)
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, source)
	if err != nil {
		return r.applyDefaultForegroundToLines(sourceLines)
	}
	var out bytes.Buffer
	if err := r.formatter.Format(&out, withTransparentChromaBackgrounds(r.styles.baseChromaStyle(), chroma.MustParseColour(theme.ResolvePalette(r.styles.theme).Transcript.Foreground.TrueColor)), iterator); err != nil {
		return r.applyDefaultForegroundToLines(sourceLines)
	}
	raw := strings.TrimRight(strings.ReplaceAll(out.String(), "\r\n", "\n"), "\n")
	raw = applyANSIStyleIntents(raw, ansiIntentPalette{ThemeForeground: r.baseForeground}, ThemeForeground)
	highlighted := strings.Split(raw, "\n")
	if len(highlighted) < len(sourceLines) {
		padded := make([]string, len(sourceLines))
		copy(padded, highlighted)
		for idx := len(highlighted); idx < len(sourceLines); idx++ {
			padded[idx] = applyANSIStyleIntents(sourceLines[idx], ansiIntentPalette{ThemeForeground: r.baseForeground}, ThemeForeground)
		}
		return padded
	}
	if len(highlighted) > len(sourceLines) {
		highlighted = highlighted[:len(sourceLines)]
	}
	return highlighted
}

func (r *codeRenderer) applyDefaultForegroundToLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, applyANSIStyleIntents(line, ansiIntentPalette{ThemeForeground: r.baseForeground}, ThemeForeground))
	}
	return out
}

func applyBackgroundTint(line string, bg string) string {
	if line == "" || bg == "" {
		return line
	}
	if !strings.Contains(line, "\x1b[") {
		return bg + line + "\x1b[0m"
	}

	parser := xansi.GetParser()
	defer xansi.PutParser(parser)

	var out strings.Builder
	out.Grow(len(line) + len(bg)*4 + len("\x1b[0m"))
	out.WriteString(bg)

	state := byte(0)
	input := line
	for len(input) > 0 {
		seq, width, n, newState := xansi.GraphemeWidth.DecodeSequenceInString(input, state, parser)
		if n <= 0 {
			out.WriteString(input)
			break
		}
		state = newState
		input = input[n:]
		out.WriteString(seq)
		if width > 0 || xansi.Cmd(parser.Command()).Final() != 'm' {
			continue
		}
		if sgrAffectsBackground(parser.Params()) {
			out.WriteString(bg)
		}
	}
	out.WriteString("\x1b[0m")
	return out.String()
}

func sgrAffectsBackground(params xansi.Params) bool {
	if len(params) == 0 {
		return true
	}
	for idx := 0; idx < len(params); {
		param, _, ok := params.Param(idx, -1)
		if !ok || param < 0 {
			idx++
			continue
		}
		switch {
		case param == 0:
			return true
		case param == 38:
			idx += skipANSIExtendedColorParams(params, idx)
		case param == 48:
			return true
		case param == 49:
			return true
		case 40 <= param && param <= 47:
			return true
		case 100 <= param && param <= 107:
			return true
		default:
			idx++
		}
	}
	return false
}

func skipANSIExtendedColorParams(params xansi.Params, start int) int {
	mode, _, ok := params.Param(start+1, -1)
	if !ok || mode < 0 {
		return 1
	}
	if mode == 5 {
		return 3
	}
	if mode != 2 {
		return 1
	}
	r1, _, ok1 := params.Param(start+2, -1)
	g1, _, ok2 := params.Param(start+3, -1)
	b1, _, ok3 := params.Param(start+4, -1)
	if ok1 && ok2 && ok3 && r1 >= 0 && g1 >= 0 && b1 >= 0 {
		return 5
	}
	r2, _, ok1 := params.Param(start+3, -1)
	g2, _, ok2 := params.Param(start+4, -1)
	b2, _, ok3 := params.Param(start+5, -1)
	if ok1 && ok2 && ok3 && r2 >= 0 && g2 >= 0 && b2 >= 0 {
		return 6
	}
	return 1
}

func bgEscape(hex string) string {
	r, g, b, ok := parseHexColor(hex)
	if !ok {
		return ""
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

func parseHexColor(hex string) (int, int, int, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(v) != 6 {
		return 0, 0, 0, false
	}
	raw, err := strconv.ParseUint(v, 16, 24)
	if err != nil {
		return 0, 0, 0, false
	}
	r := int((raw >> 16) & 0xFF)
	g := int((raw >> 8) & 0xFF)
	b := int(raw & 0xFF)
	return r, g, b, true
}
