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

func TestSharedClientUIRemainsDTOOnly(t *testing.T) {
	repoRoot := findRepoRoot(t)
	clientUIRoot := filepath.Join(repoRoot, "shared", "clientui")
	allowedTypes := map[string]struct{}{
		"ApprovalDecision":                 {},
		"ApprovalOption":                   {},
		"ApprovalPromptAnswer":             {},
		"BackgroundProcess":                {},
		"BackgroundShellEvent":             {},
		"ChatEntry":                        {},
		"ChatSnapshot":                     {},
		"CommittedTranscriptSuffix":        {},
		"CommittedTranscriptSuffixRequest": {},
		"CompactionLifecycle":              {},
		"CompactionStatus":                 {},
		"ConversationFreshness":            {},
		"EntryVisibility":                  {},
		"Event":                            {},
		"EventKind":                        {},
		"MessagePhase":                     {},
		"MessageType":                      {},
		"PendingApproval":                  {},
		"PendingAsk":                       {},
		"PendingPromptEvent":               {},
		"PendingPromptEventType":           {},
		"ProcessClient":                    {},
		"ProcessOutputChunk":               {},
		"ProjectAvailability":              {},
		"ProjectOverview":                  {},
		"ProjectSummary":                   {},
		"ProjectWorkspaceSummary":          {},
		"PromptAnswer":                     {},
		"QueuedUserMessage":                {},
		"ReasoningDelta":                   {},
		"ReviewerLifecycle":                {},
		"RunLifecycle":                     {},
		"RunLifecyclePhase":                {},
		"RunMode":                          {},
		"RunState":                         {},
		"RunStatus":                        {},
		"RunView":                          {},
		"RuntimeClient":                    {},
		"RuntimeConnectionLifecycle":       {},
		"RuntimeContextUsage":              {},
		"RuntimeGoal":                      {},
		"RuntimeGoalStatus":                {},
		"RuntimeMainView":                  {},
		"RuntimeSessionView":               {},
		"RuntimeStatus":                    {},
		"SessionExecutionTarget":           {},
		"SessionSummary":                   {},
		"ToolCallMeta":                     {},
		"ToolCallRenderBehavior":           {},
		"ToolPresentationKind":             {},
		"ToolRenderHint":                   {},
		"ToolRenderKind":                   {},
		"ToolShellDialect":                 {},
		"TranscriptMetadata":               {},
		"TranscriptPage":                   {},
		"TranscriptPageRequest":            {},
		"TranscriptRecoveryCause":          {},
		"TranscriptWindow":                 {},
		"UpdateStatus":                     {},
	}
	allowedFuncs := map[string]struct{}{
		"CompactionLifecycle.IsRunning":             {},
		"ConversationFreshness.IsFresh":             {},
		"FinishedRunLifecycle":                      {},
		"IdleRunLifecycle":                          {},
		"MustRunLifecycle":                          {},
		"NewCompactionLifecycle":                    {},
		"NewReviewerLifecycle":                      {},
		"NewRunLifecycle":                           {},
		"NewRuntimeConnectionLifecycle":             {},
		"NormalizeCommittedTranscriptSuffixRequest": {},
		"NormalizeMessagePhase":                     {},
		"NormalizeSessionExecutionTarget":           {},
		"NormalizeThinkingLevel":                    {},
		"PendingPromptEvent.IsZero":                 {},
		"ReviewerLifecycle.IsBlocking":              {},
		"ReviewerLifecycle.IsRunning":               {},
		"ReviewerLifecycle.Validate":                {},
		"RunLifecycle.IsFinished":                   {},
		"RunLifecycle.IsGoalLoopRunning":            {},
		"RunLifecycle.IsRunning":                    {},
		"RunLifecycle.Validate":                     {},
		"RunningRunLifecycle":                       {},
		"RuntimeConnectionLifecycle.IsDisconnected": {},
		"SessionExecutionTargetIsZero":              {},
		"SessionExecutionTargetsEqual":              {},
	}
	violations := make([]string, 0)
	if err := filepath.WalkDir(clientUIRoot, func(path string, d os.DirEntry, err error) error {
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
		for _, decl := range file.Decls {
			switch typedDecl := decl.(type) {
			case *ast.FuncDecl:
				name := funcDeclBoundaryName(typedDecl)
				if _, allowed := allowedFuncs[name]; allowed {
					continue
				}
				if funcUsesRuntimeEventPolicyType(typedDecl.Type) {
					violations = append(violations, relPath+": DTO-only package must not define runtime-event policy helper "+name)
					continue
				}
				violations = append(violations, relPath+": DTO-only package added function "+name+" without DTO-boundary review")
			case *ast.GenDecl:
				if typedDecl.Tok != token.TYPE {
					continue
				}
				for _, spec := range typedDecl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					name := typeSpec.Name.Name
					if _, allowed := allowedTypes[name]; !allowed {
						violations = append(violations, relPath+": DTO-only package added type "+name+" without DTO-boundary review")
					}
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("scan shared clientui DTO boundaries: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("shared/clientui DTO boundary violations:\n%s", strings.Join(violations, "\n"))
	}
}

func funcDeclBoundaryName(fn *ast.FuncDecl) string {
	if fn == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
		if fn == nil {
			return ""
		}
		return fn.Name.Name
	}
	return receiverTypeName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func receiverTypeName(expr ast.Expr) string {
	switch typedExpr := expr.(type) {
	case *ast.Ident:
		return typedExpr.Name
	case *ast.StarExpr:
		return receiverTypeName(typedExpr.X)
	default:
		return ""
	}
}

func funcUsesRuntimeEventPolicyType(funcType *ast.FuncType) bool {
	if funcType == nil {
		return false
	}
	for _, fields := range []*ast.FieldList{funcType.Params, funcType.Results} {
		if fields == nil {
			continue
		}
		for _, field := range fields.List {
			if exprUsesRuntimeEventPolicyType(field.Type) {
				return true
			}
		}
	}
	return false
}

func exprUsesRuntimeEventPolicyType(expr ast.Expr) bool {
	switch typedExpr := expr.(type) {
	case *ast.Ident:
		switch typedExpr.Name {
		case "Event", "ReasoningDelta", "BackgroundShellEvent":
			return true
		default:
			return false
		}
	case *ast.StarExpr:
		return exprUsesRuntimeEventPolicyType(typedExpr.X)
	case *ast.ArrayType:
		return exprUsesRuntimeEventPolicyType(typedExpr.Elt)
	case *ast.MapType:
		return exprUsesRuntimeEventPolicyType(typedExpr.Key) || exprUsesRuntimeEventPolicyType(typedExpr.Value)
	default:
		return false
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

type cliInternalBoundaryCase struct {
	Name                 string
	Packages             []string
	Label                string
	ForbidServer         bool
	ForbidAllBuilder     bool
	ServerViolationLabel string
}

func TestCLIAppInternalPackageBoundaries(t *testing.T) {
	cases := []cliInternalBoundaryCase{
		{Name: "Status", Packages: []string{"status"}, Label: "status package", ForbidServer: true},
		{Name: "StatusCollect", Packages: []string{"statuscollect"}, Label: "status collector package"},
		{Name: "RuntimeConnection", Packages: []string{"runtimeconn"}, Label: "runtime connection package"},
		{Name: "RuntimeAttach", Packages: []string{"runtimeattach"}, Label: "runtime connection package", ForbidServer: true},
		{Name: "SubmissionError", Packages: []string{"submissionerror"}, Label: "submission error package"},
		{Name: "ProcessView", Packages: []string{"processview"}, Label: "process view package", ForbidServer: true},
		{Name: "AuthCommand", Packages: []string{"authcommand"}, Label: "auth command package"},
		{Name: "AuthView", Packages: []string{"authview"}, Label: "auth view package"},
		{Name: "AuthFlowAdapter", Packages: []string{"authflowadapter"}, Label: "auth flow adapter package"},
		{Name: "AuthInteraction", Packages: []string{"authinteraction"}, Label: "auth interaction package", ForbidServer: true, ServerViolationLabel: "auth interaction package must use authflowadapter instead of importing server package"},
		{Name: "AuthOAuth", Packages: []string{"authoauth"}, Label: "auth OAuth package", ForbidServer: true, ServerViolationLabel: "auth OAuth package must use oauthadapter instead of importing server package"},
		{Name: "OAuthAdapter", Packages: []string{"oauthadapter"}, Label: "OAuth adapter package"},
		{Name: "TargetResolve", Packages: []string{"targetresolve"}, Label: "target resolver package", ForbidServer: true},
		{Name: "TargetStartup", Packages: []string{"targetstartup"}, Label: "target startup package", ForbidServer: true},
		{Name: "ServerAttach", Packages: []string{"serverattach"}, Label: "server attach package", ForbidServer: true},
		{Name: "DaemonLaunch", Packages: []string{"daemonlaunch"}, Label: "daemon launch package", ForbidAllBuilder: true},
		{Name: "RemoteAttach", Packages: []string{"remoteattach", "remotebinding"}, Label: "remote attach package", ForbidServer: true},
		{Name: "SessionTarget", Packages: []string{"sessiontarget"}, Label: "session target package", ForbidServer: true},
		{Name: "RunPromptTarget", Packages: []string{"runprompttarget"}, Label: "run prompt target package", ForbidServer: true},
		{Name: "ProjectBinding", Packages: []string{"projectbinding"}, Label: "project binding package", ForbidServer: true},
		{Name: "ProjectPicker", Packages: []string{"projectpicker"}, Label: "project picker package", ForbidServer: true},
		{Name: "WorktreeView", Packages: []string{"worktreeview"}, Label: "worktree view package", ForbidServer: true},
		{Name: "WorktreeMutation", Packages: []string{"worktreemutation"}, Label: "worktree mutation package", ForbidServer: true},
		{Name: "WorktreeUX", Packages: []string{"worktreecreate", "worktreecreateform", "worktreecreateresolve", "worktreeselection", "worktreedelete", "worktreeviewport"}, Label: "worktree UX package", ForbidServer: true},
		{Name: "StartupConfig", Packages: []string{"startupconfig"}, Label: "startup config package"},
		{Name: "ServeCommand", Packages: []string{"servecommand"}, Label: "serve command package"},
		{Name: "OnboardingImport", Packages: []string{"onboardingimport"}, Label: "onboarding import package"},
		{Name: "OnboardingImportChoice", Packages: []string{"onboardingimportchoice"}, Label: "onboarding import choice package", ForbidServer: true},
		{Name: "OnboardingImportSkills", Packages: []string{"onboardingimportskills"}, Label: "onboarding import skills package", ForbidServer: true},
		{Name: "OnboardingImportFS", Packages: []string{"onboardingimportfs"}, Label: "onboarding import filesystem package", ForbidServer: true, ServerViolationLabel: "onboarding import filesystem package must use onboardingimport adapter instead of importing server package"},
		{Name: "OnboardingImportProviders", Packages: []string{"onboardingimportproviders"}, Label: "onboarding import providers package", ForbidServer: true},
		{Name: "OnboardingImportGenerated", Packages: []string{"onboardingimportgenerated"}, Label: "onboarding import generated-skills package", ForbidServer: true},
		{Name: "OnboardingModel", Packages: []string{"onboardingmodel"}, Label: "onboarding model package"},
		{Name: "OnboardingReady", Packages: []string{"onboardingready"}, Label: "onboarding ready package"},
		{Name: "EmbeddedStartup", Packages: []string{"embeddedstartup", "embeddedbinding"}, Label: "embedded startup package"},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			assertCLIAppInternalPackageBoundary(t, tc)
		})
	}
}

func assertCLIAppInternalPackageBoundary(t *testing.T, tc cliInternalBoundaryCase) {
	t.Helper()
	repoRoot := findRepoRoot(t)
	violations := make([]string, 0)
	for _, packageName := range tc.Packages {
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
				case tc.ForbidAllBuilder && strings.HasPrefix(importPath, "builder/"):
					violations = append(violations, relPath+": "+tc.Label+" must not import Builder packages "+importPath)
				case tc.ForbidServer && strings.HasPrefix(importPath, "builder/server/"):
					message := tc.ServerViolationLabel
					if message == "" {
						message = tc.Label + " must not import server package"
					}
					violations = append(violations, relPath+": "+message+" "+importPath)
				case importPath == "github.com/charmbracelet/bubbletea":
					violations = append(violations, relPath+": "+tc.Label+" must not import Bubble Tea")
				case importPath == "builder/cli/app/commands":
					violations = append(violations, relPath+": "+tc.Label+" must not import app commands")
				case importPath == "builder/cli/app":
					violations = append(violations, relPath+": "+tc.Label+" must not import app package")
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if ok && ident.Name == "uiModel" {
					violations = append(violations, relPath+": "+tc.Label+" must not reference uiModel")
				}
				return true
			})
			return nil
		}); err != nil {
			t.Fatalf("scan cli app internal %s sources: %v", packageName, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("cli app internal %s boundary violations:\n%s", tc.Name, strings.Join(violations, "\n"))
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
