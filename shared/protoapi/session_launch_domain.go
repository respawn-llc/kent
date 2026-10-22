package protoapi

import (
	configurationpb "core/shared/protoapi/gen/kent/api/configuration"
	"errors"
	"fmt"
	"strings"

	authpb "core/shared/protoapi/gen/kent/api/auth"
	invocationpb "core/shared/protoapi/gen/kent/api/invocation"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/protocol"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/toolspec"

	"google.golang.org/protobuf/types/known/emptypb"
)

func SessionPlanErrorFromProto(failure *sessionlaunchpb.SessionPlanError) error {
	switch detail := failure.Detail.(type) {
	case *sessionlaunchpb.SessionPlanError_WorkspaceNotRegistered:
		return serverapi.ErrWorkspaceNotRegistered
	case *sessionlaunchpb.SessionPlanError_SubagentLaunchDenied:
		return SubagentLaunchDeniedFromProto(detail.SubagentLaunchDenied)
	case *sessionlaunchpb.SessionPlanError_MaxDepthExceeded:
		return protocol.NewMaxDepthExceededSubagentLaunchPolicyError(
			int(detail.MaxDepthExceeded.AttemptedDepth),
			int(detail.MaxDepthExceeded.MaxDepth),
		)
	case *sessionlaunchpb.SessionPlanError_LineageCorrupt:
		repeated := runtimeids.SessionIDFromProto(detail.LineageCorrupt.RepeatedSessionId)
		visited := make([]runtimeids.SessionID, 0, len(detail.LineageCorrupt.VisitedSessionIds))
		for _, raw := range detail.LineageCorrupt.VisitedSessionIds {
			visited = append(visited, runtimeids.SessionIDFromProto(raw))
		}
		return protocol.NewLineageCorruptSubagentLaunchPolicyError(repeated, visited)
	default:
		return fmt.Errorf("session plan failure %q has unsupported detail %T", failure.Code, failure.Detail)
	}
}

func SessionPlanErrorToProto(
	err error,
	workspace *projectpb.WorkspaceNotRegisteredDetails,
) (*sessionlaunchpb.SessionPlanError, bool, error) {
	failure := &sessionlaunchpb.SessionPlanError{}
	switch {
	case errors.Is(err, serverapi.ErrServerAuthRequired):
		failure.Code = "auth_required"
		failure.Detail = &sessionlaunchpb.SessionPlanError_AuthRequired{
			AuthRequired: &authpb.AuthRequiredDetails{},
		}
	case errors.Is(err, serverapi.ErrWorkspaceNotRegistered):
		failure.Code = "workspace_not_registered"
		failure.Detail = &sessionlaunchpb.SessionPlanError_WorkspaceNotRegistered{
			WorkspaceNotRegistered: workspace,
		}
	default:
		var denied *serverapi.SubagentLaunchDeniedError
		if errors.As(err, &denied) {
			details, conversionErr := SubagentLaunchDeniedToProto(denied)
			if conversionErr != nil {
				return nil, true, conversionErr
			}
			failure.Code = "subagent_launch_denied"
			failure.Detail = &sessionlaunchpb.SessionPlanError_SubagentLaunchDenied{
				SubagentLaunchDenied: details,
			}
			break
		}
		var policy *protocol.SubagentLaunchPolicyError
		if !errors.As(err, &policy) {
			return nil, false, nil
		}
		if validationErr := policy.Validate(); validationErr != nil {
			return nil, true, validationErr
		}
		switch policy.Kind {
		case protocol.SubagentLaunchPolicyMaxDepthExceeded:
			attempted, conversionErr := Int32(*policy.AttemptedDepth, "attempted subagent depth")
			if conversionErr != nil {
				return nil, true, conversionErr
			}
			maximum, conversionErr := Int32(*policy.MaxDepth, "maximum subagent depth")
			if conversionErr != nil {
				return nil, true, conversionErr
			}
			failure.Code = "max_depth_exceeded"
			failure.Detail = &sessionlaunchpb.SessionPlanError_MaxDepthExceeded{
				MaxDepthExceeded: &sessionlaunchpb.SubagentLaunchMaxDepthExceededDetails{
					AttemptedDepth: attempted, MaxDepth: maximum,
				},
			}
		case protocol.SubagentLaunchPolicyLineageCorrupt:
			visited := make([]string, 0, len(policy.VisitedSessionIDs))
			for _, sessionID := range policy.VisitedSessionIDs {
				visited = append(visited, sessionID.String())
			}
			failure.Code = "lineage_corrupt"
			failure.Detail = &sessionlaunchpb.SessionPlanError_LineageCorrupt{
				LineageCorrupt: &sessionlaunchpb.SubagentLaunchLineageCorruptDetails{
					RepeatedSessionId: policy.RepeatedSessionID.String(),
					VisitedSessionIds: visited,
				},
			}
		default:
			return nil, true, fmt.Errorf("subagent launch policy kind %q is unsupported", policy.Kind)
		}
	}
	return failure, true, nil
}

