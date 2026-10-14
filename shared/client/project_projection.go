package client

import (
	"core/shared/clientui"
	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func ProjectSummaryFromProto(project *projectpb.ProjectSummary) clientui.ProjectSummary {
	availability := protoapi.ProjectAvailabilityFromProto(project.Availability)
	return clientui.ProjectSummary{
		ProjectID:    project.ProjectId,
		ProjectKey:   project.ProjectKey,
		DisplayName:  project.DisplayName,
		RootPath:     project.RootPath,
		Availability: availability,
		SessionCount: int(project.SessionCount),
		UpdatedAt:    project.UpdatedAt.AsTime(),
	}
}

func ProjectSummariesFromProto(projects []*projectpb.ProjectSummary) []clientui.ProjectSummary {
	result := make([]clientui.ProjectSummary, 0, len(projects))
	for _, project := range projects {
		summary := ProjectSummaryFromProto(project)
		result = append(result, summary)
	}
	return result
}

func ProjectBindingFromProto(binding *projectpb.ProjectBinding) serverapi.ProjectBinding {
	availability := protoapi.ProjectAvailabilityFromProto(binding.WorkspaceStatus)
	return serverapi.ProjectBinding{
		ProjectID:       binding.ProjectId,
		ProjectKey:      binding.ProjectKey,
		ProjectName:     binding.ProjectName,
		WorkspaceID:     binding.WorkspaceId,
		CanonicalRoot:   binding.CanonicalRoot,
		WorkspaceName:   binding.WorkspaceName,
		WorkspaceStatus: string(availability),
	}
}

func SessionSummaryFromProto(session *projectpb.SessionSummary) clientui.SessionSummary {
	shortID := protoapi.SessionShortIDFromProto(session.ShortId)
	sessionID := runtimeids.SessionIDFromProto(session.SessionId)
	category := protoapi.SessionCategoryFromProto(session.Category)
	return clientui.SessionSummary{
		SessionID:          sessionID,
		ShortID:            shortID,
		Category:           category,
		Name:               textutil.Pointer(session.Name),
		FirstPromptPreview: textutil.Pointer(session.FirstPromptPreview),
		UpdatedAt:          session.UpdatedAt.AsTime(),
	}
}

func SessionSummariesFromProto(sessions []*projectpb.SessionSummary) []clientui.SessionSummary {
	result := make([]clientui.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		summary := SessionSummaryFromProto(session)
		result = append(result, summary)
	}
	return result
}
