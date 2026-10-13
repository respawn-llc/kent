package transport

import (
	"errors"
	"net/http/httptest"
	"testing"

	"core/shared/protoapi"
	settingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

func settingsFailureSessionID(t *testing.T, details *settingspb.SessionNotFoundDetails) *runtimeids.SessionID {
	t.Helper()
	if details == nil {
		t.Fatal("missing Session-not-found details")
	}
	err := protoapi.SessionTargetContextErrorFromProto(details.SessionTarget, sessioncontract.ErrSessionNotFound)
	var target *serverapi.SessionTargetError
	if !errors.As(err, &target) {
		t.Fatalf("invalid captured target: %v", err)
	}
	return target.SessionID()
}

func TestGatewayDescriptorNewChatSettingsCatalog(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	server := httptest.NewServer(fixture.gateway.Handler())
	t.Cleanup(server.Close)
	conn := dialGateway(t, server)
	t.Cleanup(func() { _ = conn.Close() })
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach-settings-project", &connectionpb.AttachProjectRequest{ProjectId: fixture.bindingA.ProjectID})
	method := settingspb.File_kent_api_chat_settings_chat_settings_proto.Services().
		ByName("ChatSettingsService").Methods().ByName("Read")
	result := &settingspb.ReadResult{}
	callGatewayDescriptor(t, conn, "new-chat-settings", method, &settingspb.ReadRequest{
		Target: &settingspb.ReadRequest_NewChat{NewChat: &settingspb.NewChatTarget{
			ProjectId: fixture.bindingA.ProjectID, WorkspaceId: fixture.bindingA.WorkspaceID,
		}},
	}, result)
	catalog := result.GetSuccess().GetNewChat()
	if catalog == nil || len(catalog.Choices) == 0 || catalog.InitialSettings == nil {
		t.Fatalf("New Chat catalog = %v", result)
	}
	for _, choice := range catalog.Choices {
		if choice.Baseline.AgentRole != choice.Agent.Role ||
			choice.Baseline.Supervisor != choice.Supervisor.Value ||
			choice.Baseline.GetQuestionsEnabled() != choice.Questions.Enabled ||
			choice.Baseline.GetAutoCompactionEnabled() != choice.AutoCompaction.Stored {
			t.Fatalf("incomplete Agent baseline: %v", choice)
		}
	}
}

func TestGatewayDescriptorSettingsMissingSessionFailure(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	server := httptest.NewServer(fixture.gateway.Handler())
	t.Cleanup(server.Close)
	conn := dialGateway(t, server)
	t.Cleanup(func() { _ = conn.Close() })
	handshakeGateway(t, conn)
	method := settingspb.File_kent_api_chat_settings_chat_settings_proto.Services().
		ByName("ChatSettingsService").Methods().ByName("Read")
	id := runtimeids.NewSessionID().String()
	for _, target := range []*sharedpb.SessionSelector{
		{Value: &sharedpb.SessionSelector_SessionId{SessionId: id}},
		{Value: &sharedpb.SessionSelector_ShortId{ShortId: runtimeids.MaxSessionShortID}},
	} {
		result := &settingspb.ReadResult{}
		callGatewayDescriptor(t, conn, "missing-settings", method, &settingspb.ReadRequest{
			Target: &settingspb.ReadRequest_Session{Session: &settingspb.SessionTarget{SessionId: target}},
		}, result)
		details := result.GetError().GetSessionNotFound()
		known := settingsFailureSessionID(t, details)
		if (target.GetShortId() != 0 && known != nil) ||
			(target.GetShortId() == 0 && (known == nil || known.String() != id)) {
			t.Fatalf("missing Session failure = %v", result)
		}
	}
}

func TestGatewayDescriptorSettingsChecksSessionWithoutProviderCredentials(t *testing.T) {
	appCore, server, _ := newGatewayTestServerWithAuth(t, false)
	t.Cleanup(func() { _ = appCore.Close() })
	t.Cleanup(server.Close)
	conn := dialGateway(t, server)
	t.Cleanup(func() { _ = conn.Close() })
	handshakeGateway(t, conn)
	session := &settingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: runtimeids.NewSessionID().String()}}}
	service := settingspb.File_kent_api_chat_settings_chat_settings_proto.Services().ByName("ChatSettingsService")
	read := &settingspb.ReadResult{}
	callGatewayDescriptor(t, conn, "unauthenticated-read", service.Methods().ByName("Read"),
		&settingspb.ReadRequest{Target: &settingspb.ReadRequest_Session{Session: session}}, read)
	if id := settingsFailureSessionID(t, read.GetError().GetSessionNotFound()); id == nil || id.String() != session.SessionId.GetSessionId() {
		t.Fatalf("read Session failure = %v", read)
	}
	mutation := &settingspb.MutationResponse{}
	callGatewayDescriptor(t, conn, "unauthenticated-mutation", service.Methods().ByName("Mutate"),
		&settingspb.MutationRequest{
			Session: session, Operation: &settingspb.MutationOperation{Operation: &settingspb.MutationOperation_QuestionsEnabled{QuestionsEnabled: false}},
		}, mutation)
	if id := settingsFailureSessionID(t, mutation.GetError().GetSessionNotFound()); id == nil || id.String() != session.SessionId.GetSessionId() {
		t.Fatalf("mutation Session failure = %v", mutation)
	}
}

func TestGatewayDescriptorSessionSettingsMutations(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	server := httptest.NewServer(fixture.gateway.Handler())
	t.Cleanup(server.Close)
	conn := dialGateway(t, server)
	t.Cleanup(func() { _ = conn.Close() })
	handshakeGateway(t, conn)
	method := settingspb.File_kent_api_chat_settings_chat_settings_proto.Services().
		ByName("ChatSettingsService").Methods().ByName("Mutate")
	for _, operation := range []*settingspb.MutationOperation{
		{Operation: &settingspb.MutationOperation_AgentRole{AgentRole: "missing-agent"}},
		{Operation: &settingspb.MutationOperation_Supervisor{Supervisor: settingspb.SupervisorValue_SUPERVISOR_VALUE_OFF}},
		{Operation: &settingspb.MutationOperation_Thinking{Thinking: " high "}},
		{Operation: &settingspb.MutationOperation_FastEnabled{FastEnabled: false}},
		{Operation: &settingspb.MutationOperation_QuestionsEnabled{QuestionsEnabled: false}},
		{Operation: &settingspb.MutationOperation_AutoCompactionEnabled{AutoCompactionEnabled: false}},
	} {
		result := &settingspb.MutationResponse{}
		callGatewayDescriptor(t, conn, "mutate-settings", method, &settingspb.MutationRequest{
			Session: &settingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: fixture.ownSessionID}}}, Operation: operation,
		}, result)
		success := result.GetSuccess()
		if success == nil || success.Session.SessionId != fixture.ownSessionID || success.Settings == nil || success.Context == nil {
			t.Fatalf("mutation did not return authoritative Session projections: %v", result)
		}
		if err := protoapi.Validate(success); err != nil {
			t.Fatal(err)
		}
		if operation.GetAgentRole() == "missing-agent" && success.Result.GetRejected() == nil {
			t.Fatalf("missing Agent was not rejected: %v", success.Result)
		}
		if _, ok := operation.Operation.(*settingspb.MutationOperation_QuestionsEnabled); ok {
			if success.Result.GetApplied() == nil || success.Settings.Questions.Enabled {
				t.Fatalf("Questions mutation not applied: %v", success)
			}
		}
	}
}
