package sessionlaunch

import (
	"context"
	"errors"
	"strings"

	"core/server/launch"
	"core/server/session"
	"core/server/subagentpolicy"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type ActiveRunPreparation struct {
	Identity   runtimeids.SessionIdentity
	Descriptor session.SessionDescriptor
}

// PrepareActiveRun validates the existing target and saves only its live Thinking
// setting. It does not resolve filesystem inputs or construct a launch plan.
func (s *Service) PrepareActiveRun(ctx context.Context, id runtimeids.SessionID, caller *string, thinking string) (ActiveRunPreparation, error) {
	record, err := session.ResolvePersistedSessionRecord(ctx, s.planner.PersistedSessions, id.String())
	if err != nil {
		return ActiveRunPreparation{}, err
	}
	if _, err := s.resolveRunCaller(ctx, caller); err != nil {
		return ActiveRunPreparation{}, err
	}
	if strings.TrimSpace(thinking) != "" {
		result, err := s.MutateChatSettings(ctx, id, &chatsettingspb.MutationOperation{
			Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: thinking},
		})
		if err != nil {
			return ActiveRunPreparation{}, err
		}
		if rejected := result.GetRejected(); rejected != nil {
			return ActiveRunPreparation{}, &serverapi.RunSelectionRejectedError{Reason: rejected.Reason}
		}
	}
	descriptor, err := session.NewScopedOpenSessionDescriptor(id, s.planner.ContainerDir)
	if err != nil {
		return ActiveRunPreparation{}, err
	}
	return ActiveRunPreparation{Identity: runtimeids.SessionIdentity{ID: id, ShortID: record.ShortID}, Descriptor: descriptor}, nil
}

func (s *Service) resolveRunCaller(ctx context.Context, caller *string) (*subagentpolicy.Caller, error) {
	if caller == nil {
		return nil, nil
	}
	resolved, err := launch.ResolveSessionCaller(ctx, s.planner.CallerSessions, *caller)
	if errors.Is(err, session.ErrSessionNotFound) {
		return nil, &serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialCallerMissing}
	}
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}
