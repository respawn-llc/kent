package testsetup

import (
	"context"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
)

func SessionNavigationBinding(context.Context, string) (*sessionlaunchpb.SessionNavigationBinding, error) {
	return &sessionlaunchpb.SessionNavigationBinding{ProjectId: "project-1", WorkspaceId: "workspace-1"}, nil
}
