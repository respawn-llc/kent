package metadata

import (
	"context"
	"errors"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
)

func (s *Store) ResolveSessionLaunchIntent(ctx context.Context, request *sessionlaunchpb.SessionLaunchRequestIntent) (serverapi.SessionLaunchIntent, error) {
	switch intent := request.Intent.(type) {
	case *sessionlaunchpb.SessionLaunchRequestIntent_OpenExistingSessionId:
		id, err := s.ResolveSessionTarget(ctx, intent.OpenExistingSessionId)
		if err != nil {
			return serverapi.SessionLaunchIntent{}, err
		}
		return serverapi.OpenExistingSessionLaunchIntent(id), nil
	case *sessionlaunchpb.SessionLaunchRequestIntent_CreateNew:
		switch origin := intent.CreateNew.Origin.(type) {
		case *sessionlaunchpb.SessionCreateRequestOrigin_Independent:
			return serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()), nil
		case *sessionlaunchpb.SessionCreateRequestOrigin_PreviousSessionId:
			id, err := s.ResolveSessionTarget(ctx, origin.PreviousSessionId)
			if err != nil {
				return serverapi.SessionLaunchIntent{}, err
			}
			return serverapi.CreateNewSessionLaunchIntent(serverapi.PreviousSessionCreateOrigin(id)), nil
		case *sessionlaunchpb.SessionCreateRequestOrigin_ParentAgentSessionId:
			id, err := s.ResolveSessionTarget(ctx, origin.ParentAgentSessionId)
			if err != nil {
				return serverapi.SessionLaunchIntent{}, err
			}
			return serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(id)), nil
		}
	}
	return serverapi.SessionLaunchIntent{}, errors.New("Session launch request intent is required")
}