func SubagentLaunchDeniedFromProto(details *sessionlaunchpb.SubagentLaunchDeniedDetails) error {
	kind := subagentLaunchDenialKindValues[details.Kind]
	return &serverapi.SubagentLaunchDeniedError{
		Kind:           kind,
		Target:         clonePointer(details.Target),
		AvailableRoles: append([]string(nil), details.AvailableRoles...),
	}
}

func SubagentLaunchDeniedToProto(denied *serverapi.SubagentLaunchDeniedError) (*sessionlaunchpb.SubagentLaunchDeniedDetails, error) {
	kind, err := subagentLaunchDenialKindToProto(denied.Kind)
	if err != nil {
		return nil, err
	}
	details := &sessionlaunchpb.SubagentLaunchDeniedDetails{
		Kind: kind, Target: clonePointer(denied.Target),
		AvailableRoles: append([]string(nil), denied.AvailableRoles...),
	}
	return details, nil
}

func SessionLaunchIntentToProto(intent serverapi.SessionLaunchIntent) (*sessionlaunchpb.SessionLaunchIntent, error) {
	message := &sessionlaunchpb.SessionLaunchIntent{}
	switch intent.Kind() {
	case serverapi.SessionLaunchIntentCreateNew:
		origin, _ := intent.CreateOrigin()
		converted, err := sessionCreateOriginToProto(origin)
		if err != nil {
			return nil, err
		}
		message.Intent = &sessionlaunchpb.SessionLaunchIntent_CreateNew{CreateNew: converted}
	case serverapi.SessionLaunchIntentOpenExisting:
		sessionID, _ := intent.SessionID()
		message.Intent = &sessionlaunchpb.SessionLaunchIntent_OpenExistingSessionId{
			OpenExistingSessionId: sessionID.String(),
		}
	default:
		return nil, fmt.Errorf("session launch intent kind %q is unsupported", intent.Kind())
	}
	return message, nil
}

func SessionLaunchIntentFromProto(message *sessionlaunchpb.SessionLaunchIntent) serverapi.SessionLaunchIntent {
	switch intent := message.Intent.(type) {
	case *sessionlaunchpb.SessionLaunchIntent_CreateNew:
		return serverapi.CreateNewSessionLaunchIntent(sessionCreateOriginFromProto(intent.CreateNew))
	default:
		existing := message.Intent.(*sessionlaunchpb.SessionLaunchIntent_OpenExistingSessionId)
		return serverapi.OpenExistingSessionLaunchIntent(runtimeids.SessionIDFromProto(existing.OpenExistingSessionId))
	}
}

