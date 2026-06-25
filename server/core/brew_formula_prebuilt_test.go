package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Homebrew kent formula must install prebuilt binaries (release archives /
// bottles), never compile from source. A source build in the formula means
// `brew install kent` silently falls back to a full toolchain build whenever a
// bottle is missing, which has regressed releases before. This guard fails the
// build if the generated formula template reintroduces build-time dependencies
// or install-time build commands. The tap repo enforces the same invariant on
// the committed Formula/kent.rb.
func TestBrewTapGeneratorEmitsPrebuiltBinaryFormula(t *testing.T) {
	repoRoot := findRepoRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "update-brew-tap.sh")
	formula := brewFormulaTemplateLines(t, scriptPath)
	installBody := rubyMethodBody(t, formula, "install")

	assertRubyLinePresent(t, installBody, `bin.install "kent_#{version}_#{os}_#{arch}" => "kent"`)
	for _, line := range installBody {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) > 0 && fields[0] == "system" {
			t.Fatalf("formula install method runs a build command: %q", strings.TrimSpace(line))
		}
	}

	for _, dependency := range []string{`depends_on "go"`, `depends_on "rust"`, `depends_on "node"`} {
		if rubyLinePresent(formula, dependency) {
			t.Fatalf("formula declares build-time dependency %s; the kent formula must install prebuilt binaries only", dependency)
		}
	}
}

func brewFormulaTemplateLines(t *testing.T, scriptPath string) []string {
	t.Helper()
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	lines := strings.Split(string(content), "\n")
	start := -1
	for idx, line := range lines {
		if strings.TrimSpace(line) == `cat >"$tmp_formula" <<EOF` {
			start = idx + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("formula heredoc not found in %s", scriptPath)
	}
	for idx := start; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "EOF" {
			return lines[start:idx]
		}
	}
	t.Fatalf("formula heredoc terminator not found in %s", scriptPath)
	return nil
}

func rubyMethodBody(t *testing.T, lines []string, name string) []string {
	t.Helper()
	startLine := "def " + name
	start := -1
	for idx, line := range lines {
		if strings.TrimSpace(line) == startLine {
			start = idx + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("Ruby method %q not found in formula template", name)
	}
	for idx := start; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "end" {
			return lines[start:idx]
		}
	}
	t.Fatalf("Ruby method %q terminator not found in formula template", name)
	return nil
}

func assertRubyLinePresent(t *testing.T, lines []string, want string) {
	t.Helper()
	if !rubyLinePresent(lines, want) {
		t.Fatalf("formula template missing required line: %s", want)
	}
}

func rubyLinePresent(lines []string, want string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
