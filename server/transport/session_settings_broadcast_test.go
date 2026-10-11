package transport

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"core/shared/apicontract"
	remoteclient "core/shared/client"
	settingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"core/shared/textutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestSessionSettingsBroadcastDeliversDormantAndLiveCommitsToTwoClients(t *testing.T) {
	for _, state := range []struct {
		name     string
		activate bool
		activity runtimepb.ActivityState
	}{
		{name: "dormant", activity: runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE},
		{name: "live", activate: true, activity: runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE},
	} {
		t.Run(state.name, func(t *testing.T) {
			testSessionSettingsBroadcast(t, state.activate, state.activity)
		})
	}
}

func testSessionSettingsBroadcast(t *testing.T, activate bool, activity runtimepb.ActivityState) {
	t.Helper()
	fixture := newRoutePolicyFixture(t)
	if activate {
		activateGatewayController(t, fixture.appCore, fixture.ownSessionID)
	}
	server := httptest.NewServer(fixture.gateway.Handler())
	t.Cleanup(server.Close)
	var clients []*remoteclient.Remote
	var subscriptions []apicontract.ChatSettingsSubscription
	for range 2 {
		client, err := remoteclient.DialRemoteURLForSession(t.Context(), "ws"+server.URL[len("http"):], fixture.ownSessionID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		subscription, err := client.SubscribeChatSettings(t.Context(), &settingspb.SubscribeRequest{SessionId: fixture.ownSessionID})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = subscription.Close() })
		clients = append(clients, client)
		subscriptions = append(subscriptions, subscription)
	}
	if err := clients[0].SetSessionName(t.Context(), &runtimepb.SetSessionNameRequest{
		SessionId: fixture.ownSessionID,
		Mutation:  &runtimepb.SessionNameMutation{Action: &runtimepb.SessionNameMutation_Set{Set: "  Review  notes  "}},
	}); err != nil {
		t.Fatal(err)
	}
	assertDeliveredSessionSettings(t, clients[0], subscriptions, fixture.ownSessionID, textutil.Value("Review  notes"))
	_, err := clients[0].MutateChatSettings(t.Context(), &settingspb.MutationRequest{
		Session: &settingspb.SessionTarget{SessionId: fixture.ownSessionID},
		Operation: &settingspb.MutationOperation{
			Operation: &settingspb.MutationOperation_QuestionsEnabled{QuestionsEnabled: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertDeliveredSessionSettings(t, clients[0], subscriptions, fixture.ownSessionID, textutil.Value("Review  notes"))
	if err := clients[0].SetSessionName(t.Context(), &runtimepb.SetSessionNameRequest{
		SessionId: fixture.ownSessionID,
		Mutation:  &runtimepb.SessionNameMutation{Action: &runtimepb.SessionNameMutation_Clear{Clear: &emptypb.Empty{}}},
	}); err != nil {
		t.Fatal(err)
	}
	assertDeliveredSessionSettings(t, clients[0], subscriptions, fixture.ownSessionID, nil)
	view, err := clients[1].GetSessionMainView(t.Context(), &sessionpb.MainViewRequest{SessionId: fixture.ownSessionID})
	if err != nil {
		t.Fatal(err)
	}
	if view.MainView.Activity.State != activity || view.MainView.Session.SessionName != nil {
		t.Fatalf("settings mutation or observation changed runtime lifetime or lost Clear: %v", view.MainView)
	}
}

func assertDeliveredSessionSettings(t *testing.T, client *remoteclient.Remote, subscriptions []apicontract.ChatSettingsSubscription, sessionID string, name *string) {
	t.Helper()
	expected, err := client.ReadChatSettings(t.Context(), &settingspb.ReadRequest{
		Target: &settingspb.ReadRequest_Session{Session: &settingspb.SessionTarget{SessionId: sessionID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, subscription := range subscriptions {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		snapshot, err := subscription.Next(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if !textutil.EqualOptional(snapshot.SessionName, name) ||
			!proto.Equal(snapshot.Settings, expected.GetSession().Settings) ||
			!proto.Equal(snapshot.Session, expected.GetSession().Session) {
			t.Fatalf("broadcast did not deliver the authoritative snapshot: %v", snapshot)
		}
	}
}
