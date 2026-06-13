package postprocess

import (
	"context"
	"strings"
	"unicode"

	"core/shared/toolspec"

	xansi "github.com/charmbracelet/x/ansi"
)

type sanitizerProcessor struct{}

func (sanitizerProcessor) ID() string {
	return "builtin/sanitize-output"
}

func (p sanitizerProcessor) Process(_ context.Context, envelope Envelope) (Decision, error) {
	sanitized := SanitizeOutput(envelope.CurrentOutput)
	if sanitized == envelope.CurrentOutput {
		return Skip(envelope), nil
	}
	return Continue(envelope.WithCurrent(sanitized), p.ID()), nil
}

type goTestSuccessProcessor struct{}

func (goTestSuccessProcessor) ID() string {
	return "builtin/go-test-pass"
}

func (goTestSuccessProcessor) Scope() Scope {
	return Scope{
		ToolNames:    []toolspec.ID{toolspec.ToolExecCommand},
		CommandNames: []string{"go"},
		ExitCodes:    ExitCodeSuccess,
	}
}

func (p goTestSuccessProcessor) Process(_ context.Context, envelope Envelope) (Decision, error) {
	req := envelope.Request
	req.Output = envelope.CurrentOutput
	if len(req.ParsedArgs) < 2 {
		return Skip(envelope), nil
	}
	if strings.TrimSpace(req.ParsedArgs[1]) != "test" {
		return Skip(envelope), nil
	}
	if goTestRequiresDetailedOutput(req.ParsedArgs[2:]) {
		return Skip(envelope), nil
	}
	return Halt(envelope.WithCurrent("PASS"), p.ID()), nil
}

func goTestRequiresDetailedOutput(args []string) bool {
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		switch {
		case trimmed == "-bench", strings.HasPrefix(trimmed, "-bench="), strings.HasPrefix(trimmed, "--bench="):
			return true
		case trimmed == "-cover", strings.HasPrefix(trimmed, "-cover="), strings.HasPrefix(trimmed, "--cover="), strings.HasPrefix(trimmed, "-coverprofile="), strings.HasPrefix(trimmed, "-covermode="), strings.HasPrefix(trimmed, "-coverpkg="):
			return true
		case trimmed == "-json", trimmed == "--json":
			return true
		}
	}
	return false
}

func SanitizeOutput(s string) string {
	if s == "" {
		return s
	}

	stripped := xansi.Strip(s)
	normalized := strings.ReplaceAll(stripped, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	var b strings.Builder
	b.Grow(len(normalized))
	for _, r := range normalized {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
