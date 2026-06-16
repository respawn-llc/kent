package selfcmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"core/shared/brand"
)

const fallbackBinaryName = brand.Command

func LaunchCommand() string {
	return formatLaunchCommand(effectiveExecutablePath())
}

func ContinueRunCommand(sessionID string) string {
	return formatContinueRunCommand(effectiveExecutablePath(), sessionID)
}

// effectiveExecutablePath prefers the short binary name (e.g. `kent`) over the
// verbose absolute path whenever the short command resolves on PATH to the same
// binary that is currently running. This keeps injected commands terse for the
// common case while still surfacing the full path when `kent` is not on PATH or
// points at a different binary than the running executable.
func effectiveExecutablePath() string {
	path := currentExecutablePath()
	if shortCommandMatches(path, exec.LookPath, filepath.EvalSymlinks) {
		return fallbackBinaryName
	}
	return path
}

// shortCommandMatches reports whether resolving the short binary name on PATH
// yields the same on-disk binary as executablePath, after symlink resolution.
func shortCommandMatches(executablePath string, lookPath func(string) (string, error), evalSymlinks func(string) (string, error)) bool {
	executablePath = strings.TrimSpace(executablePath)
	if executablePath == "" || executablePath == fallbackBinaryName {
		return false
	}
	resolved, err := lookPath(fallbackBinaryName)
	if err != nil {
		return false
	}
	canonicalResolved := canonicalPath(resolved, evalSymlinks)
	canonicalExecutable := canonicalPath(executablePath, evalSymlinks)
	if canonicalResolved == "" || canonicalExecutable == "" {
		return false
	}
	return canonicalResolved == canonicalExecutable
}

// canonicalPath resolves symlinks (best effort) and cleans the path so two
// references to the same binary compare equal.
func canonicalPath(path string, evalSymlinks func(string) (string, error)) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if resolved, err := evalSymlinks(path); err == nil {
		path = strings.TrimSpace(resolved)
	}
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func formatLaunchCommand(executablePath string) string {
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
	return formatLaunchCommand(executablePath) + " run"
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
