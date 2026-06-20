package prompts

import (
	"errors"
	"testing"
)

func TestShortCommandMatchesWhenLookPathResolvesToSameBinary(t *testing.T) {
	lookPath := func(string) (string, error) { return "/usr/local/bin/kent", nil }
	eval := func(p string) (string, error) {
		if p == "/usr/local/bin/kent" {
			return "/opt/kent/bin/kent", nil
		}
		return p, nil
	}
	if !shortCommandResolvesToRunningBinary("/opt/kent/bin/kent", lookPath, eval) {
		t.Fatal("expected short command to match running executable through symlink resolution")
	}
}

func TestShortCommandDoesNotMatchDifferentBinary(t *testing.T) {
	lookPath := func(string) (string, error) { return "/usr/local/bin/kent", nil }
	eval := func(p string) (string, error) { return p, nil }
	if shortCommandResolvesToRunningBinary("/opt/other/kent", lookPath, eval) {
		t.Fatal("expected mismatch when resolved binaries differ")
	}
}

func TestShortCommandDoesNotMatchWhenNotResolvable(t *testing.T) {
	lookPath := func(string) (string, error) { return "", errors.New("not found") }
	eval := func(p string) (string, error) { return p, nil }
	if shortCommandResolvesToRunningBinary("/opt/kent/bin/kent", lookPath, eval) {
		t.Fatal("expected verbose path when the short command is not resolvable")
	}
}

// Two distinct binaries can share the file name `kent`; the short command must
// only collapse onto the one it actually resolves to, never onto any same-named
// binary. A same-named binary at a different on-disk location must not collapse.
func TestShortCommandDoesNotMatchSameNamedDifferentBinary(t *testing.T) {
	lookPath := func(string) (string, error) { return "/usr/local/bin/kent", nil }
	eval := func(p string) (string, error) {
		if p == "/usr/local/bin/kent" {
			return "/opt/legit/kent", nil
		}
		return p, nil
	}
	if shortCommandResolvesToRunningBinary("/opt/other/kent", lookPath, eval) {
		t.Fatal("expected verbose path when a different binary shares the brand command name")
	}
}

func TestFormatLaunchCommandCollapsesPathThatMatchesShortCommand(t *testing.T) {
	if got := formatLaunchCommand(fallbackBinaryName); got != fallbackBinaryName {
		t.Fatalf("command = %q, want %q", got, fallbackBinaryName)
	}
}

func TestFormatRunCommandPrefixFallsBackToBinaryName(t *testing.T) {
	want := fallbackBinaryName + " run"
	if got := formatRunCommandPrefix(""); got != want {
		t.Fatalf("run command prefix = %q, want %q", got, want)
	}
}

func TestFormatRunCommandPrefixDoesNotQuoteFallbackBinaryName(t *testing.T) {
	want := fallbackBinaryName + " run"
	if got := formatRunCommandPrefix(fallbackBinaryName); got != want {
		t.Fatalf("run command prefix = %q, want %q", got, want)
	}
}

func TestFormatRunCommandPrefixQuotesExecutablePath(t *testing.T) {
	got := formatRunCommandPrefix("/tmp/path with space/kent")
	want := "\"/tmp/path with space/kent\" run"
	if got != want {
		t.Fatalf("run command prefix = %q, want %q", got, want)
	}
}

func TestFormatLaunchCommandQuotesExecutablePathWithoutSubcommand(t *testing.T) {
	got := formatLaunchCommand("/tmp/path with space/kent")
	want := "\"/tmp/path with space/kent\""
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestFormatContinueRunCommandForPath(t *testing.T) {
	got := formatContinueRunCommand("/tmp/kent", "session-123")
	want := "\"/tmp/kent\" run --continue \"session-123\" \"follow-up\""
	if got != want {
		t.Fatalf("continue run command = %q, want %q", got, want)
	}
}

func TestFormatContinueRunCommandForFallbackBinaryName(t *testing.T) {
	got := formatContinueRunCommand(fallbackBinaryName, "session-123")
	want := fallbackBinaryName + " run --continue \"session-123\" \"follow-up\""
	if got != want {
		t.Fatalf("continue run command = %q, want %q", got, want)
	}
}

func TestFormatContinueRunCommandWithRootIncludesPersistenceRoot(t *testing.T) {
	got := formatContinueRunCommandWithRoot(fallbackBinaryName, "session-123", "/tmp/iso-root")
	want := fallbackBinaryName + " run --persistence-root \"/tmp/iso-root\" --continue \"session-123\" \"follow-up\""
	if got != want {
		t.Fatalf("continue run command = %q, want %q", got, want)
	}
}

func TestFormatContinueRunCommandWithRootOmitsEmptyRoot(t *testing.T) {
	got := formatContinueRunCommandWithRoot(fallbackBinaryName, "session-123", "  ")
	want := fallbackBinaryName + " run --continue \"session-123\" \"follow-up\""
	if got != want {
		t.Fatalf("continue run command = %q, want %q", got, want)
	}
}
