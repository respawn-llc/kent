package metadata

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"core/server/session"
	"core/shared/clientui"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/toolspec"
)

func TestGoalObservationHydratesDormantSessionAndPublishesLaterPersistence(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	metadataStore, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	binding, err := metadataStore.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	sessionStore, err := session.Create(
		filepath.Join(root, "projects", binding.ProjectID, "sessions"),
		"workspace",
		workspace,
		sessioncontract.SessionCategoryMain,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	subscription, err := metadataStore.SubscribeGoalObservation(t.Context(), serverapi.GoalObserveRequest{
		SessionID: sessionStore.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("SubscribeGoalObservation: %v", err)
	}
	hydration, err := subscription.Next(t.Context())
	if err != nil {
		t.Fatalf("read hydration: %v", err)
	}
	if hydration.Sequence != 1 ||
		hydration.Kind != clientui.GoalObservationHydration ||
		hydration.Status.Goal != nil ||
		hydration.Status.Availability == nil {
		t.Fatalf("hydration = %+v", hydration)
	}

	goal, _, err := sessionStore.SetGoal("ship observation", session.GoalActorUser)
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	update, err := subscription.Next(t.Context())
	if err != nil {
		t.Fatalf("read update: %v", err)
	}
	if update.Sequence != 2 ||
		update.Kind != clientui.GoalObservationUpdate ||
		update.Status.Goal == nil ||
		update.Status.Goal.ID != goal.ID {
		t.Fatalf("update = %+v, want Goal %q", update, goal.ID)
	}
	if err := sessionStore.SetName("unrelated metadata"); err != nil {
		t.Fatalf("SetName: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := subscription.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("equal Goal projection emitted another update: %v", err)
	}

	if err := subscription.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := subscription.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("Next after Close = %v, want EOF", err)
	}
	metadataStore.goalObservations.mu.Lock()
	_, retained := metadataStore.goalObservations.sessions[sessionStore.Meta().SessionID]
	metadataStore.goalObservations.mu.Unlock()
	if retained {
		t.Fatal("final Goal observer left per-Session broker state")
	}
}

func TestGoalObservationPublishesAvailabilityOnlyContractChanges(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	metadataStore, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	binding, err := metadataStore.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	sessionStore, err := session.Create(
		filepath.Join(root, "projects", binding.ProjectID, "sessions"),
		"workspace",
		workspace,
		sessioncontract.SessionCategoryMain,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	subscription, err := metadataStore.SubscribeGoalObservation(t.Context(), serverapi.GoalObserveRequest{
		SessionID: sessionStore.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("SubscribeGoalObservation: %v", err)
	}
	defer func() { _ = subscription.Close() }()
	_, _ = subscription.Next(t.Context())

	if err := sessionStore.MarkModelDispatchLocked(session.LockedContract{
		Model:           "gpt-5",
		EnabledTools:    []string{string(toolspec.ToolExecCommand)},
		HasEnabledTools: true,
	}); err != nil {
		t.Fatalf("MarkModelDispatchLocked: %v", err)
	}
	update, err := subscription.Next(t.Context())
	if err != nil {
		t.Fatalf("read availability update: %v", err)
	}
	if update.Status.Goal != nil ||
		update.Status.Availability == nil ||
		*update.Status.Availability != clientui.GoalAvailabilityAgentCapabilityMissing {
		t.Fatalf("availability-only update = %+v", update)
	}
}

func TestGoalObservationAllowsACommitBetweenHydrationReadAndRegistrationToBeMissed(t *testing.T) {
	broker := newGoalObservationBroker()
	availability := clientui.GoalAvailabilityAvailable
	stale := clientui.GoalProjection{Availability: &availability}
	current := clientui.GoalProjection{
		Goal: &clientui.Goal{
			ID:        "goal-1",
			Objective: "newer commit",
			Status:    clientui.RuntimeGoalStatusActive,
			CreatedAt: testGoalObservationTime,
			UpdatedAt: testGoalObservationTime,
		},
		Availability: &availability,
	}
	broker.publish("session-1", current)
	subscription := broker.subscribe("session-1", stale)
	hydration, err := subscription.Next(t.Context())
	if err != nil {
		t.Fatalf("read stale hydration: %v", err)
	}
	if hydration.Status.Goal != nil {
		t.Fatalf("hydration unexpectedly fenced missed commit: %+v", hydration)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := subscription.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("missed commit produced an update: %v", err)
	}
	broker.publish("session-1", current)
	update, err := subscription.Next(t.Context())
	if err != nil || update.Sequence != 2 || update.Status.Goal == nil {
		t.Fatalf("later observed commit = %+v, %v", update, err)
	}
}

var testGoalObservationTime = time.Unix(1, 0).UTC()
