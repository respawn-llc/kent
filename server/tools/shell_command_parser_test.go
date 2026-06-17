package tools

import "testing"

func TestParseSimpleCommandAllowsEmptyQuotedArgumentAfterCommandName(t *testing.T) {
	args, ok := ParseSimpleShellCommand("printf ''")
	if !ok {
		t.Fatal("expected empty quoted arg to parse")
	}
	if len(args) != 2 || args[0] != "printf" || args[1] != "" {
		t.Fatalf("args = %#v, want [printf \"\"]", args)
	}
}

func TestParseSimpleCommandRejectsLeadingEnvAssignments(t *testing.T) {
	if _, ok := ParseSimpleShellCommand("FOO=bar go test ./..."); ok {
		t.Fatal("expected leading env assignment command to stay unsupported")
	}
}
