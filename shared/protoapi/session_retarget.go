package protoapi

import (
	"fmt"

	sessionretargetpb "core/shared/protoapi/gen/kent/api/session_retarget"
	"core/shared/serverapi"
)

func SessionRetargetErrorToProto(failure *serverapi.SessionRetargetError) (*sessionretargetpb.SessionRetargetWorkspaceError, error) {
	facts := &sessionretargetpb.SessionRetargetFacts{
		SessionId: failure.SessionID,
		SourceProject: &sessionretargetpb.ProjectReference{
			Id: failure.SourceProject.ID, Name: failure.SourceProject.Name,
		},
		TargetRoot:      failure.TargetRoot,
		WorkflowTaskIds: failure.WorkflowTaskIDs,
	}
	for _, project := range failure.CandidateProjects {
		facts.CandidateProjects = append(facts.CandidateProjects, &sessionretargetpb.ProjectReference{Id: project.ID, Name: project.Name})
	}
	result := &sessionretargetpb.SessionRetargetWorkspaceError{Code: string(failure.Reason)}
	switch failure.Reason {
	case serverapi.SessionRetargetTargetProjectRequired:
		result.Detail = &sessionretargetpb.SessionRetargetWorkspaceError_TargetProjectRequired{
			TargetProjectRequired: &sessionretargetpb.SessionRetargetTargetProjectRequiredDetails{Facts: facts},
		}
	case serverapi.SessionRetargetTargetProjectConflict:
		result.Detail = &sessionretargetpb.SessionRetargetWorkspaceError_TargetProjectConflict{
			TargetProjectConflict: &sessionretargetpb.SessionRetargetTargetProjectConflictDetails{Facts: facts},
		}
	case serverapi.SessionRetargetWorkflowOwned:
		result.Detail = &sessionretargetpb.SessionRetargetWorkspaceError_WorkflowOwned{
			WorkflowOwned: &sessionretargetpb.SessionRetargetWorkflowOwnedDetails{Facts: facts},
		}
	case serverapi.SessionRetargetBackgroundProcess:
		result.Detail = &sessionretargetpb.SessionRetargetWorkspaceError_BackgroundProcessActive{
			BackgroundProcessActive: &sessionretargetpb.SessionRetargetBackgroundProcessActiveDetails{Facts: facts},
		}
	case serverapi.SessionRetargetRuntimeActive:
		result.Detail = &sessionretargetpb.SessionRetargetWorkspaceError_RuntimeActive{
			RuntimeActive: &sessionretargetpb.SessionRetargetRuntimeActiveDetails{Facts: facts},
		}
	default:
		return nil, fmt.Errorf("invalid Session retarget reason %q", failure.Reason)
	}
	return result, nil
}

func SessionRetargetErrorFromProto(failure *sessionretargetpb.SessionRetargetWorkspaceError) error {
	var facts *sessionretargetpb.SessionRetargetFacts
	var reason serverapi.SessionRetargetErrorReason
	switch detail := failure.Detail.(type) {
	case *sessionretargetpb.SessionRetargetWorkspaceError_TargetProjectRequired:
		facts, reason = detail.TargetProjectRequired.Facts, serverapi.SessionRetargetTargetProjectRequired
	case *sessionretargetpb.SessionRetargetWorkspaceError_TargetProjectConflict:
		facts, reason = detail.TargetProjectConflict.Facts, serverapi.SessionRetargetTargetProjectConflict
	case *sessionretargetpb.SessionRetargetWorkspaceError_WorkflowOwned:
		facts, reason = detail.WorkflowOwned.Facts, serverapi.SessionRetargetWorkflowOwned
	case *sessionretargetpb.SessionRetargetWorkspaceError_BackgroundProcessActive:
		facts, reason = detail.BackgroundProcessActive.Facts, serverapi.SessionRetargetBackgroundProcess
	case *sessionretargetpb.SessionRetargetWorkspaceError_RuntimeActive:
		facts, reason = detail.RuntimeActive.Facts, serverapi.SessionRetargetRuntimeActive
	default:
		return fmt.Errorf("Session retarget failed with code %q", failure.Code)
	}
	result := &serverapi.SessionRetargetError{
		Reason: reason, SessionID: facts.SessionId, TargetRoot: facts.TargetRoot,
		SourceProject:   serverapi.ProjectReference{ID: facts.SourceProject.Id, Name: facts.SourceProject.Name},
		WorkflowTaskIDs: facts.WorkflowTaskIds,
	}
	for _, project := range facts.CandidateProjects {
		result.CandidateProjects = append(result.CandidateProjects, serverapi.ProjectReference{ID: project.Id, Name: project.Name})
	}
	return result
}
