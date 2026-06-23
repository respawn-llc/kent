package core_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenFullTranscriptProjectors materialize an entire events.jsonl history
// into a transcript snapshot. User-visible transcript must be served from
// bounded reverse-read windows (ReadSegmentBackward/ReadRecentEvents/
// ReadEventsBackwardUntil) projected through the engine's cursor page, never a
// full byte-0->EOF scan. These projectors survive only for test assertions.
var forbiddenFullTranscriptProjectors = map[string]struct{}{
	"ChatSnapshot":           {},
	"TranscriptPageSnapshot": {},
}

// walkEventsProductionAllowlist enumerates the production files permitted to
// scan the whole event log via WalkEvents. None of them is a transcript read:
// fork streams the parent log into a child copy (the one read the user exempted
// as "cutoff at offset and copy"), run projection rebuilds run metadata,
// bootstrap recovers freshness/last-sequence on open, and scanPersistedTranscript
// is reachable only through the test-only projectors guarded above.
var walkEventsProductionAllowlist = map[string]struct{}{
	filepath.Join("server", "session", "fork.go"):                       {},
	filepath.Join("server", "session", "runs.go"):                       {},
	filepath.Join("server", "session", "store.go"):                      {},
	filepath.Join("server", "session", "event_log.go"):                  {},
	filepath.Join("server", "runtime", "engine_state.go"):               {},
	filepath.Join("server", "session", "sessiontest", "sessiontest.go"): {},
}

func TestProductionTranscriptReadsStayBounded(t *testing.T) {
	repoRoot := findRepoRoot(t)
	violations := make([]string, 0)
	if err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipMaterializationScanDir(d.Name()) {
				return filepath.SkipDir
			}
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
		_, walkAllowed := walkEventsProductionAllowlist[relPath]
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if _, forbidden := forbiddenFullTranscriptProjectors[selector.Sel.Name]; forbidden {
				violations = append(violations, relPath+": production code must not call full-transcript projector "+selector.Sel.Name+" (serve bounded cursor pages instead)")
			}
			if selector.Sel.Name == "WalkEvents" && !walkAllowed {
				violations = append(violations, relPath+": production code must not scan the full event log via WalkEvents for transcript reads (use ReadSegmentBackward/ReadRecentEvents/ReadEventsBackwardUntil)")
			}
			return true
		})
		return nil
	}); err != nil {
		t.Fatalf("scan repository for unbounded transcript reads: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("unbounded transcript read violations:\n%s", strings.Join(violations, "\n"))
	}
}
