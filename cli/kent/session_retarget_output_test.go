package main

import (
	"strings"
	"testing"

	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"mvdan.cc/sh/v3/shell"
)

func TestRebindGuidanceUsesProjectedSessionSelectors(t *testing.T) {
	id := runtimeids.NewSessionID()
	number, err := runtimeids.NewSessionShortID(73)
	if err != nil {
		t.Fatal(err)
	}
	for _, shortID := range []*runtimeids.SessionShortID{nil, &number} {
		expected := (runtimeids.SessionIdentity{ID: id, ShortID: shortID}).String()
		for _, reason := range []serverapi.SessionRetargetErrorReason{serverapi.SessionRetargetTargetProjectRequired, serverapi.SessionRetargetTargetProjectConflict} {
			failure := &serverapi.SessionRetargetError{
				Reason: reason, SessionID: id.String(), ShortID: shortID,
				SourceProject: serverapi.ProjectReference{ID: "source", Name: "Source"},
				TargetRoot:    "/workspace", CandidateProjects: []serverapi.ProjectReference{{ID: "target", Name: "Target"}},
			}
			output := formatSessionRetargetCommandError(failure).Error()
			commands := 0
			for _, line := range strings.Split(output, "\n") {
				words := strings.Fields(line)
				for index, word := range words {
					if word != config.Command || index+1 >= len(words) || words[index+1] != "rebind" {
						continue
					}
					args, err := shell.Fields(strings.Join(words[index+2:], " "), nil)
					if err != nil {
						t.Fatal(err)
					}
					if len(args) >= 2 && args[0] == "--project" {
						args = args[2:]
					}
					if len(args) != 2 || args[0] != expected || args[1] != failure.TargetRoot {
						t.Fatalf("rebind command targets wrong Session: %v", args)
					}
					commands++
				}
			}
			want := 1
			if reason == serverapi.SessionRetargetTargetProjectRequired {
				want = 2
			}
			if commands != want {
				t.Fatalf("corrective commands = %d, want %d", commands, want)
			}
		}
	}
}
