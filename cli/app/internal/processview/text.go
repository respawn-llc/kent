package processview

import "strings"

const (
	inlineMetaSeparator = "\x1f"
	commandFallback     = "tool call"
)

func CompactCommandText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return commandFallback
	}
	parts := strings.SplitN(trimmed, "\n", 2)
	first := strings.TrimSpace(parts[0])
	if first == "" {
		return commandFallback
	}
	command, _ := splitInlineMeta(first)
	if command == "" {
		return commandFallback
	}
	return command
}

func splitInlineMeta(line string) (string, string) {
	parts := strings.SplitN(line, inlineMetaSeparator, 2)
	command := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return command, ""
	}
	return command, strings.TrimSpace(parts[1])
}
