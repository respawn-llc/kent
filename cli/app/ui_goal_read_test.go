package app

import (
	"io"
	"testing"

	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
)

func TestGoalShowAppliesSameSessionCompletionsInArrivalOrder(t *testing.T) {
	for _, outcome := range []struct {
		name          string
		firstFailure  bool
		secondFailure bool
	}{
		{name: "success_after_success"},
		{name: "error_after_success", firstFailure: true},
		{name: "success_after_error", secondFailure: true},
		{name: "error_after_error", firstFailure: true, secondFailure: true},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			client := &runtimeControlFakeClient{}
			m := newProjectedClosedUIModel(client)
			m.setKnownSessionTarget(detailTestSessionID)
			m.openGoalOverlay(nil, nil)
			m.goal.goal = goalCoreFromRuntimeGoal(runtimeControlTestGoal("initial", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE))
			first := m.goalRuntimeCommand(goalRuntimeShow, "")
			second := m.goalRuntimeCommand(goalRuntimeShow, "")
			client.goal = runtimeControlTestGoal("second", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE)
			if outcome.secondFailure {
				client.err = io.EOF
			}
			m.applyGoalRuntimeDone(second().(goalRuntimeDoneMsg))
			lastGoal := m.goal.goal
			client.err = nil
			if outcome.firstFailure {
				client.err = io.EOF
			} else {
				client.goal = runtimeControlTestGoal("first", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_PAUSED)
			}
			m.applyGoalRuntimeDone(first().(goalRuntimeDoneMsg))
			if outcome.firstFailure {
				if m.goal.error == "" || !m.runtimeDisconnectStatusVisible() {
					t.Fatal("earlier read error was not applied after the later read")
				}
				if m.goal.goal != lastGoal {
					t.Fatal("read error changed the displayed Goal")
				}
			} else if m.goal.goal == nil || m.goal.goal.Objective != "first" || m.goal.error != "" || m.runtimeDisconnectStatusVisible() {
				t.Fatalf("earlier read success was not applied: %+v", m.goal)
			}
		})
	}
}

func TestGoalShowDoesNotSupersedeMutationPreflight(t *testing.T) {
	for _, operation := range []goalRuntimeOperation{goalRuntimeCheckSet, goalRuntimeCheckClear} {
		t.Run(string(operation), func(t *testing.T) {
			client := &runtimeControlFakeClient{
				goal: runtimeControlTestGoal("current", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE),
			}
			m := newProjectedClosedUIModel(client)
			m.setKnownSessionTarget(detailTestSessionID)
			m.openGoalOverlay(nil, nil)
			check := m.goalRuntimeCommand(operation, "replacement")
			m.applyGoalRuntimeDone(m.goalRuntimeCommand(goalRuntimeShow, "")().(goalRuntimeDoneMsg))
			m.applyGoalRuntimeDone(check().(goalRuntimeDoneMsg))
			if m.goal.confirmMode == "" {
				t.Fatal("Show superseded the mutation confirmation")
			}
		})
	}
}

func TestGoalShowIgnoresCompletionsOutsideItsDestination(t *testing.T) {
	for _, destination := range []string{"closed", "confirmation", "other_session", "new_session", "mutation"} {
		for _, failure := range []bool{false, true} {
			name := destination + "/success"
			if failure {
				name = destination + "/error"
			}
			t.Run(name, func(t *testing.T) {
				client := &runtimeControlFakeClient{
					goal: runtimeControlTestGoal("old", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE),
				}
				m := newProjectedClosedUIModel(client)
				m.setKnownSessionTarget(detailTestSessionID)
				m.openGoalOverlay(nil, nil)
				show := m.goalRuntimeCommand(goalRuntimeShow, "")
				switch destination {
				case "closed":
					m.closeGoalOverlay()
				case "confirmation":
					m.openGoalConfirmOverlay("replace", client.goal, "replacement", nil)
				case "other_session":
					m.setKnownSessionTarget(runtimeids.NewSessionID().String())
				case "new_session":
					m.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{}}
				case "mutation":
					m.applyGoalRuntimeDone(m.goalRuntimeCommand(goalRuntimePause, "")().(goalRuntimeDoneMsg))
				}
				goal := m.goal.goal
				m.goal.error = "existing error"
				m.setRuntimeDisconnected(!failure)
				if failure {
					client.err = io.EOF
				}
				if cmd := m.applyGoalRuntimeDone(show().(goalRuntimeDoneMsg)); cmd != nil {
					t.Fatal("discarded Show produced a follow-up")
				}
				if m.goal.goal != goal || m.goal.error != "existing error" || m.runtimeDisconnectStatusVisible() != !failure {
					t.Fatalf("discarded Show changed Goal, error, or connection: %+v", m.goal)
				}
			})
		}
	}
}

