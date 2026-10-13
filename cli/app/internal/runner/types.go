package runner

import (
	"context"
	"io"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
)

type Request struct {
	SessionID                 string
	WorkspaceContextSessionID string
	AgentRole                 *string
}

type Dependencies struct {
	StartSessionServer  func(context.Context) (io.Closer, error)
	RunSessionLifecycle func(context.Context, io.Closer, *sessionlaunchpb.SessionLaunchRequestIntent, serverapi.RunPromptOverrides) error
}
