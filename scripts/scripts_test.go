package scripts_test

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Dir(filepath.Dir(file))
}

func TestReleaseArtifactsReportsMissingOptionValue(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "release-artifacts.sh")
	cmd := exec.Command("bash", script, "smoke-test", "--version", "v1.2.3", "--goos")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected missing option value failure")
	}
	text := string(output)
	if !strings.Contains(text, "Missing required argument value: --goos") {
		t.Fatalf("expected clear missing option error, got %q", text)
	}
	if strings.Contains(text, "shift count out of range") {
		t.Fatalf("expected guarded argument failure instead of shift error, got %q", text)
	}
}

func TestUpdateBrewTapReportsNotInsideGitRepo(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "update-brew-tap.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = t.TempDir()
	cmd.Env = append(sanitizedScriptTestEnv(os.Environ()), gitHookEnv(t, root)...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected git repo probe to fail outside repo")
	}
	text := string(output)
	if !strings.Contains(text, "Not inside a git repo") {
		t.Fatalf("expected explicit git repo error, got %q", text)
	}
}

func TestUpdateDepsDryRunPlansSupportedEcosystems(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "update-deps.sh")
	cmd := exec.Command("bash", script, "--dry-run")
	cmd.Dir = root
	cmd.Env = sanitizedScriptTestEnv(os.Environ())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected dry run to succeed: %v (%s)", err, output)
	}
	text := string(output)
	for _, needle := range []string{
		"==> Updating Go module dependencies",
		"[dry-run] go get -u -t ./...",
		"[dry-run] go mod tidy",
		"==> Updating docs pnpm dependencies",
		"[dry-run] pnpm --dir",
		"up --latest",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("expected %q in output, got %q", needle, text)
		}
	}
	if strings.Contains(text, "github-actions") {
		t.Fatalf("expected dry run to exclude GitHub Actions, got %q", text)
	}
}

func TestUpdateDepsUnknownArgument(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "update-deps.sh")
	cmd := exec.Command("bash", script, "--wat")
	cmd.Dir = root
	cmd.Env = sanitizedScriptTestEnv(os.Environ())
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected unknown argument failure")
	}
	text := string(output)
	if !strings.Contains(text, "Unknown argument: --wat") {
		t.Fatalf("expected explicit unknown arg error, got %q", text)
	}
	if !strings.Contains(text, "Usage: scripts/update-deps.sh") {
		t.Fatalf("expected usage output, got %q", text)
	}
}

func TestSandboxServeUpReportsHostPortInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on local port: %v", err)
	}
	defer listener.Close()

	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "sandbox-serve.sh")
	port := listener.Addr().(*net.TCPAddr).Port
	binDir := t.TempDir()
	fakeDocker := filepath.Join(binDir, "docker")
	if err := os.WriteFile(fakeDocker, []byte("#!/usr/bin/env bash\nif [ \"${1:-}\" = info ]; then exit 0; fi\nif [ \"${1:-}\" = container ] && [ \"${2:-}\" = inspect ]; then exit 1; fi\necho unexpected docker \"$@\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	cmd := exec.Command("bash", script, "up", "--host-port", strconv.Itoa(port))
	cmd.Dir = root
	cmd.Env = append(sanitizedScriptTestEnv(os.Environ()), "PATH="+binDir+string(os.PathListSeparator)+mustLookupEnv(t, "PATH"))
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected occupied host port failure")
	}
	text := string(output)
	expected := "host port " + strconv.Itoa(port) + " is already in use"
	if !strings.Contains(text, expected) {
		t.Fatalf("expected %q in output, got %q", expected, text)
	}
	if strings.Contains(text, "build sandbox image") {
		t.Fatalf("expected port preflight before image build, got %q", text)
	}
}

func TestWindowsInstallerScriptDocumentsSupportedFlags(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	text := string(data)
	for _, needle := range []string{
		"-Version <vX.Y.Z|X.Y.Z>",
		"-InstallDir <path>",
		"-Yes",
		"-NoPath",
		"-NoDeps",
		"-Uninstall",
		"-Force",
		"-NoServiceRestart",
		"BUILDER_RELEASE_BASE",
		"Git.Git",
		"BurntSushi.ripgrep.MSVC",
		"IsWow64Process2",
		"UninstallString",
		"checksums.txt",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("install.ps1 missing %q", needle)
		}
	}
}

func TestWindowsInstallerStopsRunningServiceBeforeReplacingBinary(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	text := string(data)
	stopIndex := strings.Index(text, "Stop-ServiceForUpdate $target")
	installIndex := strings.Index(text, "Install-ArchiveBinary $extractedBinary $target")
	if stopIndex < 0 {
		t.Fatal("install.ps1 does not stop service before binary replacement")
	}
	if installIndex < 0 {
		t.Fatal("install.ps1 does not install archive binary")
	}
	if stopIndex > installIndex {
		t.Fatalf("service stop occurs after binary replacement: stop=%d install=%d", stopIndex, installIndex)
	}
}

