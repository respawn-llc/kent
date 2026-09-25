package main

import (
	"fmt"
	"testing"

	"core/shared/serverapi"
)

func TestRunErrorMessagePreservesCallerContext(t *testing.T) {
	err := fmt.Errorf("caller context remediation: %w", &serverapi.SubagentLaunchDeniedError{
		Kind: serverapi.SubagentLaunchDenialCallerMissing,
	})
	if got := runErrorMessage(err); got != err.Error() {
		t.Fatalf("caller context discarded: %v", got)
	}
}
