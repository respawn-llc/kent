package sessionservice

import (
	"context"
	"path/filepath"
	"testing"

	"core/server/auth"
	"core/server/session"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/rollbacktarget"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
)

func TestSessionTransitionMapsEveryActionToTypedLifecycleResult(t *testing.T) {
	parentID := runtimeids.NewSessionID()
	service := newTestSessionLifecycleService(t.TempDir(), nil)

	tests := []struct {
		name       string
		transition *sessionlaunchpb.SessionTransition
		assert     func(t *testing.T, result *sessionlaunchpb.SessionDirective)
	}{
		{
			name:       "none stops",
			transition: &sessionlaunchpb.SessionTransition{Action: sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_NONE},
			assert: func(t *testing.T, result *sessionlaunchpb.SessionDirective) {
				if result.GetStop() == nil {
					t.Fatalf("result = %v, want stop", result)
				}
			},
		},
		{
			name:       "resume selects with current auth",
			transition: &sessionlaunchpb.SessionTransition{Action: sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_RESUME},
			assert: func(t *testing.T, result *sessionlaunchpb.SessionDirective) {
				assertSessionLifecycleAuth(t, result, sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH)
			},
		},
		{
			name: "new session launches create with parent and prompt",
			transition: &sessionlaunchpb.SessionTransition{
				Action:                       sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_NEW_SESSION,
				InitialPrompt:                "seed prompt",
				InitialPromptHistoryRecorded: true,
				PreviousSessionId:            proto.String(parentID.String()),
			},
			assert: func(t *testing.T, result *sessionlaunchpb.SessionDirective) {
				intent, preparation := requireSessionLifecycleLaunch(t, result)
				if intent.Kind() != serverapi.SessionLaunchIntentCreateNew {
					t.Fatalf("intent kind = %q, want create new", intent.Kind())
				}
				origin, ok := intent.CreateOrigin()
				source, hasSource := origin.SessionID()
				if !ok || origin.Kind() != serverapi.SessionCreateOriginPreviousSession || !hasSource || source != parentID {
					t.Fatalf("origin = %+v/%v source=%q/%v, want previous session %q", origin, ok, source.String(), hasSource, parentID.String())
				}
				assertSessionLaunchPreparation(
					t,
					preparation,
					&sessionlaunchpb.SessionInitialPromptMetadata{Text: "seed prompt", HistoryRecorded: true},
					"",
					false, sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH,
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
				Transition: test.transition,
			})
			if err != nil {
				t.Fatalf("ResolveTransition: %v", err)
			}
			if test.assert != nil {
				test.assert(t, result)
			}
		})
	}
}

func TestSessionTransitionRollbackLaunchesCreatedFork(t *testing.T) {
	_, containerDir, store := createPersistedSession(t)
	appendSessionMessage(t, store, "step-1", session.MessageRoleUser, "u1")
	appendSessionMessage(t, store, "step-1", session.MessageRoleAssistant, "a1")

	service := newTestSessionLifecycleService(containerDir, nil)
	result, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: proto.String(store.Meta().SessionID),
		Transition: &sessionlaunchpb.SessionTransition{
			Action:                       sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
			InitialPrompt:                "edited prompt",
			InitialPromptHistoryRecorded: true,
			ForkRollbackTargetId:         proto.String(rollbacktarget.EncodeUserMessageSeq(userMessageSeqAt(t, store, 1))),
		},
	})
	if err != nil {
		t.Fatalf("ResolveTransition: %v", err)
	}
	intent, preparation := requireSessionLifecycleLaunch(t, result)
	if intent.Kind() != serverapi.SessionLaunchIntentOpenExisting {
		t.Fatalf("intent kind = %q, want open existing", intent.Kind())
	}
	forkID, ok := intent.SessionID()
	if !ok {
		t.Fatal("rollback launch omitted fork session ID")
	}
	if forkID.String() == store.Meta().SessionID {
		t.Fatal("rollback launch targeted the parent")
	}
	if _, err := session.Open(filepath.Join(containerDir, forkID.String()), sessionServiceTestPersistence.Options()...); err != nil {
		t.Fatalf("open forked session: %v", err)
	}
	assertSessionLaunchPreparation(
		t,
		preparation,
		&sessionlaunchpb.SessionInitialPromptMetadata{Text: "edited prompt", HistoryRecorded: true},
		"",
		false, sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH,
	)
}

