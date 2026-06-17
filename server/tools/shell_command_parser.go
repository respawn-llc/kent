package tools

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func ParseSimpleShellCommand(command string) ([]string, bool) {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil || file == nil || len(file.Stmts) != 1 {
		return nil, false
	}

	stmt := file.Stmts[0]
	if stmt == nil || stmt.Cmd == nil || stmt.Negated || stmt.Background || stmt.Coprocess || len(stmt.Redirs) > 0 {
		return nil, false
	}

	callExpr, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(callExpr.Assigns) > 0 || len(callExpr.Args) == 0 {
		return nil, false
	}

	args := make([]string, 0, len(callExpr.Args))
	for _, arg := range callExpr.Args {
		literal, ok := literalWord(arg)
		if !ok || (len(args) == 0 && literal == "") {
			return nil, false
		}
		args = append(args, literal)
	}

	return args, true
}

func NormalizeShellCommandName(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	base := path.Base(strings.ReplaceAll(command, "\\", "/"))
	base = strings.ToLower(strings.TrimSpace(base))
	base = strings.TrimSuffix(base, ".exe")
	base = strings.TrimSuffix(base, ".cmd")
	base = strings.TrimSuffix(base, ".bat")
	base = strings.TrimSuffix(base, ".com")
	return base
}

func literalWord(word *syntax.Word) (string, bool) {
	if word == nil || len(word.Parts) == 0 {
		return "", false
	}

	var out strings.Builder
	for _, part := range word.Parts {
		switch x := part.(type) {
		case *syntax.Lit:
			out.WriteString(x.Value)
		case *syntax.SglQuoted:
			out.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, nested := range x.Parts {
				lit, ok := nested.(*syntax.Lit)
				if !ok {
					return "", false
				}
				out.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}

	return out.String(), true
}
