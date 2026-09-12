package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func requireSessionLifecycleResult(t *testing.T, result *sessionlaunchpb.SessionDirective) *sessionlaunchpb.SessionDirective {
	t.Helper()
	if err := protoapi.Validate(result); err != nil {
		t.Fatalf("expected valid session directive: %v", err)
	}
	return result
}

func requireSessionPickerDestination(t *testing.T, result *sessionlaunchpb.SessionDirective) {
	t.Helper()
	result = requireSessionLifecycleResult(t, result)
	if result.GetSelectSession() == nil {
		t.Fatalf("result = %v, want session picker", result)
	}
}

func requireSessionOpenDestination(t *testing.T, result *sessionlaunchpb.SessionDirective) string {
	t.Helper()
	result = requireSessionLifecycleResult(t, result)
	intent, err := protoapi.SessionLaunchIntentFromProto(result.GetLaunch().GetIntent())
	if err != nil || intent.Kind() != serverapi.SessionLaunchIntentOpenExisting {
		t.Fatalf("launch intent = %+v, want existing session", intent)
	}
	sessionID, present := intent.SessionID()
	if !present {
		t.Fatal("existing-session launch omitted session id")
	}
	return sessionID.String()
}

func requireSessionCreateDestination(t *testing.T, result *sessionlaunchpb.SessionDirective) *string {
	t.Helper()
	result = requireSessionLifecycleResult(t, result)
	intent, err := protoapi.SessionLaunchIntentFromProto(result.GetLaunch().GetIntent())
	if err != nil || intent.Kind() != serverapi.SessionLaunchIntentCreateNew {
		t.Fatalf("launch intent = %+v, want new session", intent)
	}
	origin, present := intent.CreateOrigin()
	if !present || origin.Kind() == serverapi.SessionCreateOriginIndependent {
		return nil
	}
	parentID, present := origin.SessionID()
	if !present {
		t.Fatalf("creation origin = %+v, want source session", origin)
	}
	value := parentID.String()
	return &value
}

func sessionLaunchRequestFromLifecycleResult(t *testing.T, result *sessionlaunchpb.SessionDirective, overrides serverapi.RunPromptOverrides) sessionLaunchRequest {
	t.Helper()
	result = requireSessionLifecycleResult(t, result)
	intent, err := protoapi.SessionLaunchIntentFromProto(result.GetLaunch().GetIntent())
	if err != nil {
		t.Fatal(err)
	}
	request, err := sessionLaunchRequestFromIntent(intent, overrides)
	if err != nil {
		t.Fatalf("build launch request: %v", err)
	}
	return request
}

func sessionLifecycleSessionIDForTest(t *testing.T, raw string) runtimeids.SessionID {
	t.Helper()
	id, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatalf("parse session id %q: %v", raw, err)
	}
	return id
}

func TestSessionLifecycleResultExitIsClientLocalStop(t *testing.T) {
	result, err := resolveSessionAction(
		context.Background(),
		nil,
		nil,
		"current-session",
		UITransition{Exit: true},
	)
	if err != nil {
		t.Fatalf("resolveSessionAction: %v", err)
	}
	if result.GetStop() == nil {
		t.Fatalf("result = %v, want stop", result)
	}
}

func TestSessionLifecycleResultReauthenticationCompletesBeforeDispatch(t *testing.T) {
	events := make([]string, 0, 3)
	target := runtimeids.NewSessionID()
	want, err := defaultSessionLaunchDirective(serverapi.OpenExistingSessionLaunchIntent(target))
	if err != nil {
		t.Fatal(err)
	}
	want.GetLaunch().Preparation.Auth = sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE
	result, err := resolveSessionAction(
		context.Background(),
		narrowSessionLifecycleServer{
			lifecycle: &recordingSessionLifecycleClient{
				resolveTransition: func(context.Context, *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error) {
					events = append(events, "resolve")
					return want, nil
				},
			},
			reauthenticate: func(context.Context, authInteractor) error {
				events = append(events, "reauthenticate")
				return nil
			},
		},
		nil,
		"current-session",
		UITransition{Action: UIActionLogout},
	)
	if err != nil {
		t.Fatalf("resolveSessionAction: %v", err)
	}
	events = append(events, "dispatch")
	requireSessionDirectiveWireEqual(t, result, want)
	if got := strings.Join(events, ","); got != "resolve,reauthenticate,dispatch" {
		t.Fatalf("event order = %q, want resolve,reauthenticate,dispatch", got)
	}
}

