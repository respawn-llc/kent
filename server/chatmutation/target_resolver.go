package chatmutation

import (
	"context"
	"errors"
	"fmt"

	"core/server/launch"
	"core/server/metadata"
	"core/server/promptcommands"
	"core/server/runtimecontrol"
	"core/server/session"
	"core/server/sessionlaunch"
	"core/shared/clientui"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
)

type SessionCreationService interface {
	PlanSession(context.Context, *sessionlaunchpb.SessionPlanRequest) (*sessionlaunchpb.SessionPlanSuccess, error)
}

type SessionNamingService interface {
	SetSessionName(context.Context, *runtimepb.SetSessionNameRequest) error
}

type WorktreeEnteringService interface {
	EnterWorktree(context.Context, *worktreepb.EnterRequest) (*worktreepb.ScheduledAcknowledgement, error)
}

type SessionLaunchServiceResolver func(
	context.Context,
	string,
	string,
) (SessionCreationService, error)

type TargetResolutionRequest struct {
	Target       *chatpb.ChatTarget
	InitialDraft *string
}

type ResolvedTarget struct {
	SessionID   runtimeids.SessionID
	ShortID     *runtimeids.SessionShortID
	Created     bool
	Preparation *serverapi.SessionRuntimePreparationOptions
}

type TargetResolver struct {
	metadata          *metadata.Store
	persistedSessions session.PersistedSessionResolver
	sessionLaunch     SessionLaunchServiceResolver
	sourceLaunch      SessionRuntimePlannerResolver
	activity          runtimecontrol.RuntimeActivityResolver
	names             SessionNamingService
	worktrees         WorktreeEnteringService
}

func NewTargetResolver(
	persistedSessions session.PersistedSessionResolver,
	sessionLaunch SessionLaunchServiceResolver,
	sourceLaunch SessionRuntimePlannerResolver,
	activity runtimecontrol.RuntimeActivityResolver,
	store *metadata.Store,
	names SessionNamingService,
	worktrees WorktreeEnteringService,
) *TargetResolver {
	return &TargetResolver{
		metadata:          store,
		persistedSessions: persistedSessions,
		sessionLaunch:     sessionLaunch,
		sourceLaunch:      sourceLaunch,
		activity:          activity,
		names:             names,
		worktrees:         worktrees,
	}
}

func (r *TargetResolver) SelectPlacement(ctx context.Context, target ResolvedTarget, placement promptcommands.Placement) (ResolvedTarget, error) {
	if placement == promptcommands.PlacementCurrent || target.Created {
		return target, nil
	}
	if placement != promptcommands.PlacementFresh {
		return target, errors.New("invalid prompt command placement")
	}
	record, err := session.ResolvePersistedSessionRecord(ctx, r.persistedSessions, target.SessionID.String())
	if err != nil {
		return target, err
	}
	if r.activity == nil {
		return target, errors.New("Runtime activity resolver is required")
	}
	activity, err := r.activity.RuntimeReadModelFeedSnapshot(ctx, target.SessionID.String())
	if err != nil {
		return target, err
	}
	if !record.Meta.ConversationEstablished && !protoapi.RuntimeActivityActiveForControl(activity.GetActivity()) {
		return target, nil
	}
	if r.sourceLaunch == nil {
		return target, errors.New("persisted Session launch resolver is required")
	}
	service, err := r.sourceLaunch(ctx, target.SessionID)
	if err != nil {
		return target, err
	}
	planned, err := service.PlanLaunchSession(ctx, sessionlaunch.PlanRequest{
		Mode:   launch.ModeInteractive,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.PreviousSessionCreateOrigin(target.SessionID)),
	})
	if err != nil {
		return target, err
	}
	return ResolvedTarget{
		SessionID: planned.Plan.Descriptor.SessionID(), ShortID: planned.Identity.ShortID, Created: true,
		Preparation: target.Preparation,
	}, nil
}

