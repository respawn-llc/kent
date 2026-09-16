package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"core/shared/textutil"

	"google.golang.org/protobuf/proto"
)

func TestRuntimeAgentSelectionRolePresence(t *testing.T) {
	for _, test := range []struct {
		name  string
		role  *string
		valid bool
	}{
		{name: "interactive base", valid: true},
		{name: "headless default", role: textutil.Value("default"), valid: true},
		{name: "named role", role: textutil.Value("worker"), valid: true},
		{name: "empty role", role: textutil.Value("")},
		{name: "blank role", role: textutil.Value(" ")},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := proto.Marshal(protoapi.SessionRuntimeAgentSelectionToProto(&serverapi.SessionRuntimeAgentSelection{
				AgentRole: test.role,
				Baseline:  serverapi.SessionRuntimeChatSettings{Supervisor: "off", Thinking: "low"},
			}))
			if err != nil {
				t.Fatal(err)
			}
			var wire sessionlaunchpb.SessionRuntimeAgentSelection
			if err := proto.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			selection, err := protoapi.SessionRuntimeAgentSelectionFromProto(&wire)
			if !test.valid {
				if err == nil {
					t.Fatal("present invalid role accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !textutil.EqualOptional(selection.AgentRole, test.role) {
				t.Fatalf("role presence changed: got %v, want %v", selection.AgentRole, test.role)
			}
		})
	}
}
