package protoapi

import (
	"fmt"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
)

func SessionRetargetErrorToProto(failure *serverapi.SessionRetargetError) (*sessionlaunchpb.SessionRetargetWorkspaceError, error) {
	facts := &sessionlaunchpb.SessionRetargetFacts{
		SessionId: failure.SessionID,
		SourceProject: &sessionlaunchpb.ProjectReference{
			Id: failure.SourceProject.ID, Name: failure.SourceProject.Name,
		},
		TargetRoot:      failure.TargetRoot,
		WorkflowTaskIds: failure.WorkflowTaskIDs,
	}
	for _, project := range failure.CandidateProjects {
		facts.CandidateProjects = append(facts.CandidateProjects, &sessionlaunchpb.ProjectReference{Id: project.ID, Name: project.Name})
	}
	result := &sessionlaunchpb.SessionRetargetWorkspaceError{Code: string(failure.Reason)}
	switch failure.Reason {
	case serverapi.SessionRetargetTargetProjectRequired:
		result.Detail = &sessionlaunchpb.SessionRetargetWorkspaceError_TargetProjectRequired{
			TargetProjectRequired: &sessionlaunchpb.SessionRetargetTargetProjectRequiredDetails{Facts: facts},
		}
	case serverapi.SessionRetargetTargetProjectConflict:
		result.Detail = &sessionlaunchpb.SessionRetargetWorkspaceError_TargetProjectConflict{
			TargetProjectConflict: &sessionlaunchpb.SessionRetargetTargetProjectConflictDetails{Facts: facts},
		}
	case serverapi.SessionRetargetWorkflowOwned:
		result.Detail = &sessionlaunchpb.SessionRetargetWorkspaceError_WorkflowOwned{
			WorkflowOwned: &sessionlaunchpb.SessionRetargetWorkflowOwnedDetails{Facts: facts},
		}
	case serverapi.SessionRetargetBackgroundProcess:
		result.Detail = &sessionlaunchpb.SessionRetargetWorkspaceError_BackgroundProcessActive{
			BackgroundProcessActive: &sessionlaunchpb.SessionRetargetBackgroundProcessActiveDetails{Facts: facts},
		}
	case serverapi.SessionRetargetRuntimeActive:
		result.Detail = &sessionlaunchpb.SessionRetargetWorkspaceError_RuntimeActive{
			RuntimeActive: &sessionlaunchpb.SessionRetargetRuntimeActiveDetails{Facts: facts},
		}
	default:
		return nil, fmt.Errorf("invalid Session retarget reason %q", failure.Reason)
	}
	return result, nil
}

func SessionRetargetErrorFromProto(failure *sessionlaunchpb.SessionRetargetWorkspaceError) error {
	var facts *sessionlaunchpb.SessionRetargetFacts
	var reason serverapi.SessionRetargetErrorReason
	switch detail := failure.Detail.(type) {
	case *sessionlaunchpb.SessionRetargetWorkspaceError_TargetProjectRequired:
		facts, reason = detail.TargetProjectRequired.Facts, serverapi.SessionRetargetTargetProjectRequired
	case *sessionlaunchpb.SessionRetargetWorkspaceError_TargetProjectConflict:
		facts, reason = detail.TargetProjectConflict.Facts, serverapi.SessionRetargetTargetProjectConflict
	case *sessionlaunchpb.SessionRetargetWorkspaceError_WorkflowOwned:
		facts, reason = detail.WorkflowOwned.Facts, serverapi.SessionRetargetWorkflowOwned
	case *sessionlaunchpb.SessionRetargetWorkspaceError_BackgroundProcessActive:
		facts, reason = detail.BackgroundProcessActive.Facts, serverapi.SessionRetargetBackgroundProcess
	case *sessionlaunchpb.SessionRetargetWorkspaceError_RuntimeActive:
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
