package app

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestNativeScrollbackLedgerOwnsFlushSequencingState(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info os.FileInfo) bool {
		name := info.Name()
		return !info.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse app package: %v", err)
	}
	pkg := pkgs["app"]
	if pkg == nil {
		t.Fatal("app package not found")
	}

	bannedFields := map[string]struct{}{
		"nativeFlushSequence":                {},
		"nativeFlushedSequence":              {},
		"nativePendingFlushes":               {},
		"nativeFlushedEntryCount":            {},
		"nativeHistoryReplayed":              {},
		"nativeProjection":                   {},
		"nativeProjectionBaseOffset":         {},
		"nativeRenderedProjection":           {},
		"nativeRenderedBaseOffset":           {},
		"nativeRenderedSnapshot":             {},
		"nativeRenderedProjectionCommit":     {},
		"nativeStreamingStableFlushSequence": {},
		"nativeStreamingStepID":              {},
		"nativeStreamingCommitStart":         {},
		"nativeStreamingCommitEnd":           {},
		"nativeStreamingCommitRangeSet":      {},
		"nativeStreamingController":          {},
		"nativeStreamingTail":                {},
		"nativeStreamingText":                {},
		"nativeStreamingWidth":               {},
		"nativeStreamingFlushedLineCount":    {},
	}
	for filename, file := range pkg.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if fn, ok := node.(*ast.FuncDecl); ok {
				switch fn.Name.Name {
				case "acceptNativeProjectionWithoutReplay":
					t.Fatalf("%s reintroduced acceptNativeProjectionWithoutReplay; rendered native projection must advance through ledger checkpoints", filename)
				case "newOngoingCommittedDeliveryCursor":
					t.Fatalf("%s reintroduced app-owned ongoing committed delivery cursor constructor; NativeScrollbackLedger owns committed delivery frontiers", filename)
				case "emitForcedNativeProjectionReplay", "replayNativeTranscriptThroughEntry":
					t.Fatalf("%s reintroduced %s; normal-buffer full replay is only allowed through typed startup or continuity-recovery paths", filename, fn.Name.Name)
				case "discardPendingNativeHistoryFlushes":
					t.Fatalf("%s reintroduced discardPendingNativeHistoryFlushes; production app code must not expose standalone pending native write discard", filename)
				}
			}
			if ident, ok := node.(*ast.Ident); ok && ident.Name == "forceFull" && strings.Contains(filename, "ui_native_history") {
				t.Fatalf("%s reintroduced native scrollback forceFull replay flag; full replay must be a typed startup/continuity-recovery path", filename)
			}
			if selector, ok := node.(*ast.SelectorExpr); ok {
				if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "tui" {
					switch selector.Sel.Name {
					case "CommittedOngoingEntries", "PendingOngoingEntries", "PendingToolEntries":
						t.Fatalf("%s calls tui.%s; app native scrollback paths must use nativescrollback partition helpers", filename, selector.Sel.Name)
					}
				}
			}
			typeSpec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			switch typeSpec.Name.Name {
			case "ongoingCommittedDeliveryCursor", "ongoingCommittedRange":
				t.Fatalf("%s reintroduced app-owned %s; NativeScrollbackLedger owns committed delivery frontiers", filename, typeSpec.Name.Name)
			case "nativeStreamingStableFlushAckMsg", "nativeAssistantStreamController":
				t.Fatalf("%s reintroduced %s; terminal write ack and assistant streaming state must live in NativeScrollbackLedger", filename, typeSpec.Name.Name)
			}
			if typeSpec.Name.Name == "uiTranscriptFeatureState" {
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					t.Fatalf("uiTranscriptFeatureState in %s is not a struct", filename)
				}
				for _, field := range structType.Fields.List {
					for _, name := range field.Names {
						switch name.Name {
						case "ongoingCommittedDelivery", "ongoingCommittedDeliveryCursor":
							t.Fatalf("uiTranscriptFeatureState must not store %s; NativeScrollbackLedger owns committed delivery frontiers", name.Name)
						}
					}
				}
				return false
			}
			if typeSpec.Name.Name != "uiNativeHistoryFeatureState" {
				return true
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("uiNativeHistoryFeatureState in %s is not a struct", filename)
			}
			for _, field := range structType.Fields.List {
				for _, name := range field.Names {
					if _, banned := bannedFields[name.Name]; banned {
						t.Fatalf("uiNativeHistoryFeatureState must not store %s; NativeScrollbackLedger owns flush sequencing", name.Name)
					}
				}
				if strings.Contains(renderASTExpr(t, fset, field.Type), "map[uint64]nativeHistoryFlushMsg") {
					t.Fatalf("uiNativeHistoryFeatureState must not store pending native flush messages; NativeScrollbackLedger owns pending writes")
				}
			}
			return false
		})
	}
}

func renderASTExpr(t *testing.T, fset *token.FileSet, expr ast.Expr) string {
	t.Helper()
	var out bytes.Buffer
	if err := printer.Fprint(&out, fset, expr); err != nil {
		t.Fatalf("render AST expr: %v", err)
	}
	return out.String()
}
