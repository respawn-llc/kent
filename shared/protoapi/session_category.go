package protoapi

import (
	"fmt"

	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/sessioncontract"
)

var sessionCategoryValues = map[projectpb.SessionCategory]sessioncontract.SessionCategory{
	projectpb.SessionCategory_SESSION_CATEGORY_MAIN:     sessioncontract.SessionCategoryMain,
	projectpb.SessionCategory_SESSION_CATEGORY_SUBAGENT: sessioncontract.SessionCategorySubagent,
}

func SessionCategoryToProto(category sessioncontract.SessionCategory) (projectpb.SessionCategory, error) {
	for wire, native := range sessionCategoryValues {
		if native == category {
			return wire, nil
		}
	}
	return projectpb.SessionCategory_SESSION_CATEGORY_UNSPECIFIED, fmt.Errorf("unsupported Session category %q", category)
}

func SessionCategoryFromProto(category projectpb.SessionCategory) sessioncontract.SessionCategory {
	return sessionCategoryValues[category]
}