func TestWindowsInstallerRestartsStoppedServiceAfterFailedUpdate(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	text := string(data)
	for _, needle := range []string{
		"$serviceStoppedForUpdate = [bool](Stop-ServiceForUpdate $target)",
		"if ($serviceStoppedForUpdate -and -not $installSucceeded)",
		"Restart-ServiceAfterFailedUpdate $target",
		"service may be left stopped",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("install.ps1 missing service recovery behavior %q", needle)
		}
	}
}

func TestWindowsInstallerScriptAvoidsPowerShell7OnlySyntax(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	text := string(data)
	for _, forbidden := range []string{"??", "ForEach-Object -Parallel"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("install.ps1 contains PowerShell 7-only syntax %q", forbidden)
		}
	}
}

func TestTestScriptEnforcesWallClockTimeout(t *testing.T) {
	root := repoRoot(t)
	relPkg := newSlowTestPackage(t, root, "5 * time.Second")

	script := filepath.Join(root, "scripts", "test.sh")
	cmd := exec.Command(script, relPkg, "-count=1")
	cmd.Dir = root
	cmd.Env = append(
		sanitizedScriptTestEnv(os.Environ()),
		"BUILDER_TEST_DISABLE_WALL_CLOCK_CAP=0",
		"BUILDER_TEST_TIMEOUT_SECONDS=1",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected timeout failure")
	}
	text := string(output)
	if !strings.Contains(text, "test suite exceeded 1s wall-clock cap") {
		t.Fatalf("expected timeout message, got %q", text)
	}
}

func TestTestScriptCanDisableWallClockTimeout(t *testing.T) {
	root := repoRoot(t)
	relPkg := newSlowTestPackage(t, root, "1500 * time.Millisecond")

	script := filepath.Join(root, "scripts", "test.sh")
	cmd := exec.Command(script, relPkg, "-count=1")
	cmd.Dir = root
	cmd.Env = append(
		sanitizedScriptTestEnv(os.Environ()),
		"BUILDER_TEST_DISABLE_WALL_CLOCK_CAP=1",
		"BUILDER_TEST_TIMEOUT_SECONDS=1",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected disabled wall-clock timeout to pass: %v (%s)", err, output)
	}
}

func TestTestScriptRunsFrontendForDefaultFullSuite(t *testing.T) {
	root := repoRoot(t)
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "commands.log")
	writeLoggingFakeCommand(t, binDir, "go")
	writeLoggingFakeCommand(t, binDir, "pnpm")

	cmd := exec.Command(filepath.Join(root, "scripts", "test.sh"))
	cmd.Dir = root
	cmd.Env = append(
		sanitizedScriptTestEnv(os.Environ()),
		"SCRIPT_TEST_LOG="+logPath,
		"BUILDER_TEST_FRONTEND=auto",
		"PATH="+binDir+string(os.PathListSeparator)+mustLookupEnv(t, "PATH"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected test script to pass: %v (%s)", err, output)
	}

	logText := readTextFile(t, logPath)
	for _, needle := range []string{
		"go test ./...",
		"pnpm --dir apps install --frozen-lockfile",
		"pnpm --dir apps test",
	} {
		if !strings.Contains(logText, needle) {
			t.Fatalf("expected %q in command log, got %q", needle, logText)
		}
	}
}

func TestTestScriptSkipsFrontendForTargetedGoArgs(t *testing.T) {
	root := repoRoot(t)
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "commands.log")
	writeLoggingFakeCommand(t, binDir, "go")
	writeLoggingFakeCommand(t, binDir, "pnpm")

	cmd := exec.Command(filepath.Join(root, "scripts", "test.sh"), "./server/...", "-count=1")
	cmd.Dir = root
	cmd.Env = append(
		sanitizedScriptTestEnv(os.Environ()),
		"SCRIPT_TEST_LOG="+logPath,
		"BUILDER_TEST_FRONTEND=auto",
		"PATH="+binDir+string(os.PathListSeparator)+mustLookupEnv(t, "PATH"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected test script to pass: %v (%s)", err, output)
	}

	logText := readTextFile(t, logPath)
	if !strings.Contains(logText, "go test ./server/... -count=1") {
		t.Fatalf("expected targeted go test in command log, got %q", logText)
	}
	if strings.Contains(logText, "pnpm ") {
		t.Fatalf("expected targeted go test to skip frontend, got %q", logText)
	}
}

func TestTestScriptCanForceFrontendForTargetedGoArgs(t *testing.T) {
	root := repoRoot(t)
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "commands.log")
	writeLoggingFakeCommand(t, binDir, "go")
	writeLoggingFakeCommand(t, binDir, "pnpm")

	cmd := exec.Command(filepath.Join(root, "scripts", "test.sh"), "./server/...", "-count=1")
	cmd.Dir = root
	cmd.Env = append(
		sanitizedScriptTestEnv(os.Environ()),
		"SCRIPT_TEST_LOG="+logPath,
		"BUILDER_TEST_FRONTEND=1",
		"PATH="+binDir+string(os.PathListSeparator)+mustLookupEnv(t, "PATH"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected test script to pass: %v (%s)", err, output)
	}

	logText := readTextFile(t, logPath)
	for _, needle := range []string{
		"go test ./server/... -count=1",
		"pnpm --dir apps install --frozen-lockfile",
		"pnpm --dir apps test",
	} {
		if !strings.Contains(logText, needle) {
			t.Fatalf("expected %q in command log, got %q", needle, logText)
		}
	}
}

