package launch

import (
	"core/server/session"
	"core/shared/config"
	"core/shared/runtimeids"
)

func ApplyRetainedToolSelection(app config.App, meta session.Meta) (config.App, error) {
	if meta.RetainedToolSelection == nil {
		return app, nil
	}
	id, err := runtimeids.ParseSessionID(meta.SessionID)
	if err != nil {
		return config.App{}, err
	}
	selection := *meta.RetainedToolSelection
	selection.Origin.RetainedSessionID = &id
	app.Settings, app.Source.Sources = config.OverlayToolSelection(app.Settings, app.Source.Sources, selection)
	return app, nil
}