func TestGoalShowStartedBeforeMutationPreflightRespectsConfirmation(t *testing.T) {
	for _, operation := range []goalRuntimeOperation{goalRuntimeCheckSet, goalRuntimeCheckClear} {
		for _, order := range []string{"show_first", "confirmation_first"} {
			for _, failure := range []bool{false, true} {
				name := string(operation) + "/" + order + "/success"
				if failure {
					name = string(operation) + "/" + order + "/error"
				}
				t.Run(name, func(t *testing.T) {
					client := &runtimeControlFakeClient{
						goal: runtimeControlTestGoal("earlier", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE),
					}
					m := newProjectedClosedUIModel(client)
					m.setKnownSessionTarget(detailTestSessionID)
					m.openGoalOverlay(nil, nil)
					if failure {
						client.err = io.EOF
					}
					show := m.goalRuntimeCommand(goalRuntimeShow, "")().(goalRuntimeDoneMsg)
					client.err = nil
					client.goal = runtimeControlTestGoal("confirmation", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE)
					check := m.goalRuntimeCommand(operation, "replacement")().(goalRuntimeDoneMsg)
					if order == "show_first" {
						m.applyGoalRuntimeDone(show)
						if failure {
							if m.goal.error == "" || !m.runtimeDisconnectStatusVisible() {
								t.Fatal("pending preflight rejected the earlier Show error")
							}
						} else if m.goal.goal == nil || m.goal.goal.Objective != "earlier" {
							t.Fatal("pending preflight rejected the earlier Show success")
						}
						m.applyGoalRuntimeDone(check)
					} else {
						m.applyGoalRuntimeDone(check)
						goal := m.goal.goal
						m.goal.error = "existing error"
						m.setRuntimeDisconnected(!failure)
						if cmd := m.applyGoalRuntimeDone(show); cmd != nil {
							t.Fatal("Show returned a follow-up over the confirmation")
						}
						if m.goal.goal != goal || m.goal.error != "existing error" || m.runtimeDisconnectStatusVisible() != !failure {
							t.Fatal("earlier Show changed the confirmation, error, or connection")
						}
					}
					if m.goal.confirmMode == "" || m.goal.goal == nil || m.goal.goal.Objective != "confirmation" {
						t.Fatal("preflight confirmation did not keep its authoritative Goal")
					}
				})
			}
		}
	}
}

func TestGoalShowCanCompleteAfterSameSessionOverlayReopens(t *testing.T) {
	client := &runtimeControlFakeClient{
		goal: runtimeControlTestGoal("current", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE),
	}
	m := newProjectedClosedUIModel(client)
	m.setKnownSessionTarget(detailTestSessionID)
	m.openGoalOverlay(nil, nil)
	first := m.goalRuntimeCommand(goalRuntimeShow, "")
	m.closeGoalOverlay()
	m.openGoalOverlay(nil, nil)
	m.goalRuntimeCommand(goalRuntimeShow, "")
	m.applyGoalRuntimeDone(first().(goalRuntimeDoneMsg))
	if m.goal.goal == nil || m.goal.goal.Objective != "current" {
		t.Fatal("same-Session reopened overlay rejected an earlier Show")
	}
}

func TestGoalShowWithoutSessionDoesNotChangeOverlayOrConnection(t *testing.T) {
	m := newProjectedClosedUIModel(&runtimeControlFakeClient{})
	m.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{}}
	m.openGoalOverlay(nil, nil)
	m.goal.error = "existing error"
	m.setRuntimeDisconnected(true)
	if cmd := m.applyGoalRuntimeDone(m.goalRuntimeCommand(goalRuntimeShow, "")().(goalRuntimeDoneMsg)); cmd != nil {
		t.Fatal("Show without a Session produced a follow-up")
	}
	if m.goal.goal != nil || m.goal.error != "existing error" || !m.runtimeDisconnectStatusVisible() {
		t.Fatal("Show without a Session changed the overlay or connection")
	}
}

func TestGoalMutationPreflightRejectsSupersededReads(t *testing.T) {
	for _, operation := range []goalRuntimeOperation{goalRuntimeCheckSet, goalRuntimeCheckClear} {
		for _, supersededBy := range []string{"preflight", "mutation", "coalesced_mutation"} {
			t.Run(string(operation)+"/"+supersededBy, func(t *testing.T) {
				client := &runtimeControlFakeClient{
					goal: runtimeControlTestGoal("current", runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE),
				}
				m := newProjectedClosedUIModel(client)
				m.setKnownSessionTarget(detailTestSessionID)
				m.openGoalOverlay(nil, nil)
				if supersededBy == "coalesced_mutation" {
					m.goalRuntimeCommand(goalRuntimePause, "")
				}
				first := m.goalRuntimeCommand(operation, "replacement")
				switch supersededBy {
				case "preflight":
					m.goalRuntimeCommand(operation, "newer replacement")
				case "mutation", "coalesced_mutation":
					m.goalRuntimeCommand(goalRuntimePause, "")
				}
				if cmd := m.applyGoalRuntimeDone(first().(goalRuntimeDoneMsg)); cmd != nil || m.goal.confirmMode != "" {
					t.Fatal("superseded preflight started a mutation or confirmation")
				}
			})
		}
	}
}
