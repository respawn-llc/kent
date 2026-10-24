package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
)

func TestChatSettingsConnectionReplacementBoundary(t *testing.T) {
	for _, test := range []struct {
		id       string
		accepted bool
	}{
		{id: "openai", accepted: true},
		{id: "local-2_api", accepted: true},
		{id: ""},
		{id: "Bad"},
		{id: " padded "},
		{id: "bad/name"},
		{id: "\uFEFF"},
	} {
		for _, previous := range []bool{true, false} {
			value := &chatsettingspb.ConnectionReplacement{Previous: "openai", Current: "local"}
			if previous {
				value.Previous = test.id
			} else {
				value.Current = test.id
			}
			wire, err := protoapi.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			err = protoapi.Decode(wire, &chatsettingspb.ConnectionReplacement{})
			if (err == nil) != test.accepted {
				t.Errorf("id %q previous=%t: acceptance error = %v, want accepted=%t", test.id, previous, err, test.accepted)
			}
		}
	}
}
