package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/auth"
	"google.golang.org/protobuf/proto"
)

func TestConnectionSelectionIdentityAlphabet(t *testing.T) {
	for _, id := range []string{"openai", "local-2_api", "Bad", " padded ", "bad/name", "é"} {
		valid := id == "openai" || id == "local-2_api"
		if err := protoapi.Validate(&pb.ConnectionSelection{Ids: []string{id}}); (err == nil) != valid {
			t.Errorf("connection %q acceptance = %v", id, err)
		}
	}
}

func TestConnectionRequestIdentityAlphabet(t *testing.T) {
	for _, id := range []string{"openai", "local-2_api", "Bad", " padded ", "bad/name", "é"} {
		valid := id == "openai" || id == "local-2_api"
		for _, request := range []proto.Message{
			&pb.ConnectionDefinition{Id: id, Protocol: pb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT},
			&pb.ConnectionTarget{Target: &pb.ConnectionTarget_ConnectionId{ConnectionId: id}},
			&pb.ProviderSelection{ConnectionId: id},
			&pb.ConnectionReferenceEdit{ConnectionId: id, EnvironmentVariable: "API_KEY"},
			&pb.ConfigureConnectionRequest{Change: &pb.ConfigureConnectionRequest_DefaultConnectionId{DefaultConnectionId: id}},
		} {
			if err := protoapi.Validate(request); (err == nil) != valid {
				t.Errorf("%T connection %q acceptance = %v", request, id, err)
			}
		}
	}
}
