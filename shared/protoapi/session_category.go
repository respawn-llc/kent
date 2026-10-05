package protoapi

import (
	"fmt"

	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/sessioncontract"
)

func SessionCategoryToProto(category sessioncontract.SessionCategory) (projectpb.SessionCategory, error) {
	switch category {
	case sessioncontract.SessionCategoryMain:
		return projectpb.SessionCategory_SESSION_CATEGORY_MAIN, nil
	case sessioncontract.SessionCategorySubagent:
		return projectpb.SessionCategory_SESSION_CATEGORY_SUBAGENT, nil
	default:
		return projectpb.SessionCategory_SESSION_CATEGORY_UNSPECIFIED, fmt.Errorf("unsupported Session category %q", category)
	}
}

func SessionCategoryFromProto(category projectpb.SessionCategory) (sessioncontract.SessionCategory, error) {
	switch category {
	case projectpb.SessionCategory_SESSION_CATEGORY_MAIN:
		return sessioncontract.SessionCategoryMain, nil
	case projectpb.SessionCategory_SESSION_CATEGORY_SUBAGENT:
		return sessioncontract.SessionCategorySubagent, nil
	default:
		return "", fmt.Errorf("unsupported generated Session category %s", category)
	}
}