func TestSessionLifecycleResultAuthFailureDoesNotFabricateResult(t *testing.T) {
	authErr := errors.New("authentication canceled")
	target := runtimeids.NewSessionID()
	result, err := resolveSessionAction(
		context.Background(),
		narrowSessionLifecycleServer{
			lifecycle: &recordingSessionLifecycleClient{
				resolveTransition: func(context.Context, *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error) {
					directive, err := defaultSessionLaunchDirective(serverapi.OpenExistingSessionLaunchIntent(target))
					if err != nil {
						return nil, err
					}
					directive.GetLaunch().Preparation.Auth = sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE
					return directive, nil
				},
			},
			reauthenticate: func(context.Context, authInteractor) error {
				return authErr
			},
		},
		nil,
		"current-session",
		UITransition{Action: UIActionLogout},
	)
	if !errors.Is(err, authErr) {
		t.Fatalf("error = %v, want auth cancellation", err)
	}
	if err := protoapi.Validate(result); err == nil {
		t.Fatalf("auth failure fabricated lifecycle result %+v", result)
	}
}

func requireSessionDirectiveWireEqual(t *testing.T, got *sessionlaunchpb.SessionDirective, want *sessionlaunchpb.SessionDirective) {
	t.Helper()
	if !proto.Equal(got, want) {
		t.Fatalf("directive = %v, want %v", got, want)
	}
}

func TestInitialInputPolicyComesFromLifecycleResultNotTransitionAction(t *testing.T) {
	target := runtimeids.NewSessionID()
	want, err := defaultSessionLaunchDirective(serverapi.OpenExistingSessionLaunchIntent(target))
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolveSessionAction(
		context.Background(),
		narrowSessionLifecycleServer{lifecycle: &recordingSessionLifecycleClient{
			resolveTransition: func(context.Context, *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error) {
				return want, nil
			},
		}},
		nil,
		"current-session",
		UITransition{
			Action:          UIActionOpenSession,
			TargetSessionID: target.String(),
			InitialInput:    textutil.Value("transition input must not choose policy"),
		},
	)
	if err != nil {
		t.Fatalf("resolveSessionAction: %v", err)
	}
	preparation := result.GetLaunch().GetPreparation()
	if preparation == nil {
		t.Fatal("launch result omitted preparation")
	}
	if preparation.InputPolicy.GetRestoreStoredDraft() == nil {
		t.Fatalf("input policy = %v, want restore stored draft", preparation.InputPolicy)
	}
}

func TestSessionTransitionInitialInputPreservesOpenSessionOmission(t *testing.T) {
	target := runtimeids.NewSessionID()
	var recorded *sessionlaunchpb.SessionResolveTransitionRequest
	_, err := resolveSessionAction(
		context.Background(),
		narrowSessionLifecycleServer{lifecycle: &recordingSessionLifecycleClient{
			resolveTransition: func(_ context.Context, req *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error) {
				recorded = req
				return &sessionlaunchpb.SessionDirective{Directive: &sessionlaunchpb.SessionDirective_Stop{Stop: &emptypb.Empty{}}}, nil
			},
		}},
		nil,
		"current-session",
		UITransition{
			Action:          UIActionOpenSession,
			TargetSessionID: target.String(),
		},
	)
	if err != nil {
		t.Fatalf("resolveSessionAction: %v", err)
	}
	if recorded.Transition.InitialInput != nil {
		t.Fatalf("open Session emitted initial input = %q, want absent", *recorded.Transition.InitialInput)
	}
}

func requireAppLifecycleLaunch(t *testing.T, result *sessionlaunchpb.SessionDirective) (serverapi.SessionLaunchIntent, *sessionlaunchpb.SessionLaunchPreparation) {
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