func sessionCreateOriginToProto(origin serverapi.SessionCreateOrigin) (*sessionlaunchpb.SessionCreateOrigin, error) {
	message := &sessionlaunchpb.SessionCreateOrigin{}
	switch origin.Kind() {
	case serverapi.SessionCreateOriginIndependent:
		message.Origin = &sessionlaunchpb.SessionCreateOrigin_Independent{Independent: &emptypb.Empty{}}
	case serverapi.SessionCreateOriginNavigationOnly:
		id, _ := origin.SessionID()
		message.Origin = &sessionlaunchpb.SessionCreateOrigin_NavigationOnlySessionId{NavigationOnlySessionId: id.String()}
	case serverapi.SessionCreateOriginPreviousSession, serverapi.SessionCreateOriginParentAgent:
		sessionID, _ := origin.SessionID()
		if origin.Kind() == serverapi.SessionCreateOriginPreviousSession {
			message.Origin = &sessionlaunchpb.SessionCreateOrigin_PreviousSessionId{PreviousSessionId: sessionID.String()}
		} else {
			message.Origin = &sessionlaunchpb.SessionCreateOrigin_ParentAgentSessionId{ParentAgentSessionId: sessionID.String()}
		}
	default:
		return nil, fmt.Errorf("session create origin kind %q is unsupported", origin.Kind())
	}
	return message, nil
}

func sessionCreateOriginFromProto(message *sessionlaunchpb.SessionCreateOrigin) serverapi.SessionCreateOrigin {
	switch origin := message.Origin.(type) {
	case *sessionlaunchpb.SessionCreateOrigin_Independent:
		return serverapi.IndependentSessionCreateOrigin()
	case *sessionlaunchpb.SessionCreateOrigin_NavigationOnlySessionId:
		return serverapi.NavigationOnlySessionCreateOrigin(runtimeids.SessionIDFromProto(origin.NavigationOnlySessionId))
	case *sessionlaunchpb.SessionCreateOrigin_PreviousSessionId:
		sessionID := runtimeids.SessionIDFromProto(origin.PreviousSessionId)
		return serverapi.PreviousSessionCreateOrigin(sessionID)
	default:
		parent := message.Origin.(*sessionlaunchpb.SessionCreateOrigin_ParentAgentSessionId)
		return serverapi.ParentAgentSessionCreateOrigin(runtimeids.SessionIDFromProto(parent.ParentAgentSessionId))
	}
}

func RunPromptOverridesToProto(overrides serverapi.RunPromptOverrides) (*invocationpb.RunPromptOverrides, error) {
	if err := overrides.ValidateAgentRoleOverride(); err != nil {
		return nil, err
	}
	message := &invocationpb.RunPromptOverrides{AgentRole: clonePointer(overrides.AgentRole)}
	setOptionalNonblank(&message.Model, overrides.Model)
	setOptionalNonblank(&message.ThinkingLevel, overrides.ThinkingLevel)
	setOptionalNonblank(&message.Theme, overrides.Theme)
	setOptionalNonblank(&message.Tools, overrides.Tools)
	if overrides.ModelTimeoutSeconds != 0 {
		value, err := Int32(overrides.ModelTimeoutSeconds, "model timeout seconds")
		if err != nil {
			return nil, err
		}
		message.ModelTimeoutSeconds = &value
	}
	return message, nil
}

func RunPromptOverridesFromProto(message *invocationpb.RunPromptOverrides) (serverapi.RunPromptOverrides, error) {
	overrides := serverapi.RunPromptOverrides{
		AgentRole:     clonePointer(message.AgentRole),
		Model:         dereference(message.Model),
		ThinkingLevel: dereference(message.ThinkingLevel),
		Theme:         dereference(message.Theme),
		Tools:         dereference(message.Tools),
	}
	if message.ModelTimeoutSeconds != nil {
		overrides.ModelTimeoutSeconds = int(*message.ModelTimeoutSeconds)
	}
	return overrides, overrides.ValidateAgentRoleOverride()
}

func setOptionalNonblank(target **string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	copied := value
	*target = &copied
}

