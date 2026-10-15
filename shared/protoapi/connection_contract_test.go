package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/auth"
)

func TestConnectionSelectionIdentityAlphabet(t *testing.T) {
	for _, id := range []string{"openai", "local-2_api", "Bad", " padded ", "bad/name", "é"} {
		valid := id == "openai" || id == "local-2_api"
		if err := protoapi.Validate(&pb.ConnectionSelection{Ids: []string{id}}); (err == nil) != valid {
			t.Errorf("connection %q acceptance = %v", id, err)
		}
	}
}
