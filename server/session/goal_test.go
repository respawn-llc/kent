package session

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestSetGoalPersistsMetadataAndEvent(t *testing.T) {
	store := newSessionTestStore(t)

	goal, err := store.SetGoal("  ship goal mode\nwith docs  ", GoalActorUser)
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if goal.ID == "" {
		t.Fatalf("goal id is empty")
	}
	if goal.Objective != "ship goal mode\nwith docs" {
		t.Fatalf("objective = %q", goal.Objective)
	}
	if goal.Status != GoalStatusActive {
		t.Fatalf("status = %q, want active", goal.Status)
	}

	reopened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	persisted := reopened.Meta().Goal
	if persisted == nil {
		t.Fatalf("persisted goal is nil")
	}
	if *persisted != goal {
		t.Fatalf("persisted goal = %+v, want %+v", *persisted, goal)
	}

	events, err := reopened.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Kind != "goal_set" {
		t.Fatalf("event kind = %q, want goal_set", events[0].Kind)
	}
	var payload GoalSetEvent
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Actor != GoalActorUser {
		t.Fatalf("actor = %q, want user", payload.Actor)
	}
	if payload.Goal != goal {
		t.Fatalf("payload goal = %+v, want %+v", payload.Goal, goal)
	}
	if payload.ReplacedGoalID != "" {
		t.Fatalf("replaced goal id = %q, want empty", payload.ReplacedGoalID)
	}
}

func TestGoalWithEventsRollsBackMetadataWhenEventAppendFails(t *testing.T) {
	store := newSessionTestStore(t)

	makeEventsPathDirectory(t, store)
	_, err := store.SetGoalWithEvents("ship goal mode", GoalActorUser, []EventInput{{Kind: "message", Payload: "goal feedback"}})
	if err == nil {
		t.Fatal("expected SetGoalWithEvents to fail when event append fails")
	}
	if goal := store.Meta().Goal; goal != nil {
		t.Fatalf("goal after failed atomic set = %+v, want nil", goal)
	}
	restoreEmptyEventsFile(t, store)
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events after failed atomic set = %+v, want none", events)
	}

	goal, err := store.SetGoal("ship goal mode", GoalActorUser)
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	previous := *store.Meta().Goal
	makeEventsPathDirectory(t, store)
	if _, err := store.SetGoalStatusWithEvents(GoalStatusPaused, GoalActorUser, []EventInput{{Kind: "message", Payload: "goal feedback"}}); err == nil {
		t.Fatal("expected SetGoalStatusWithEvents to fail when event append fails")
	}
	if got := store.Meta().Goal; got == nil || !reflect.DeepEqual(*got, previous) {
		t.Fatalf("goal after failed status update = %+v, want %+v", got, previous)
	}

	restoreEmptyEventsFile(t, store)
	previous = goal
	makeEventsPathDirectory(t, store)
	if _, err := store.ClearGoalWithEvents(GoalActorUser, []EventInput{{Kind: "message", Payload: "goal feedback"}}); err == nil {
		t.Fatal("expected ClearGoalWithEvents to fail when event append fails")
	}
	if got := store.Meta().Goal; got == nil || !reflect.DeepEqual(*got, previous) {
		t.Fatalf("goal after failed clear = %+v, want %+v", got, previous)
	}
}

func makeEventsPathDirectory(t *testing.T, store *Store) {
	t.Helper()
	if err := os.Remove(store.eventsFP); err != nil {
		t.Fatalf("remove events file: %v", err)
	}
	if err := os.Mkdir(store.eventsFP, 0o755); err != nil {
		t.Fatalf("replace events file with directory: %v", err)
	}
}

func restoreEmptyEventsFile(t *testing.T, store *Store) {
	t.Helper()
	if err := os.Remove(store.eventsFP); err != nil {
		t.Fatalf("remove events directory: %v", err)
	}
	if err := os.WriteFile(store.eventsFP, nil, 0o644); err != nil {
		t.Fatalf("restore events file: %v", err)
	}
}

func TestGoalStatusAndClearPersistMetadataAndEvents(t *testing.T) {
	store := newSessionTestStore(t)
	first, err := store.SetGoal("first goal", GoalActorUser)
	if err != nil {
		t.Fatalf("SetGoal first: %v", err)
	}
	second, err := store.SetGoal("second goal", GoalActorUser)
	if err != nil {
		t.Fatalf("SetGoal second: %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("replacement reused goal id %q", second.ID)
	}

	paused, err := store.SetGoalStatus(GoalStatusPaused, GoalActorAgent)
	if err != nil {
		t.Fatalf("SetGoalStatus paused: %v", err)
	}
	if paused.Status != GoalStatusPaused {
		t.Fatalf("paused status = %q", paused.Status)
	}
	cleared, err := store.ClearGoal(GoalActorUser)
	if err != nil {
		t.Fatalf("ClearGoal: %v", err)
	}
	if cleared.ID != second.ID || cleared.Status != GoalStatusPaused {
		t.Fatalf("cleared goal = %+v, want second paused goal", cleared)
	}
	if store.Meta().Goal != nil {
		t.Fatalf("meta goal after clear = %+v, want nil", store.Meta().Goal)
	}

	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events len = %d, want 4", len(events))
	}
	var replacement GoalSetEvent
	if err := json.Unmarshal(events[1].Payload, &replacement); err != nil {
		t.Fatalf("decode replacement: %v", err)
	}
	if replacement.ReplacedGoalID != first.ID {
		t.Fatalf("replaced id = %q, want %q", replacement.ReplacedGoalID, first.ID)
	}
	var status GoalStatusUpdatedEvent
	if err := json.Unmarshal(events[2].Payload, &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if events[2].Kind != "goal_status_updated" || status.Actor != GoalActorAgent || status.PreviousStatus != GoalStatusActive || status.Goal.Status != GoalStatusPaused {
		t.Fatalf("status event kind/payload = %s %+v", events[2].Kind, status)
	}
	var clear GoalClearedEvent
	if err := json.Unmarshal(events[3].Payload, &clear); err != nil {
		t.Fatalf("decode clear: %v", err)
	}
	if events[3].Kind != "goal_cleared" || clear.Actor != GoalActorUser || clear.Goal.ID != second.ID {
		t.Fatalf("clear event kind/payload = %s %+v", events[3].Kind, clear)
	}
}

func TestGoalValidationRejectsInvalidValues(t *testing.T) {
	store := newSessionTestStore(t)
	if _, err := store.SetGoal(" \n\t ", GoalActorUser); err == nil {
		t.Fatalf("SetGoal empty objective error = nil")
	}
	if _, err := store.SetGoal("objective", GoalActor("robot")); err == nil {
		t.Fatalf("SetGoal invalid actor error = nil")
	}
	if _, err := store.SetGoal("objective", GoalActorUser); err != nil {
		t.Fatalf("SetGoal valid: %v", err)
	}
	if _, err := store.SetGoalStatus(GoalStatus("blocked"), GoalActorUser); err == nil {
		t.Fatalf("SetGoalStatus invalid status error = nil")
	}
}