func SessionRuntimeAgentSelectionFromProto(
	message *sessionlaunchpb.SessionRuntimeAgentSelection,
) (*serverapi.SessionRuntimeAgentSelection, error) {
	if message == nil {
		return nil, nil
	}
	return &serverapi.SessionRuntimeAgentSelection{
		AgentRole: clonePointer(message.AgentRole),
		Baseline: serverapi.SessionRuntimeChatSettings{
			Supervisor:     message.Baseline.Supervisor,
			Thinking:       message.Baseline.Thinking,
			Fast:           message.Baseline.Fast,
			Questions:      message.Baseline.Questions,
			AutoCompaction: message.Baseline.AutoCompaction,
		},
	}, nil
}

func SessionRuntimeAgentSelectionToProto(selection *serverapi.SessionRuntimeAgentSelection) *sessionlaunchpb.SessionRuntimeAgentSelection {
	if selection == nil {
		return nil
	}
	return &sessionlaunchpb.SessionRuntimeAgentSelection{
		AgentRole: clonePointer(selection.AgentRole),
		Baseline: &sessionlaunchpb.SessionRuntimeChatSettings{
			Supervisor:     selection.Baseline.Supervisor,
			Thinking:       selection.Baseline.Thinking,
			Fast:           selection.Baseline.Fast,
			Questions:      selection.Baseline.Questions,
			AutoCompaction: selection.Baseline.AutoCompaction,
		},
	}
}

func SessionToolIDToProto(toolID toolspec.ID) (configurationpb.ToolID, error) {
	return enumToProto(sessionToolIDValues, toolID, "tool id")
}

func SessionToolIDFromProto(toolID configurationpb.ToolID) toolspec.ID {
	return sessionToolIDValues[toolID]
}

var sessionToolIDValues = map[configurationpb.ToolID]toolspec.ID{
	configurationpb.ToolID_TOOL_ID_EXEC_COMMAND:    toolspec.ToolExecCommand,
	configurationpb.ToolID_TOOL_ID_WRITE_STDIN:     toolspec.ToolWriteStdin,
	configurationpb.ToolID_TOOL_ID_VIEW_IMAGE:      toolspec.ToolViewImage,
	configurationpb.ToolID_TOOL_ID_PATCH:           toolspec.ToolPatch,
	configurationpb.ToolID_TOOL_ID_EDIT:            toolspec.ToolEdit,
	configurationpb.ToolID_TOOL_ID_ASK_QUESTION:    toolspec.ToolAskQuestion,
	configurationpb.ToolID_TOOL_ID_COMPLETE_NODE:   toolspec.ToolCompleteNode,
	configurationpb.ToolID_TOOL_ID_TRIGGER_HANDOFF: toolspec.ToolTriggerHandoff,
	configurationpb.ToolID_TOOL_ID_WEB_SEARCH:      toolspec.ToolWebSearch,
}

func subagentLaunchDenialKindToProto(kind serverapi.SubagentLaunchDenialKind) (sessionlaunchpb.SubagentLaunchDenialKind, error) {
	return enumToProto(subagentLaunchDenialKindValues, kind, "subagent launch denial kind")
}

var subagentLaunchDenialKindValues = map[sessionlaunchpb.SubagentLaunchDenialKind]serverapi.SubagentLaunchDenialKind{
	sessionlaunchpb.SubagentLaunchDenialKind_SUBAGENT_LAUNCH_DENIAL_KIND_INVALID_TARGET: serverapi.SubagentLaunchDenialInvalidTarget,
	sessionlaunchpb.SubagentLaunchDenialKind_SUBAGENT_LAUNCH_DENIAL_KIND_TARGET_MISSING: serverapi.SubagentLaunchDenialTargetMissing,
	sessionlaunchpb.SubagentLaunchDenialKind_SUBAGENT_LAUNCH_DENIAL_KIND_NOT_CALLABLE:   serverapi.SubagentLaunchDenialNotCallable,
	sessionlaunchpb.SubagentLaunchDenialKind_SUBAGENT_LAUNCH_DENIAL_KIND_CALLER_MISSING: serverapi.SubagentLaunchDenialCallerMissing,
	sessionlaunchpb.SubagentLaunchDenialKind_SUBAGENT_LAUNCH_DENIAL_KIND_PARENT_MISSING: serverapi.SubagentLaunchDenialParentMissing,
}
