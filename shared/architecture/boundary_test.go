package architecture_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type goListPackage struct {
	ImportPath string
	Imports    []string
}

func TestArchitectureBoundaries(t *testing.T) {
	repoRoot := findRepoRoot(t)
	packages := loadRepoPackages(t, repoRoot)
	violations := make([]string, 0)
	for _, pkg := range packages {
		importPath := strings.TrimSpace(pkg.ImportPath)
		if importPath == "" {
			continue
		}
		for _, imported := range pkg.Imports {
			trimmedImport := strings.TrimSpace(imported)
			if trimmedImport == "" || !strings.HasPrefix(trimmedImport, "builder/") {
				continue
			}
			switch {
			case strings.HasPrefix(importPath, "builder/server/") && strings.HasPrefix(trimmedImport, "builder/cli/"):
				violations = append(violations, importPath+" must not import frontend package "+trimmedImport)
			case strings.HasPrefix(importPath, "builder/shared/") && strings.HasPrefix(trimmedImport, "builder/cli/"):
				violations = append(violations, importPath+" must not import frontend package "+trimmedImport)
			case strings.HasPrefix(importPath, "builder/shared/") && strings.HasPrefix(trimmedImport, "builder/server/"):
				violations = append(violations, importPath+" must not import server package "+trimmedImport)
			case strings.HasPrefix(importPath, "builder/cli/") && trimmedImport == "builder/server/metadata":
				violations = append(violations, importPath+" must not import persistence metadata package "+trimmedImport)
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("architecture boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIPackagesDoNotImportServerOutsideCompositionBridges(t *testing.T) {
	repoRoot := findRepoRoot(t)
	// Keep CLI -> server imports concentrated in documented local composition
	// bridges. UI, TUI, status, and command handlers must use shared contracts.
	allowedServerImportsByFile := map[string]map[string]struct{}{
		filepath.Join("cli", "app", "internal", "serverbridge", "serverbridge.go"): {
			"builder/server/auth":       {},
			"builder/server/authflow":   {},
			"builder/server/bootstrap":  {},
			"builder/server/embedded":   {},
			"builder/server/generated":  {},
			"builder/server/llm":        {},
			"builder/server/onboarding": {},
			"builder/server/runtime":    {},
			"builder/server/serve":      {},
			"builder/server/startup":    {},
		},
		filepath.Join("cli", "builder", "internal", "serverbridge", "serverbridge.go"): {
			"builder/server/serve":            {},
			"builder/server/sessionlifecycle": {},
			"builder/server/startup":          {},
		},
	}
	actualAllowedServerImportsByFile := make(map[string]map[string]struct{})
	violations := make([]string, 0)
	walkRoot := filepath.Join(repoRoot, "cli")
	if err := filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			if !strings.HasPrefix(importPath, "builder/server/") {
				continue
			}
			allowedImports := allowedServerImportsByFile[relPath]
			if _, allowed := allowedImports[importPath]; allowed {
				if actualAllowedServerImportsByFile[relPath] == nil {
					actualAllowedServerImportsByFile[relPath] = make(map[string]struct{})
				}
				actualAllowedServerImportsByFile[relPath][importPath] = struct{}{}
				continue
			}
			violations = append(violations, relPath+": CLI production file must not import server package "+importPath)
		}
		return nil
	}); err != nil {
		t.Fatalf("scan cli server imports: %v", err)
	}
	for relPath, expectedImports := range allowedServerImportsByFile {
		actualImports := actualAllowedServerImportsByFile[relPath]
		for importPath := range expectedImports {
			if _, found := actualImports[importPath]; !found {
				violations = append(violations, relPath+": remove stale allowed server import "+importPath+" from architecture test")
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli to server import boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppUIFilesDoNotAddServerImports(t *testing.T) {
	repoRoot := findRepoRoot(t)
	allowedServerImportsByFile := map[string]map[string]struct{}{}
	actualServerImportsByFile := make(map[string]map[string]struct{})
	violations := make([]string, 0)
	walkRoot := filepath.Join(repoRoot, "cli", "app")
	if err := filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != walkRoot {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "ui") || !strings.HasSuffix(base, ".go") || strings.HasSuffix(base, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			if !strings.HasPrefix(importPath, "builder/server/") {
				continue
			}
			if actualServerImportsByFile[relPath] == nil {
				actualServerImportsByFile[relPath] = make(map[string]struct{})
			}
			actualServerImportsByFile[relPath][importPath] = struct{}{}
			if _, allowed := allowedServerImportsByFile[relPath][importPath]; !allowed {
				violations = append(violations, relPath+": UI file must not add server import "+importPath)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("scan cli app UI sources: %v", err)
	}
	for relPath, expectedImports := range allowedServerImportsByFile {
		actualImports := actualServerImportsByFile[relPath]
		for importPath := range expectedImports {
			if _, found := actualImports[importPath]; !found {
				violations = append(violations, relPath+": remove stale allowed server import "+importPath+" from architecture test")
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli app UI server import boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIUIFilesDoNotBypassServerAttachService(t *testing.T) {
	repoRoot := findRepoRoot(t)
	violations := make([]string, 0)
	for _, root := range []string{
		filepath.Join(repoRoot, "cli", "app"),
		filepath.Join(repoRoot, "cli", "tui"),
	} {
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != root && root == filepath.Join(repoRoot, "cli", "app") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			base := filepath.Base(path)
			isAppUI := root == filepath.Join(repoRoot, "cli", "app") && strings.HasPrefix(base, "ui")
			isTUI := strings.Contains(filepath.ToSlash(path), "/cli/tui/")
			if !isAppUI && !isTUI {
				return nil
			}
			fileSet := token.NewFileSet()
			file, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
			if parseErr != nil {
				return parseErr
			}
			relPath, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				relPath = path
			}
			for _, spec := range file.Imports {
				importPath := strings.Trim(spec.Path.Value, "\"")
				if importPath == "builder/cli/app/internal/serverattach" || importPath == "builder/cli/app/internal/remoteattach" {
					violations = append(violations, relPath+": UI files must not import startup attachment package "+importPath)
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("scan UI sources under %s: %v", root, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli UI server attach bypass violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppRootFilesDoNotImportServerPackages(t *testing.T) {
	repoRoot := findRepoRoot(t)
	violations := make([]string, 0)
	walkCLIAppRootFiles(t, repoRoot, false, parser.ImportsOnly, func(source parsedGoSource) {
		for _, spec := range source.File.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			if strings.HasPrefix(importPath, "builder/server/") {
				violations = append(violations, source.RelPath+": app root package must not import server package "+importPath)
			}
		}
	})
	if len(violations) > 0 {
		t.Fatalf("cli app root server import boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppDoesNotReintroduceEmbeddedServerServiceLocator(t *testing.T) {
	repoRoot := findRepoRoot(t)
	violations := make([]string, 0)
	walkCLIAppRootFiles(t, repoRoot, true, parser.SkipObjectResolution, func(source parsedGoSource) {
		ast.Inspect(source.File, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "embeddedServer" {
				violations = append(violations, source.RelPath+": must not reintroduce embeddedServer service-locator identifier")
			}
			return true
		})
	})
	if len(violations) > 0 {
		t.Fatalf("cli app embeddedServer service-locator violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppStartupEntrypointsUseServerAttach(t *testing.T) {
	repoRoot := findRepoRoot(t)
	for _, relPath := range []string{
		filepath.Join("cli", "app", "session_server_target.go"),
		filepath.Join("cli", "app", "run_prompt_target.go"),
	} {
		path := filepath.Join(repoRoot, relPath)
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", relPath, err)
		}
		importsServerAttach := false
		violations := make([]string, 0)
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch importPath {
			case "builder/cli/app/internal/serverattach":
				importsServerAttach = true
			case "builder/cli/app/internal/targetstartup", "builder/cli/app/internal/targetresolve":
				violations = append(violations, relPath+": startup entrypoint must use serverattach instead of "+importPath)
			}
		}
		if !importsServerAttach {
			violations = append(violations, relPath+": startup entrypoint must import serverattach")
		}
		usesResolve := false
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name == "serverattach" && selector.Sel.Name == "Resolve" {
				usesResolve = true
			}
			if ident.Name == "remoteattach" && (selector.Sel.Name == "DialHeadless" || selector.Sel.Name == "DialInteractive") {
				violations = append(violations, relPath+": startup entrypoint must not call remoteattach."+selector.Sel.Name+" directly")
			}
			return true
		})
		if !usesResolve {
			violations = append(violations, relPath+": startup entrypoint must resolve targets through serverattach.Resolve")
		}
		if len(violations) > 0 {
			t.Fatalf("startup server attach boundary violations:\n%s", strings.Join(violations, "\n"))
		}
	}
}

func TestCLIAppStartupFilesDoNotReachIntoEmbeddedInternals(t *testing.T) {
	repoRoot := findRepoRoot(t)
	for _, relPath := range []string{
		filepath.Join("cli", "app", "session_server_target.go"),
		filepath.Join("cli", "app", "run_prompt_target.go"),
		filepath.Join("cli", "app", "session_lifecycle.go"),
	} {
		path := filepath.Join(repoRoot, relPath)
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", relPath, err)
		}
		violations := make([]string, 0)
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "inner" {
				violations = append(violations, relPath+": startup files must use narrow embedded attachments instead of embeddedAppServer.inner")
			}
			return true
		})
		if len(violations) > 0 {
			t.Fatalf("embedded startup boundary violations:\n%s", strings.Join(violations, "\n"))
		}
	}
}

type parsedGoSource struct {
	RelPath string
	File    *ast.File
}

func walkCLIAppRootFiles(t *testing.T, repoRoot string, includeTests bool, mode parser.Mode, visit func(parsedGoSource)) {
	t.Helper()
	root := filepath.Join(repoRoot, "cli", "app")
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || (!includeTests && strings.HasSuffix(path, "_test.go")) {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, mode)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		visit(parsedGoSource{RelPath: relPath, File: file})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app root sources: %v", err)
	}
}

func TestCLIAppSplitFilesDoNotImportServerPackages(t *testing.T) {
	repoRoot := findRepoRoot(t)
	files := []string{
		filepath.Join("cli", "app", "auth_oauth_presenter.go"),
		filepath.Join("cli", "app", "onboarding_render.go"),
		filepath.Join("cli", "app", "onboarding_workflow.go"),
		filepath.Join("cli", "app", "runtime_attachment.go"),
		filepath.Join("cli", "app", "ui_native_history_projection.go"),
		filepath.Join("cli", "app", "ui_process_render.go"),
		filepath.Join("cli", "app", "ui_runtime_client_control.go"),
		filepath.Join("cli", "app", "ui_status_overlay_format.go"),
	}
	violations := make([]string, 0)
	for _, relPath := range files {
		path := filepath.Join(repoRoot, relPath)
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse %s imports: %v", relPath, parseErr)
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			if strings.HasPrefix(importPath, "builder/server/") {
				violations = append(violations, relPath+": split app file must not import server package "+importPath)
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli app split-file server import boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLITUIFilesDoNotImportServerPackages(t *testing.T) {
	repoRoot := findRepoRoot(t)
	allowedServerImportsByFile := map[string]map[string]struct{}{}
	actualServerImportsByFile := make(map[string]map[string]struct{})
	tuiRoot := filepath.Join(repoRoot, "cli", "tui")
	violations := make([]string, 0)
	if err := filepath.WalkDir(tuiRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			if !strings.HasPrefix(importPath, "builder/server/") {
				continue
			}
			if actualServerImportsByFile[relPath] == nil {
				actualServerImportsByFile[relPath] = make(map[string]struct{})
			}
			actualServerImportsByFile[relPath][importPath] = struct{}{}
			if _, allowed := allowedServerImportsByFile[relPath][importPath]; !allowed {
				violations = append(violations, relPath+": TUI package must not add server import "+importPath)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("scan cli tui sources: %v", err)
	}
	for relPath, expectedImports := range allowedServerImportsByFile {
		actualImports := actualServerImportsByFile[relPath]
		for importPath := range expectedImports {
			if _, found := actualImports[importPath]; !found {
				violations = append(violations, relPath+": remove stale allowed server import "+importPath+" from architecture test")
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli tui server import boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalStatusBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	statusRoot := filepath.Join(repoRoot, "cli", "app", "internal", "status")
	violations := make([]string, 0)
	if err := filepath.WalkDir(statusRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": status package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": status package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": status package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": status package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": status package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal status sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal status boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalStatusCollectBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	statusRoot := filepath.Join(repoRoot, "cli", "app", "internal", "statuscollect")
	violations := make([]string, 0)
	if err := filepath.WalkDir(statusRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": status collector package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": status collector package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": status collector package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": status collector package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal status collector sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal status collector boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalRuntimeConnectionBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	violations := make([]string, 0)
	for _, packageName := range []string{"runtimeconn", "runtimeattach"} {
		root := filepath.Join(repoRoot, "cli", "app", "internal", packageName)
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fileSet := token.NewFileSet()
			file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			relPath, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				relPath = path
			}
			for _, spec := range file.Imports {
				importPath := strings.Trim(spec.Path.Value, "\"")
				switch {
				case packageName == "runtimeattach" && strings.HasPrefix(importPath, "builder/server/"):
					violations = append(violations, relPath+": runtime connection package must not import server package "+importPath)
				case importPath == "github.com/charmbracelet/bubbletea":
					violations = append(violations, relPath+": runtime connection package must not import Bubble Tea")
				case importPath == "builder/cli/app/commands":
					violations = append(violations, relPath+": runtime connection package must not import app commands")
				case importPath == "builder/cli/app":
					violations = append(violations, relPath+": runtime connection package must not import app package")
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if ok && ident.Name == "uiModel" {
					violations = append(violations, relPath+": runtime connection package must not reference uiModel")
				}
				return true
			})
			return nil
		}); err != nil {
			t.Fatalf("scan cli app internal %s sources: %v", packageName, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal runtime connection boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalSubmissionErrorBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "submissionerror")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": submission error package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": submission error package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": submission error package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": submission error package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal submission error sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal submission error boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalProcessViewBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "processview")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": process view package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": process view package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": process view package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": process view package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": process view package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal process view sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal process view boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalAuthCommandBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "authcommand")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": auth command package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": auth command package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": auth command package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": auth command package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal auth command sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal auth command boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalAuthViewBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "authview")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": auth view package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": auth view package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": auth view package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": auth view package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal auth view sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal auth view boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalAuthFlowAdapterBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "authflowadapter")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": auth flow adapter package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": auth flow adapter package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": auth flow adapter package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": auth flow adapter package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal auth flow adapter sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal auth flow adapter boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalAuthInteractionBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "authinteraction")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": auth interaction package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": auth interaction package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": auth interaction package must not import app package")
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": auth interaction package must use authflowadapter instead of importing server package "+importPath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": auth interaction package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal auth interaction sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal auth interaction boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalAuthOAuthBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "authoauth")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": auth OAuth package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": auth OAuth package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": auth OAuth package must not import app package")
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": auth OAuth package must use oauthadapter instead of importing server package "+importPath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": auth OAuth package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal auth OAuth sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal auth OAuth boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalOAuthAdapterBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "oauthadapter")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": OAuth adapter package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": OAuth adapter package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": OAuth adapter package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": OAuth adapter package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal OAuth adapter sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal OAuth adapter boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalTargetResolveBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "targetresolve")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": target resolver package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": target resolver package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": target resolver package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": target resolver package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": target resolver package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal target resolver sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal target resolver boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalTargetStartupBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "targetstartup")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": target startup package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": target startup package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": target startup package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": target startup package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": target startup package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal target startup sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal target startup boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalServerAttachBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "serverattach")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": server attach package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": server attach package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": server attach package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": server attach package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": server attach package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal server attach sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal server attach boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalDaemonLaunchBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "daemonlaunch")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/"):
				violations = append(violations, relPath+": daemon launch package must not import Builder packages "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": daemon launch package must not import Bubble Tea")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": daemon launch package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal daemon launch sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal daemon launch boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalRemoteAttachBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	violations := make([]string, 0)
	for _, packageName := range []string{"remoteattach", "remotebinding"} {
		root := filepath.Join(repoRoot, "cli", "app", "internal", packageName)
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fileSet := token.NewFileSet()
			file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			relPath, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				relPath = path
			}
			for _, spec := range file.Imports {
				importPath := strings.Trim(spec.Path.Value, "\"")
				switch {
				case strings.HasPrefix(importPath, "builder/server/"):
					violations = append(violations, relPath+": remote attach package must not import server package "+importPath)
				case importPath == "github.com/charmbracelet/bubbletea":
					violations = append(violations, relPath+": remote attach package must not import Bubble Tea")
				case importPath == "builder/cli/app/commands":
					violations = append(violations, relPath+": remote attach package must not import app commands")
				case importPath == "builder/cli/app":
					violations = append(violations, relPath+": remote attach package must not import app package")
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if ok && ident.Name == "uiModel" {
					violations = append(violations, relPath+": remote attach package must not reference uiModel")
				}
				return true
			})
			return nil
		}); err != nil {
			t.Fatalf("scan cli app internal %s sources: %v", packageName, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal remote attach boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalSessionTargetBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "sessiontarget")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": session target package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": session target package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": session target package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": session target package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": session target package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal session target sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal session target boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalRunPromptTargetBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "runprompttarget")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": run prompt target package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": run prompt target package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": run prompt target package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": run prompt target package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": run prompt target package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal run prompt target sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal run prompt target boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalProjectBindingBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "projectbinding")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": project binding package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": project binding package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": project binding package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": project binding package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": project binding package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal project binding sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal project binding boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalProjectPickerBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "projectpicker")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": project picker package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": project picker package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": project picker package must not import app package")
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": project picker package must not import server package "+importPath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": project picker package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal project picker sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal project picker boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalWorktreeViewBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "worktreeview")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": worktree view package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": worktree view package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": worktree view package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": worktree view package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": worktree view package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal worktree view sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal worktree view boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalWorktreeMutationBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "worktreemutation")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": worktree mutation package must not import server package "+importPath)
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": worktree mutation package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": worktree mutation package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": worktree mutation package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": worktree mutation package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal worktree mutation sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal worktree mutation boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalWorktreeUXBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	violations := make([]string, 0)
	for _, packageName := range []string{"worktreecreate", "worktreecreateform", "worktreecreateresolve", "worktreeselection", "worktreedelete", "worktreeviewport"} {
		root := filepath.Join(repoRoot, "cli", "app", "internal", packageName)
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fileSet := token.NewFileSet()
			file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			relPath, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				relPath = path
			}
			for _, spec := range file.Imports {
				importPath := strings.Trim(spec.Path.Value, "\"")
				switch {
				case strings.HasPrefix(importPath, "builder/server/"):
					violations = append(violations, relPath+": worktree UX package must not import server package "+importPath)
				case importPath == "github.com/charmbracelet/bubbletea":
					violations = append(violations, relPath+": worktree UX package must not import Bubble Tea")
				case importPath == "builder/cli/app/commands":
					violations = append(violations, relPath+": worktree UX package must not import app commands")
				case importPath == "builder/cli/app":
					violations = append(violations, relPath+": worktree UX package must not import app package")
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if ok && ident.Name == "uiModel" {
					violations = append(violations, relPath+": worktree UX package must not reference uiModel")
				}
				return true
			})
			return nil
		}); err != nil {
			t.Fatalf("scan cli app internal %s sources: %v", packageName, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal worktree UX boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalStartupConfigBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "startupconfig")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": startup config package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": startup config package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": startup config package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": startup config package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal startup config sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal startup config boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalServeCommandBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "servecommand")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": serve command package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": serve command package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": serve command package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": serve command package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal serve command sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal serve command boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalOnboardingImportBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "onboardingimport")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": onboarding import package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": onboarding import package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": onboarding import package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": onboarding import package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal onboarding import sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal onboarding import boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalOnboardingImportChoiceBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "onboardingimportchoice")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": onboarding import choice package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": onboarding import choice package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": onboarding import choice package must not import app package")
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": onboarding import choice package must not import server package "+importPath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": onboarding import choice package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal onboarding import choice sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal onboarding import choice boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalOnboardingImportSkillsBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "onboardingimportskills")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": onboarding import skills package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": onboarding import skills package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": onboarding import skills package must not import app package")
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": onboarding import skills package must not import server package "+importPath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": onboarding import skills package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal onboarding import skills sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal onboarding import skills boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalOnboardingImportFSBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "onboardingimportfs")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": onboarding import filesystem package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": onboarding import filesystem package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": onboarding import filesystem package must not import app package")
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": onboarding import filesystem package must use onboardingimport adapter instead of importing server package "+importPath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": onboarding import filesystem package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal onboarding import filesystem sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal onboarding import filesystem boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalOnboardingImportProvidersBoundary(t *testing.T) {
	assertCLIAppInternalOnboardingImportPackageBoundary(t, "onboardingimportproviders", "onboarding import providers package")
}

func TestCLIAppInternalOnboardingImportGeneratedBoundary(t *testing.T) {
	assertCLIAppInternalOnboardingImportPackageBoundary(t, "onboardingimportgenerated", "onboarding import generated-skills package")
}

func assertCLIAppInternalOnboardingImportPackageBoundary(t *testing.T, packageName string, label string) {
	t.Helper()
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", packageName)
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": "+label+" must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": "+label+" must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": "+label+" must not import app package")
			case strings.HasPrefix(importPath, "builder/server/"):
				violations = append(violations, relPath+": "+label+" must not import server package "+importPath)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": "+label+" must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal %s sources: %v", packageName, err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal %s boundary violations:\n%s", packageName, strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalOnboardingModelBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "onboardingmodel")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": onboarding model package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": onboarding model package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": onboarding model package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": onboarding model package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal onboarding model sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal onboarding model boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalOnboardingReadyBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	root := filepath.Join(repoRoot, "cli", "app", "internal", "onboardingready")
	violations := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch {
			case importPath == "github.com/charmbracelet/bubbletea":
				violations = append(violations, relPath+": onboarding ready package must not import Bubble Tea")
			case importPath == "builder/cli/app/commands":
				violations = append(violations, relPath+": onboarding ready package must not import app commands")
			case importPath == "builder/cli/app":
				violations = append(violations, relPath+": onboarding ready package must not import app package")
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "uiModel" {
				violations = append(violations, relPath+": onboarding ready package must not reference uiModel")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli app internal onboarding ready sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal onboarding ready boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIAppInternalEmbeddedStartupBoundary(t *testing.T) {
	repoRoot := findRepoRoot(t)
	violations := make([]string, 0)
	for _, packageName := range []string{"embeddedstartup", "embeddedbinding"} {
		root := filepath.Join(repoRoot, "cli", "app", "internal", packageName)
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fileSet := token.NewFileSet()
			file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			relPath, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				relPath = path
			}
			for _, spec := range file.Imports {
				importPath := strings.Trim(spec.Path.Value, "\"")
				switch {
				case importPath == "github.com/charmbracelet/bubbletea":
					violations = append(violations, relPath+": embedded startup package must not import Bubble Tea")
				case importPath == "builder/cli/app/commands":
					violations = append(violations, relPath+": embedded startup package must not import app commands")
				case importPath == "builder/cli/app":
					violations = append(violations, relPath+": embedded startup package must not import app package")
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if ok && ident.Name == "uiModel" {
					violations = append(violations, relPath+": embedded startup package must not reference uiModel")
				}
				return true
			})
			return nil
		}); err != nil {
			t.Fatalf("scan cli app internal %s sources: %v", packageName, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal embedded startup boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCLIDoesNotCallPersistenceStorageAPIsDirectly(t *testing.T) {
	repoRoot := findRepoRoot(t)
	forbiddenCalls := map[string]map[string]struct{}{
		"builder/server/metadata": {
			"Open":                     {},
			"ResolveBinding":           {},
			"RegisterBinding":          {},
			"EnsureWorkspaceBinding":   {},
			"ResolveWorkspacePath":     {},
			"AttachWorkspaceToProject": {},
			"RebindWorkspace":          {},
		},
		"builder/server/session": {
			"Open":         {},
			"OpenByID":     {},
			"Create":       {},
			"NewLazy":      {},
			"ListSessions": {},
		},
	}
	violations := make([]string, 0)
	walkRoot := filepath.Join(repoRoot, "cli")
	if err := filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		importAliases := make(map[string]string)
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			alias := ""
			if spec.Name != nil {
				alias = strings.TrimSpace(spec.Name.Name)
			} else {
				alias = filepath.Base(importPath)
			}
			importAliases[alias] = importPath
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			importPath, ok := importAliases[ident.Name]
			if !ok {
				return true
			}
			forbiddenSelectors, ok := forbiddenCalls[importPath]
			if !ok {
				return true
			}
			if _, forbidden := forbiddenSelectors[selector.Sel.Name]; forbidden {
				relPath, relErr := filepath.Rel(repoRoot, path)
				if relErr != nil {
					relPath = path
				}
				violations = append(violations, relPath+": frontend must not call "+importPath+"."+selector.Sel.Name)
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan cli sources: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("cli persistence boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func loadRepoPackages(t *testing.T, repoRoot string) []goListPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = repoRoot
	cmd.Env = filteredGoListEnv()
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list packages: %v", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	packages := make([]goListPackage, 0)
	for decoder.More() {
		var pkg goListPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("decode go list package json: %v", err)
		}
		packages = append(packages, pkg)
	}
	return packages
}

func filteredGoListEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "ENV=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}
