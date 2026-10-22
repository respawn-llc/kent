package app

import (
	brand "core/shared/config"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

const defaultSessionTitle = brand.Command

func (m *uiModel) sessionTitle() string {
	return sessionDisplayTitle(m.sessionName, m.sessionCategory, m.sessionShortID, defaultSessionTitle)
}

func sessionDisplayTitle(name *string, category *sessioncontract.SessionCategory, shortID *runtimeids.SessionShortID, fallback string) string {
	if name != nil {
		return *name
	}
	if category != nil && *category == sessioncontract.SessionCategorySubagent && shortID != nil {
		return "Subagent " + shortID.String()
	}
	return fallback
}
