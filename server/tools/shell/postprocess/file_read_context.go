package postprocess

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"builder/server/tools/shellcmd"
	"builder/shared/toolspec"
)

const maxFileReadContextFileBytes int64 = 1024 * 1024
const unknownSedScript = "\x00unknown-sed-script"

type fileReadContextProcessor struct{}

type fileReadCandidate struct {
	path                      string
	certainlyFull             bool
	fullWhenLineCountAtMost   int
	canInferFullFromLineCount bool
}

func (fileReadContextProcessor) ID() string {
	return "builtin/file-read-context"
}

func (fileReadContextProcessor) Scope() Scope {
	return Scope{
		ToolNames: []toolspec.ID{toolspec.ToolExecCommand},
		ExitCodes: ExitCodeSuccess,
	}
}

func (p fileReadContextProcessor) Process(_ context.Context, envelope Envelope) (Decision, error) {
	req := envelope.Request
	req.Output = envelope.CurrentOutput
	if len(req.ParsedArgs) < 2 || strings.HasPrefix(req.Output, "[Total line count: ") {
		return Skip(envelope), nil
	}

	args, ok := fileReadArgsWithoutCommand(req.CommandName, req.ParsedArgs)
	if !ok {
		return Skip(envelope), nil
	}
	candidate, ok := classifyFileRead(req.CommandName, args)
	if !ok {
		return Skip(envelope), nil
	}
	if candidate.certainlyFull {
		return Skip(envelope), nil
	}
	path, ok := resolveReadPath(req.Workdir, candidate.path)
	if !ok {
		return Skip(envelope), nil
	}
	lineCount, ok := countSmallRegularFileLines(path)
	if !ok {
		return Skip(envelope), nil
	}
	if candidate.canInferFullFromLineCount && lineCount <= candidate.fullWhenLineCountAtMost {
		return Skip(envelope), nil
	}

	return Continue(envelope.WithCurrent(fmt.Sprintf("[Total line count: %d]\n%s", lineCount, req.Output)), p.ID()), nil
}

func fileReadArgsWithoutCommand(commandName string, parsedArgs []string) ([]string, bool) {
	normalizedCommand := shellcmd.NormalizeCommandName(commandName)
	if normalizedCommand == "" || len(parsedArgs) < 2 {
		return nil, false
	}
	if shellcmd.NormalizeCommandName(parsedArgs[0]) != normalizedCommand {
		return nil, false
	}
	return parsedArgs[1:], true
}

func classifyFileRead(commandName string, args []string) (fileReadCandidate, bool) {
	switch strings.ToLower(strings.TrimSpace(commandName)) {
	case "sed":
		return classifySedFileRead(args)
	case "head":
		return classifyHeadTailFileRead(false, args)
	case "tail":
		return classifyHeadTailFileRead(true, args)
	case "get-content", "gc":
		return classifyPowerShellGetContent(args)
	default:
		return fileReadCandidate{}, false
	}
}

func resolveReadPath(workdir string, rawPath string) (string, bool) {
	path := strings.TrimSpace(rawPath)
	if path == "" || strings.ContainsAny(path, "*?[") {
		return "", false
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", false
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), true
	}
	base := strings.TrimSpace(workdir)
	if base == "" {
		return "", false
	}
	return filepath.Clean(filepath.Join(base, path)), true
}

func countSmallRegularFileLines(path string) (int, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileReadContextFileBytes {
		return 0, false
	}

	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	lines := 0
	seenBytes := false
	endedWithNewline := false
	buf := make([]byte, 32*1024)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			seenBytes = true
			chunk := buf[:n]
			endedWithNewline = chunk[len(chunk)-1] == '\n'
			for _, b := range chunk {
				if b == 0 {
					return 0, false
				}
				if b == '\n' {
					lines++
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, false
		}
	}
	if seenBytes && !endedWithNewline {
		lines++
	}
	return lines, true
}

