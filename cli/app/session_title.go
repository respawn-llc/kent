package app

import brand "core/shared/config"

const defaultSessionTitle = brand.Command

func sessionTitle(name *string) string {
	if name == nil {
		return defaultSessionTitle
	}
	return *name
}
