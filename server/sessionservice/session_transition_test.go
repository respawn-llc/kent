package sessionservice

import (
	"context"
	"path/filepath"
	"testing"

	"core/server/session"
	"core/server/session/sessiontest"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestInitialInputPrefersPersistedDraft(t *testing.T) {
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/work", sessioncontract.SessionCategoryMain, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	if err := store.SetInputDraft("persisted"); err != nil {
		t.Fatalf("set input draft: %v", err)
	}
	if got := initialSessionInput(store.Meta(), "fallback"); got != "persisted" {
		t.Fatalf("initial input = %q, want persisted", got)
	}
}

func TestPersistInputDraftNoOpForNilStore(t *testing.T) {
	if err := persistSessionInputDraft(nil, "draft"); err != nil {
		t.Fatalf("persist input draft with nil store: %v", err)
	}
}

func TestResolveOpenSessionRestoresStoredDraftWhenInitialInputIsOmitted(t *testing.T) {
	targetID := runtimeids.NewSessionID()
	resolved, err := resolveSessionTransition(context.Background(), sessionTransitionResolveRequest{
		Transition: sessionTransition{
			Action:          sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_OPEN_SESSION,
			TargetSessionID: targetID.String(),
		},
	})
	if err != nil {
		t.Fatalf("resolve open Session: %v", err)
	}
	intent, preparation := requireSessionLifecycleLaunch(t, resolved)
	resolvedTargetID, existing := intent.SessionID()
	if !existing || resolvedTargetID != targetID {
		t.Fatalf("open Session target = %q/%t, want %q", resolvedTargetID, existing, targetID)
	}
	if preparation.InputPolicy.GetRestoreStoredDraft() == nil {
		t.Fatalf("open Session draft disposition = %v, want restore stored draft", preparation.InputPolicy)
	}
}

func TestResolveOpenSessionPreservesIntentionalEmptyDraftOverride(t *testing.T) {
	targetID := runtimeids.NewSessionID()
	resolved, err := resolveSessionTransition(context.Background(), sessionTransitionResolveRequest{
		Transition: sessionTransition{
			Action:          sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_OPEN_SESSION,
			InitialInput:    textutil.Value(""),
			TargetSessionID: targetID.String(),
		},
	})
	if err != nil {
		t.Fatalf("resolve open Session: %v", err)
	}
	_, preparation := requireSessionLifecycleLaunch(t, resolved)
	override, present := preparation.InputPolicy.Disposition.(*sessionlaunchpb.SessionDraftDisposition_OverrideStoredDraft)
	if !present || override.OverrideStoredDraft != "" {
		t.Fatalf("open Session draft override = %v, want intentional empty override", preparation.InputPolicy)
	}
}

func TestResolveForkRollbackCreatesForkedSession(t *testing.T) {
	root := t.TempDir()
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(root, "workspace-x", "/tmp/work", sessioncontract.SessionCategoryMain, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	if err := store.SetName("parent"); err != nil {
		t.Fatalf("set session name: %v", err)
	}
	appendSessionMessage(t, store, "s1", session.MessageRoleUser, "u1")
	appendSessionMessage(t, store, "s1", session.MessageRoleAssistant, "a1")
	u2Evt := appendSessionMessage(t, store, "s2", session.MessageRoleUser, "u2")
	appendSessionMessage(t, store, "s2", session.MessageRoleAssistant, "a2")

	resolved, err := resolveSessionTransition(context.Background(), sessionTransitionResolveRequest{
		Store:        store,
		ForkThinking: session.ForkThinking{Desired: "medium"},
		Transition: sessionTransition{
			Action:             sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
			InitialPrompt:      "edited user message",
			ForkUserMessageSeq: u2Evt.Seq(),
		},
	})
	if err != nil {
		t.Fatalf("resolve fork rollback: %v", err)
	}
	intent, preparation := requireSessionLifecycleLaunch(t, resolved)
	forkID, ok := intent.SessionID()
	if !ok || forkID.String() == store.Meta().SessionID {
		t.Fatalf("expected new fork session id, got %q/%v", forkID.String(), ok)
	}
	prompt := preparation.InitialPrompt
	if prompt == nil || prompt.Text != "edited user message" {
		t.Fatalf("initial prompt = %+v", prompt)
	}
	child, err := persistence.Open(filepath.Join(root, forkID.String()))
	if err != nil {
		t.Fatalf("open forked session: %v", err)
	}
	if got := child.Meta().Name; got != "parent \u2192 edit u2" {
		t.Fatalf("forked session name = %q", got)
	}
}

func TestResolveForkRollbackPreservesIntentionalEmptyDraftOverride(t *testing.T) {
	root := t.TempDir()
	persistence := sessiontest.NewPersistence()
	store, err := session.Create(root, "workspace-x", "/tmp/work", sessioncontract.SessionCategoryMain, persistence.Options()...)
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	appendSessionMessage(t, store, "s1", session.MessageRoleUser, "u1")
	appendSessionMessage(t, store, "s1", session.MessageRoleAssistant, "a1")
	u2Evt := appendSessionMessage(t, store, "s2", session.MessageRoleUser, "u2")
	appendSessionMessage(t, store, "s2", session.MessageRoleAssistant, "a2")

	resolved, err := resolveSessionTransition(context.Background(), sessionTransitionResolveRequest{
		Store:        store,
		ForkThinking: session.ForkThinking{Desired: "medium"},
		Transition: sessionTransition{
			Action:             sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
			InitialInput:       textutil.Value(""),
			ForkUserMessageSeq: u2Evt.Seq(),
		},
	})
	if err != nil {
		t.Fatalf("resolve fork rollback: %v", err)
	}
	_, preparation := requireSessionLifecycleLaunch(t, resolved)
	if preparation.InitialPrompt != nil {
		t.Fatal("rollback fork must not submit the selected user message")
	}
	override, present := preparation.InputPolicy.Disposition.(*sessionlaunchpb.SessionDraftDisposition_OverrideStoredDraft)
	if !present || override.OverrideStoredDraft != "" {
		t.Fatalf("rollback fork draft = %v, want intentional empty override", preparation.InputPolicy)
	}
}