func classifyHeadTailFileRead(isTail bool, args []string) (fileReadCandidate, bool) {
	lineLimit := 10
	limitKnown := true
	wholeFileRead := false
	paths := make([]string, 0, 1)

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "":
			return fileReadCandidate{}, false
		case arg == "--":
			paths = append(paths, args[i+1:]...)
			i = len(args)
		case arg == "-n" || arg == "--lines":
			if i+1 >= len(args) {
				return fileReadCandidate{}, false
			}
			nextLimit, wholeFile, ok := parseHeadTailLineLimit(isTail, args[i+1])
			if !ok {
				limitKnown = false
				wholeFileRead = false
			} else if wholeFile {
				limitKnown = false
				wholeFileRead = true
			} else {
				lineLimit = nextLimit
				limitKnown = true
				wholeFileRead = false
			}
			i++
		case strings.HasPrefix(arg, "--lines="):
			nextLimit, wholeFile, ok := parseHeadTailLineLimit(isTail, strings.TrimPrefix(arg, "--lines="))
			if !ok {
				limitKnown = false
				wholeFileRead = false
			} else if wholeFile {
				limitKnown = false
				wholeFileRead = true
			} else {
				lineLimit = nextLimit
				limitKnown = true
				wholeFileRead = false
			}
		case arg == "-c" || arg == "--bytes":
			return fileReadCandidate{}, false
		case strings.HasPrefix(arg, "--bytes="), strings.HasPrefix(arg, "-c"):
			return fileReadCandidate{}, false
		case isHeadTailCompactLineOption(arg):
			nextLimit, ok := parsePositiveLineLimit(strings.TrimPrefix(arg, "-"))
			if !ok {
				limitKnown = false
				wholeFileRead = false
			} else {
				lineLimit = nextLimit
				limitKnown = true
				wholeFileRead = false
			}
		case strings.HasPrefix(arg, "-n"):
			nextLimit, wholeFile, ok := parseHeadTailLineLimit(isTail, strings.TrimPrefix(arg, "-n"))
			if !ok {
				limitKnown = false
				wholeFileRead = false
			} else if wholeFile {
				limitKnown = false
				wholeFileRead = true
			} else {
				lineLimit = nextLimit
				limitKnown = true
				wholeFileRead = false
			}
		case arg == "-q" || arg == "-v" || arg == "-z" || arg == "--quiet" || arg == "--silent" || arg == "--verbose" || arg == "--zero-terminated":
		case strings.HasPrefix(arg, "-"):
			return fileReadCandidate{}, false
		default:
			paths = append(paths, arg)
		}
	}

	path, ok := singlePath(paths)
	if !ok {
		return fileReadCandidate{}, false
	}
	if wholeFileRead {
		return fileReadCandidate{}, false
	}
	candidate := fileReadCandidate{path: path}
	if limitKnown {
		candidate.fullWhenLineCountAtMost = lineLimit
		candidate.canInferFullFromLineCount = true
	}
	return candidate, true
}