func TestSessionTransitionLogoutResultDependsOnCurrentSession(t *testing.T) {
	manager := auth.NewManager(auth.NewMemoryStore(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "sk-before"},
		},
	}), nil)
	service := newTestSessionLifecycleService(t.TempDir(), manager)
	currentID := runtimeids.NewSessionID()

	withCurrent, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId:  proto.String(currentID.String()),
		Transition: &sessionlaunchpb.SessionTransition{Action: sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_LOGOUT},
	})
	if err != nil {
		t.Fatalf("ResolveTransition with current session: %v", err)
	}
	intent, preparation := requireSessionLifecycleLaunch(t, withCurrent)
	target, ok := intent.SessionID()
	if intent.Kind() != serverapi.SessionLaunchIntentOpenExisting || !ok || target != currentID {
		t.Fatalf("logout launch intent = kind %q target %q/%v", intent.Kind(), target.String(), ok)
	}
	assertSessionLaunchPreparation(
		t,
		preparation,
		nil,
		"",
		false, sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE,
	)

	withoutCurrent, err := service.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		Transition: &sessionlaunchpb.SessionTransition{Action: sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_LOGOUT},
	})
	if err != nil {
		t.Fatalf("ResolveTransition without current session: %v", err)
	}
	if withoutCurrent.GetSelectSession() == nil {
		t.Fatalf("result = %v, want select session", withoutCurrent)
	}
	assertSessionLifecycleAuth(t, withoutCurrent, sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE)
}

func requireSessionDirectiveWireEqual(t *testing.T, got *sessionlaunchpb.SessionDirective, want *sessionlaunchpb.SessionDirective) {
	t.Helper()
	if !proto.Equal(got, want) {
		t.Fatalf("directive = %v, want %v", got, want)
	}
}

func requireSessionLifecycleLaunch(t *testing.T, result *sessionlaunchpb.SessionDirective) (serverapi.SessionLaunchIntent, *sessionlaunchpb.SessionLaunchPreparation) {
	t.Helper()
	intent, err := protoapi.SessionLaunchIntentFromProto(result.GetLaunch().GetIntent())
	if err != nil {
		t.Fatal(err)
	}
	preparation := result.GetLaunch().GetPreparation()
	if preparation == nil {
		t.Fatal("launch result omitted preparation")
	}
	return intent, preparation
}

func assertSessionLifecycleAuth(t *testing.T, result *sessionlaunchpb.SessionDirective, want sessionlaunchpb.SessionAuthPreparation) {
	t.Helper()
	authPreparation := result.GetSelectSession()
	if authPreparation == nil || authPreparation.Auth != want {
		t.Fatalf("auth preparation = %v, want %v", authPreparation, want)
	}
}

func assertSessionLaunchPreparation(
	t *testing.T,
	preparation *sessionlaunchpb.SessionLaunchPreparation,
	wantPrompt *sessionlaunchpb.SessionInitialPromptMetadata,
	wantOverride string,
	wantOverridePresent bool,
	wantAuth sessionlaunchpb.SessionAuthPreparation,
) {
	t.Helper()
	if !proto.Equal(preparation.InitialPrompt, wantPrompt) {
		t.Fatalf("initial prompt = %v, want %v", preparation.InitialPrompt, wantPrompt)
	}
	inputPolicy := preparation.InputPolicy
	override, hasOverride := inputPolicy.GetDisposition().(*sessionlaunchpb.SessionDraftDisposition_OverrideStoredDraft)
	if hasOverride != wantOverridePresent || (hasOverride && override.OverrideStoredDraft != wantOverride) ||
		(!hasOverride && inputPolicy.GetRestoreStoredDraft() == nil) {
		t.Fatalf("input policy = %v, want override %q/%v", inputPolicy, wantOverride, wantOverridePresent)
	}
	if preparation.Auth != wantAuth {
		t.Fatalf("auth preparation = %v, want %v", preparation.Auth, wantAuth)
	}
}
