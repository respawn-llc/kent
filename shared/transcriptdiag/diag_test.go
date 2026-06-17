package transcriptdiag

import (
	"testing"

	"core/shared/clientui"
)

func TestEventDigestIncludesRunLifecycleMode(t *testing.T) {
	base := clientui.Event{
		Kind:   clientui.EventRunStateChanged,
		StepID: "step-1",
		RunState: &clientui.RunState{
			Lifecycle: clientui.MustRunLifecycle(clientui.RunLifecycleRunning, clientui.RunModeTurn),
			RunID:     "run-1",
			Status:    clientui.RunStatusRunning,
		},
	}
	goal := base
	goal.RunState = &clientui.RunState{
		Lifecycle: clientui.MustRunLifecycle(clientui.RunLifecycleRunning, clientui.RunModeGoalLoop),
		RunID:     "run-1",
		Status:    clientui.RunStatusRunning,
	}

	if got, wantDifferent := EventDigest(base), EventDigest(goal); got == wantDifferent {
		t.Fatalf("run lifecycle mode must affect digest, got collision %q", got)
	}
}