func isHeadTailCompactLineOption(arg string) bool {
	if len(arg) < 2 || arg[0] != '-' || arg[1] == '-' || arg[1] == 'n' || arg[1] == 'c' {
		return false
	}
	for _, r := range arg[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseHeadTailLineLimit(isTail bool, value string) (int, bool, bool) {
	trimmed := strings.TrimSpace(value)
	if isTail && strings.HasPrefix(trimmed, "+") {
		startLine, ok := parsePositiveLineLimit(strings.TrimPrefix(trimmed, "+"))
		if !ok {
			return 0, false, false
		}
		return 0, startLine <= 1, true
	}
	if isTail && strings.HasPrefix(trimmed, "-") {
		lineLimit, ok := parsePositiveLineLimit(strings.TrimPrefix(trimmed, "-"))
		return lineLimit, false, ok
	}
	lineLimit, ok := parsePositiveLineLimit(trimmed)
	return lineLimit, false, ok
}

func parsePositiveLineLimit(value string) (int, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

func classifySedFileRead(args []string) (fileReadCandidate, bool) {
	scripts := make([]string, 0, 1)
	files := make([]string, 0, 1)
	suppressDefault := false
	scriptFromOperand := false

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "":
			return fileReadCandidate{}, false
		case arg == "--":
			if !scriptFromOperand && len(scripts) == 0 && i+1 < len(args) {
				scripts = append(scripts, args[i+1])
				scriptFromOperand = true
				files = append(files, args[i+2:]...)
			} else {
				files = append(files, args[i+1:]...)
			}
			i = len(args)
		case arg == "-n" || arg == "--quiet" || arg == "--silent":
			suppressDefault = true
		case arg == "-e" || arg == "--expression":
			if i+1 >= len(args) {
				return fileReadCandidate{}, false
			}
			scripts = append(scripts, args[i+1])
			i++
		case strings.HasPrefix(arg, "-e") && len(arg) > 2:
			scripts = append(scripts, strings.TrimPrefix(arg, "-e"))
		case strings.HasPrefix(arg, "--expression="):
			scripts = append(scripts, strings.TrimPrefix(arg, "--expression="))
		case arg == "-f" || arg == "--file":
			if i+1 >= len(args) {
				return fileReadCandidate{}, false
			}
			scripts = append(scripts, unknownSedScript)
			i++
		case strings.HasPrefix(arg, "--file="):
			scripts = append(scripts, unknownSedScript)
		case strings.HasPrefix(arg, "-") && !scriptFromOperand:
			return fileReadCandidate{}, false
		case !scriptFromOperand && len(scripts) == 0:
			scripts = append(scripts, arg)
			scriptFromOperand = true
		default:
			files = append(files, arg)
		}
	}

	path, ok := singlePath(files)
	if !ok || len(scripts) == 0 {
		return fileReadCandidate{}, false
	}
	classification, ok := classifySedScripts(scripts, suppressDefault)
	if !ok {
		return fileReadCandidate{}, false
	}
	candidate := fileReadCandidate{path: path, certainlyFull: classification.certainlyFull}
	if classification.canInferFullFromLineCount {
		candidate.fullWhenLineCountAtMost = classification.fullWhenLineCountAtMost
		candidate.canInferFullFromLineCount = true
	}
	return candidate, true
}

type sedScriptClassification struct {
	certainlyFull             bool
	fullWhenLineCountAtMost   int
	canInferFullFromLineCount bool
}

func classifySedScripts(scripts []string, suppressDefault bool) (sedScriptClassification, bool) {
	classifications := make([]sedScriptClassification, 0, len(scripts))
	anyPartial := false
	for _, script := range scripts {
		classification, ok := classifySedScript(script, suppressDefault)
		if !ok {
			return sedScriptClassification{}, false
		}
		classifications = append(classifications, classification)
		if !classification.certainlyFull {
			anyPartial = true
		}
	}
	if !anyPartial {
		return sedScriptClassification{certainlyFull: true}, true
	}
	if len(classifications) == 1 && classifications[0].canInferFullFromLineCount {
		return classifications[0], true
	}
	return sedScriptClassification{}, true
}

func classifySedScript(script string, suppressDefault bool) (sedScriptClassification, bool) {
	if script == unknownSedScript {
		return sedScriptClassification{}, true
	}
	trimmed := strings.TrimSpace(script)
	if trimmed == "" {
		return sedScriptClassification{certainlyFull: !suppressDefault}, true
	}
	summary, ok := sedSingleCommand(trimmed)
	if !ok {
		return sedScriptClassification{}, false
	}
	if summary.command == 'p' && suppressDefault {
		return sedScriptClassification{
			certainlyFull:             !summary.hasAddress || summary.fullRange,
			fullWhenLineCountAtMost:   summary.fullWhenLineCountAtMost,
			canInferFullFromLineCount: summary.canInferFullFromLineCount,
		}, true
	}
	if summary.command == 'd' && !suppressDefault {
		return sedScriptClassification{}, true
	}
	return sedScriptClassification{}, false
}

type sedCommandSummary struct {
	command                   byte
	hasAddress                bool
	fullRange                 bool
	fullWhenLineCountAtMost   int
	canInferFullFromLineCount bool
}

func sedSingleCommand(script string) (sedCommandSummary, bool) {
	trimmed := strings.TrimSpace(script)
	i, firstAddress, ok := consumeSedAddress(trimmed, 0)
	if !ok {
		return sedCommandSummary{}, false
	}
	summary := sedCommandSummary{hasAddress: firstAddress.present}
	rest := strings.TrimSpace(trimmed[i:])
	if firstAddress.present && strings.HasPrefix(rest, ",") {
		rangeTail := strings.TrimSpace(strings.TrimPrefix(rest, ","))
		next, secondAddress, ok := consumeSedAddress(rangeTail, 0)
		if !ok || !secondAddress.present {
			return sedCommandSummary{}, false
		}
		summary.fullRange = firstAddress.numeric && firstAddress.line == 1 && secondAddress.endOfFile
		if firstAddress.numeric && firstAddress.line == 1 && secondAddress.numeric {
			summary.fullWhenLineCountAtMost = secondAddress.line
			summary.canInferFullFromLineCount = true
		}
		rest = strings.TrimSpace(rangeTail[next:])
	}
	if firstAddress.present && strings.HasPrefix(rest, "!") {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "!"))
		summary.fullRange = false
		summary.canInferFullFromLineCount = false
	}
	if len(rest) != 1 {
		return sedCommandSummary{}, false
	}
	summary.command = rest[0]
	return summary, true
}

