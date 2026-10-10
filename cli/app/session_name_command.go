package app

import (
	"strings"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"google.golang.org/protobuf/types/known/emptypb"
)

func sessionNameCommandMutation(arguments string) *runtimepb.SessionNameMutation {
	name := strings.TrimSpace(arguments)
	if name == "" {
		return &runtimepb.SessionNameMutation{Action: &runtimepb.SessionNameMutation_Clear{Clear: &emptypb.Empty{}}}
	}
	return &runtimepb.SessionNameMutation{Action: &runtimepb.SessionNameMutation_Set{Set: name}}
}