func TestBuildScriptSkipFrontendSuppressesFrontendBuild(t *testing.T) {
	root := repoRoot(t)
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "commands.log")
	outPath := filepath.Join(t.TempDir(), "builder")
	writeFakeCommand(t, binDir, "go", `#!/usr/bin/env bash
printf 'go %s\n' "$*" >>"$SCRIPT_TEST_LOG"
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    output="$2"
    shift 2
    continue
  fi
  shift
done
if [ -n "$output" ]; then
  mkdir -p "$(dirname "$output")"
  printf 'fake builder\n' >"$output"
fi
exit 0
`)
	writeLoggingFakeCommand(t, binDir, "pnpm")

	cmd := exec.Command(filepath.Join(root, "scripts", "build.sh"), "--output", outPath, "--skip-frontend")
	cmd.Dir = root
	cmd.Env = append(
		sanitizedScriptTestEnv(os.Environ()),
		"SCRIPT_TEST_LOG="+logPath,
		"PATH="+binDir+string(os.PathListSeparator)+mustLookupEnv(t, "PATH"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected build script to pass: %v (%s)", err, output)
	}

	logText := readTextFile(t, logPath)
	if !strings.Contains(logText, "go build ") {
		t.Fatalf("expected go build in command log, got %q", logText)
	}
	if strings.Contains(logText, "pnpm ") {
		t.Fatalf("expected --skip-frontend to skip frontend build, got %q", logText)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected fake output binary: %v", err)
	}
}

func newSlowTestPackage(t *testing.T, root string, sleepDuration string) string {
	t.Helper()
	tempPkg, err := os.MkdirTemp(root, "_script-timeout-test-*")
	if err != nil {
		t.Fatalf("create temp package: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempPkg) })

	if err := os.WriteFile(filepath.Join(tempPkg, "slow_test.go"), []byte(`package slowtest

import (
	"testing"
	"time"
)

func TestSlow(t *testing.T) {
	time.Sleep(`+sleepDuration+`)
}
`), 0o644); err != nil {
		t.Fatalf("write slow test: %v", err)
	}

	relPkg, err := filepath.Rel(root, tempPkg)
	if err != nil {
		t.Fatalf("relative temp package: %v", err)
	}
	return "./" + filepath.ToSlash(relPkg)
}

func writeFakeCommand(t *testing.T, dir string, name string, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake command %s: %v", name, err)
	}
}

func writeLoggingFakeCommand(t *testing.T, dir string, name string) {
	t.Helper()
	writeFakeCommand(t, dir, name, "#!/usr/bin/env bash\nprintf '"+name+" %s\\n' \"$*\" >>\"$SCRIPT_TEST_LOG\"\nexit 0\n")
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func gitHookEnv(t *testing.T, root string) []string {
	t.Helper()
	gitDir := gitOutput(t, root, "rev-parse", "--git-dir")
	gitCommonDir := gitOutput(t, root, "rev-parse", "--git-common-dir")
	return []string{
		"PATH=" + mustLookupEnv(t, "PATH"),
		"HOME=" + mustLookupEnv(t, "HOME"),
		"GIT_DIR=" + gitDir,
		"GIT_WORK_TREE=" + root,
		"GIT_COMMON_DIR=" + gitCommonDir,
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = sanitizedScriptTestEnv(os.Environ())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func mustLookupEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("expected %s in environment", key)
	}
	return value
}

func sanitizedScriptTestEnv(base []string) []string {
	filtered := make([]string, 0, len(base))
	for _, entry := range base {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			key = entry[:idx]
		}
		switch key {
		case "GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_COMMON_DIR",
			"GIT_CONFIG",
			"GIT_CONFIG_COUNT",
			"GIT_CONFIG_PARAMETERS",
			"GIT_DIR",
			"GIT_GLOB_PATHSPECS",
			"GIT_GRAFT_FILE",
			"GIT_ICASE_PATHSPECS",
			"GIT_IMPLICIT_WORK_TREE",
			"GIT_INDEX_FILE",
			"GIT_INTERNAL_SUPER_PREFIX",
			"GIT_LITERAL_PATHSPECS",
			"GIT_NAMESPACE",
			"GIT_NOGLOB_PATHSPECS",
			"GIT_NO_REPLACE_OBJECTS",
			"GIT_OBJECT_DIRECTORY",
			"GIT_PREFIX",
			"GIT_REPLACE_REF_BASE",
			"GIT_SHALLOW_FILE",
			"GIT_WORK_TREE":
			continue
		}
		if strings.HasPrefix(key, "GIT_CONFIG_KEY_") || strings.HasPrefix(key, "GIT_CONFIG_VALUE_") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