type sedAddress struct {
	present   bool
	numeric   bool
	line      int
	endOfFile bool
}

func consumeSedAddress(script string, start int) (int, sedAddress, bool) {
	i := start
	for i < len(script) && script[i] == ' ' {
		i++
	}
	if i >= len(script) {
		return i, sedAddress{}, true
	}
	switch {
	case script[i] >= '0' && script[i] <= '9':
		start := i
		for i < len(script) && script[i] >= '0' && script[i] <= '9' {
			i++
		}
		line, ok := parsePositiveLineLimit(script[start:i])
		if !ok {
			return 0, sedAddress{}, false
		}
		return i, sedAddress{present: true, numeric: true, line: line}, true
	case script[i] == '$':
		return i + 1, sedAddress{present: true, endOfFile: true}, true
	case script[i] == '/':
		end, ok := consumeSedDelimitedAddress(script, i, '/')
		return end, sedAddress{present: ok}, ok
	default:
		return i, sedAddress{}, true
	}
}

func consumeSedDelimitedAddress(script string, start int, delimiter byte) (int, bool) {
	escaped := false
	for i := start + 1; i < len(script); i++ {
		if escaped {
			escaped = false
			continue
		}
		if script[i] == '\\' {
			escaped = true
			continue
		}
		if script[i] == delimiter {
			return i + 1, true
		}
	}
	return 0, false
}

func classifyPowerShellGetContent(args []string) (fileReadCandidate, bool) {
	paths := make([]string, 0, 1)
	partial := false
	lineLimit := 0
	limitKnown := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		switch {
		case arg == "":
			return fileReadCandidate{}, false
		case lower == "-path" || lower == "-literalpath":
			if i+1 >= len(args) {
				return fileReadCandidate{}, false
			}
			paths = append(paths, args[i+1])
			i++
		case lower == "-totalcount" || lower == "-head" || lower == "-first" || lower == "-tail":
			if i+1 >= len(args) {
				return fileReadCandidate{}, false
			}
			partial = true
			if nextLimit, ok := parsePositiveLineLimit(args[i+1]); ok {
				lineLimit = nextLimit
				limitKnown = true
			}
			i++
		case strings.HasPrefix(lower, "-totalcount:"):
			partial = true
			if nextLimit, ok := parsePositiveLineLimit(strings.TrimPrefix(lower, "-totalcount:")); ok {
				lineLimit = nextLimit
				limitKnown = true
			}
		case strings.HasPrefix(lower, "-head:"):
			partial = true
			if nextLimit, ok := parsePositiveLineLimit(strings.TrimPrefix(lower, "-head:")); ok {
				lineLimit = nextLimit
				limitKnown = true
			}
		case strings.HasPrefix(lower, "-first:"):
			partial = true
			if nextLimit, ok := parsePositiveLineLimit(strings.TrimPrefix(lower, "-first:")); ok {
				lineLimit = nextLimit
				limitKnown = true
			}
		case strings.HasPrefix(lower, "-tail:"):
			partial = true
			if nextLimit, ok := parsePositiveLineLimit(strings.TrimPrefix(lower, "-tail:")); ok {
				lineLimit = nextLimit
				limitKnown = true
			}
		case strings.HasPrefix(arg, "-"):
			return fileReadCandidate{}, false
		default:
			paths = append(paths, arg)
		}
	}
	if !partial {
		return fileReadCandidate{}, false
	}
	path, ok := singlePath(paths)
	if !ok {
		return fileReadCandidate{}, false
	}
	candidate := fileReadCandidate{path: path}
	if limitKnown {
		candidate.fullWhenLineCountAtMost = lineLimit
		candidate.canInferFullFromLineCount = true
	}
	return candidate, true
}

func singlePath(paths []string) (string, bool) {
	if len(paths) != 1 {
		return "", false
	}
	path := strings.TrimSpace(paths[0])
	return path, path != ""
}