func (r *TargetResolver) Resolve(
	ctx context.Context,
	request TargetResolutionRequest,
) (resolved ResolvedTarget, err error) {
	var captured serverapi.SessionTarget
	defer func() {
		if captured != nil {
			err = serverapi.WithSessionTarget(captured, err)
		}
	}()
	if err := protoapi.Validate(request.Target); err != nil {
		return ResolvedTarget{}, fmt.Errorf("Chat target: %w", err)
	}
	if r == nil {
		return ResolvedTarget{}, errors.New("Chat target resolver is required")
	}
	if sessionTarget := request.Target.GetSession(); sessionTarget != nil {
		captured = serverapi.UnresolvedSessionTarget{}
		sessionID, err := r.metadata.ResolveSessionTarget(ctx, sessionTarget.SessionId)
		if err != nil {
			return ResolvedTarget{}, err
		}
		captured = serverapi.CanonicalSessionTarget{ID: sessionID}
		record, err := session.ResolvePersistedSessionRecord(
			ctx,
			r.persistedSessions,
			sessionID.String(),
		)
		if err != nil {
			return ResolvedTarget{}, err
		}
		target := ResolvedTarget{SessionID: sessionID, ShortID: record.ShortID}
		if sessionTarget.Preparation != nil {
			options, err := protoapi.SessionRuntimePreparationFromProto(sessionTarget.Preparation)
			if err != nil {
				return target, err
			}
			target.Preparation = &options
		}
		return target, nil
	}
	newChat := request.Target.GetNewChat()
	if newChat == nil {
		return ResolvedTarget{}, errors.New("Chat target selection is required")
	}
	if r.sessionLaunch == nil {
		return ResolvedTarget{}, errors.New("Session launch service resolver is required")
	}
	service, err := r.sessionLaunch(ctx, newChat.ProjectId, newChat.WorkspaceId)
	if err != nil {
		return ResolvedTarget{}, err
	}
	if service == nil {
		return ResolvedTarget{}, errors.New("Session launch service is required")
	}
	intent, err := protoapi.SessionLaunchRequestIntentToProto(
		serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	)
	if err != nil {
		return ResolvedTarget{}, err
	}
	if newChat.Origin != nil {
		intent = &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_CreateNew{CreateNew: newChat.Origin}}
	}
	settings := proto.Clone(newChat.InitialSettings).(*chatsettingspb.InitialChatSettings)
	response, err := service.PlanSession(ctx, &sessionlaunchpb.SessionPlanRequest{
		Mode:                sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
		Intent:              intent,
		InitialChatSettings: settings,
		InitialInputDraft:   request.InitialDraft,
		Overrides:           newChat.Invocation,
	})
	if err != nil {
		return ResolvedTarget{}, err
	}
	if response == nil || response.Plan == nil {
		return ResolvedTarget{}, errors.New("ordinary Session creation result is required")
	}
	sessionID, err := runtimeids.ParseSessionID(response.Plan.SessionId)
	if err != nil {
		return ResolvedTarget{}, fmt.Errorf("ordinary Session creation result: %w", err)
	}
	captured = serverapi.CanonicalSessionTarget{ID: sessionID}
	resolved = ResolvedTarget{SessionID: sessionID, Created: true}
	shortID, err := protoapi.SessionShortIDFromProto(response.Plan.ShortId)
	if err != nil {
		return resolved, err
	}
	resolved.ShortID = shortID
	if newChat.Name != nil {
		if r.names == nil {
			return resolved, errors.New("Session naming service is required")
		}
		if err := r.names.SetSessionName(ctx, &runtimepb.SetSessionNameRequest{
			SessionId: sessionID.String(),
			Mutation:  &runtimepb.SessionNameMutation{Action: &runtimepb.SessionNameMutation_Set{Set: *newChat.Name}},
		}); err != nil {
			return resolved, err
		}
	}
	if newChat.WorktreeSelector != nil {
		if r.worktrees == nil {
			return resolved, errors.New("Worktree management service is required")
		}
		binding, err := r.metadata.LookupWorkspaceBindingByID(ctx, newChat.WorkspaceId)
		if err != nil {
			return resolved, err
		}
		if _, err := r.worktrees.EnterWorktree(ctx, &worktreepb.EnterRequest{
			OperationId: clientui.NewWorktreeTransitionID().String(),
			SessionId:   protoapi.SessionSelectorToProto(sessionID), Selector: *newChat.WorktreeSelector,
			TargetWorkspace: &worktreepb.TransitionWorkspace{
				WorkspaceId: binding.WorkspaceID, WorkspaceRoot: binding.CanonicalRoot,
			},
		}); err != nil {
			return resolved, err
		}
	}
	resolved.Preparation, err = preparationFromPlan(response.Plan)
	return resolved, err
}

func preparationFromPlan(plan *sessionlaunchpb.SessionPlan) (*serverapi.SessionRuntimePreparationOptions, error) {
	options, err := protoapi.SessionRuntimePreparationFromProto(&sessionlaunchpb.SessionRuntimePreparationOptions{
		ActiveSettings: plan.ActiveSettings, EnabledToolIds: plan.EnabledToolIds,
		QuestionsEnabled: &plan.QuestionsEnabled, AutoCompactionEnabled: &plan.AutoCompactionEnabled,
		ThinkingOverrideExplicit: plan.ThinkingOverrideExplicit, Source: plan.Source,
		AgentSelection: plan.ActivationAgentSelection, ExplicitToolSelection: plan.ExplicitToolSelection,
	})
	if err != nil {
		return nil, err
	}
	return &options, nil
}
