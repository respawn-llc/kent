package app

import "testing"

func TestRunPromptCallerSessionIDCarriesInheritedIdentity(t *testing.T) {
	callerID := runPromptCallerSessionID(Options{WorkspaceContextSessionID: "context-session"})
	if callerID == nil || *callerID != "context-session" {
		t.Fatalf("caller session ID = %v", callerID)
	}
	if runPromptCallerSessionID(Options{}) != nil {
		t.Fatal("human caller must not carry a Session identity")
	}
}
