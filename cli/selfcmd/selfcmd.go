package selfcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"core/shared/brand"
)

const fallbackBinaryName = brand.Command

func BuilderCommand() string {
	return formatBuilderCommand(currentExecutablePath())
}

func ContinueRunCommand(sessionID string) string {
	return formatContinueRunCommand(currentExecutablePath(), sessionID)
}

func formatBuilderCommand(executablePath string) string {
	executablePath = strings.TrimSpace(executablePath)
	if executablePath == "" {
		return fallbackBinaryName
	}
	if executablePath == fallbackBinaryName {
		return fallbackBinaryName
	}
	return strconv.Quote(executablePath)
}

func formatRunCommandPrefix(executablePath string) string {
	return formatBuilderCommand(executablePath) + " run"
}

func formatContinueRunCommand(executablePath, sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	return fmt.Sprintf("%s --continue %s %s", formatRunCommandPrefix(executablePath), sessionID, strconv.Quote("follow-up"))
}

func currentExecutablePath() string {
	path, err := os.Executable()
	if err != nil {
		return fallbackBinaryName
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fallbackBinaryName
	}
	cleaned := filepath.Clean(path)
	if strings.TrimSpace(cleaned) == "." {
		return fallbackBinaryName
	}
	return cleaned
}
