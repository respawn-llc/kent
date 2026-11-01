package transport

import (
	"errors"
	"reflect"
	"testing"

	"core/server/sessionstore"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	settingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestGoalFailureEncodesCapturedTargetWithoutDiscovery(t *testing.T) {
	id := runtimeids.NewSessionID()
	number, err := runtimeids.NewSessionShortID(73)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []serverapi.SessionTarget{
		serverapi.UnresolvedSessionTarget{},
		serverapi.CanonicalSessionTarget{ID: id},
		serverapi.ProjectedSessionTarget{Identity: runtimeids.SessionIdentity{ID: id}},
		serverapi.ProjectedSessionTarget{Identity: runtimeids.SessionIdentity{ID: id, ShortID: &number}},
	} {
		original := serverapi.WithSessionTarget(target, serverapi.ErrRuntimeUnavailable)
		request := &runtimepb.GoalSetRequest{Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
			Session: &chatpb.ExistingSessionRequestTarget{SessionId: protoapi.SessionSelectorToProto(number)},
		}}}
		wire := binaryGoalSetFailure(nil, nil, request, original).(*runtimepb.GoalSetError)
		var decoded *serverapi.SessionTargetError
		err := protoapi.RuntimeUnavailableFromProto(wire.GetRuntimeUnavailable())
		if !errors.Is(err, serverapi.ErrRuntimeUnavailable) || !errors.As(err, &decoded) || !reflect.DeepEqual(decoded.Target, target) {
			t.Fatalf("Goal failure lost cause or captured facts: %v", err)
		}
	}
}

func TestChatFailureEncodesCapturedTargetWithoutDiscovery(t *testing.T) {
	id := runtimeids.NewSessionID()
	number, err := runtimeids.NewSessionShortID(73)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []serverapi.SessionTarget{
		serverapi.UnresolvedSessionTarget{},
		serverapi.CanonicalSessionTarget{ID: id},
		serverapi.ProjectedSessionTarget{Identity: runtimeids.SessionIdentity{ID: id}},
		serverapi.ProjectedSessionTarget{Identity: runtimeids.SessionIdentity{ID: id, ShortID: &number}},
	} {
		original := serverapi.WithSessionTarget(target, session.ErrSessionNotFound)
		chatRequest := &chatpb.SteerRequest{Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
			Session: &chatpb.ExistingSessionRequestTarget{SessionId: protoapi.SessionSelectorToProto(number)},
		}}}
		settingsRequest := &settingspb.ReadRequest{Target: &settingspb.ReadRequest_Session{
			Session: &settingspb.SessionTarget{SessionId: protoapi.SessionSelectorToProto(number)},
		}}
		for _, details := range []*settingspb.SessionNotFoundDetails{
			binaryChatFailure(nil, nil, chatRequest, original).(*settingspb.SessionNotFoundDetails),
			binaryChatSettingsFailure(nil, nil, settingsRequest, original).(*settingspb.SessionNotFoundDetails),
		} {
			err := protoapi.ChatSettingsErrorFromProto(&settingspb.ReadError{Code: "session_not_found", Detail: &settingspb.ReadError_SessionNotFound{SessionNotFound: details}})
			var decoded *serverapi.SessionTargetError
			if !errors.Is(err, session.ErrSessionNotFound) || !errors.As(err, &decoded) || !reflect.DeepEqual(decoded.Target, target) {
				t.Fatalf("Chat failure lost original cause or target: %v", err)
			}
		}
	}
}
