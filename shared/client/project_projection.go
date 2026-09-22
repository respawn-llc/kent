package client

import (
	"core/shared/clientui"
	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func ProjectSummaryFromProto(project *projectpb.ProjectSummary) (clientui.ProjectSummary, error) {
	availability, err := protoapi.ProjectAvailabilityFromProto(project.Availability)
	if err != nil {
		return clientui.ProjectSummary{}, err
	}
	return clientui.ProjectSummary{
		ProjectID:    project.ProjectId,
		ProjectKey:   project.ProjectKey,
		DisplayName:  project.DisplayName,
		RootPath:     project.RootPath,
		Availability: availability,
		SessionCount: int(project.SessionCount),
		UpdatedAt:    project.UpdatedAt.AsTime(),
	}, nil
}

func ProjectSummariesFromProto(projects []*projectpb.ProjectSummary) ([]clientui.ProjectSummary, error) {
	result := make([]clientui.ProjectSummary, 0, len(projects))
	for _, project := range projects {
		summary, err := ProjectSummaryFromProto(project)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, nil
}

func ProjectBindingFromProto(binding *projectpb.ProjectBinding) (serverapi.ProjectBinding, error) {
	availability, err := protoapi.ProjectAvailabilityFromProto(binding.WorkspaceStatus)
	if err != nil {
		return serverapi.ProjectBinding{}, err
	}
	return serverapi.ProjectBinding{
		ProjectID:       binding.ProjectId,
		ProjectKey:      binding.ProjectKey,
		ProjectName:     binding.ProjectName,
		WorkspaceID:     binding.WorkspaceId,
		CanonicalRoot:   binding.CanonicalRoot,
		WorkspaceName:   binding.WorkspaceName,
		WorkspaceStatus: string(availability),
	}, nil
}

func SessionSummaryFromProto(session *projectpb.SessionSummary) (clientui.SessionSummary, error) {
	sessionID, err := runtimeids.ParseSessionID(session.SessionId)
	if err != nil {
		return clientui.SessionSummary{}, err
	}
	category, err := protoapi.SessionCategoryFromProto(session.Category)
	if err != nil {
		return clientui.SessionSummary{}, err
	}
	return clientui.SessionSummary{
		SessionID:          sessionID,
		Category:           category,
		Name:               textutil.Pointer(session.Name),
		FirstPromptPreview: textutil.Pointer(session.FirstPromptPreview),
		UpdatedAt:          session.UpdatedAt.AsTime(),
	}, nil
}

func SessionSummariesFromProto(sessions []*projectpb.SessionSummary) ([]clientui.SessionSummary, error) {
	result := make([]clientui.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		summary, err := SessionSummaryFromProto(session)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, nil
}
