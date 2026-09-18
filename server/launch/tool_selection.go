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

func retainedPreparedToolTargets(prepared PreparedRunPromptOverrides, meta session.Meta) (PreparedRunPromptOverrides, error) {
	if meta.RetainedToolSelection == nil {
		return prepared, nil
	}
	var err error
	prepared.OverrideConfig, err = ApplyRetainedToolSelection(prepared.OverrideConfig, meta)
	if err != nil {
		return PreparedRunPromptOverrides{}, err
	}
	if prepared.BaseTarget != nil {
		target, err := retainedToolTarget(*prepared.BaseTarget, meta)
		if err != nil {
			return PreparedRunPromptOverrides{}, err
		}
		prepared.BaseTarget = &target
	}
	if prepared.NamedTarget != nil {
		named := *prepared.NamedTarget
		target, err := retainedToolTarget(PreparedBaseTarget{Settings: named.Settings, Source: named.Source, EnabledTools: named.EnabledTools}, meta)
		if err != nil {
			return PreparedRunPromptOverrides{}, err
		}
		named.Settings, named.Source, named.EnabledTools = target.Settings, target.Source, target.EnabledTools
		prepared.NamedTarget = &named
	}
	return prepared, nil
}

func retainedToolTarget(target PreparedBaseTarget, meta session.Meta) (PreparedBaseTarget, error) {
	app, err := ApplyRetainedToolSelection(config.App{Settings: target.Settings, Source: target.Source}, meta)
	if err != nil {
		return PreparedBaseTarget{}, err
	}
	target.Settings, target.Source = app.Settings, app.Source
	target.EnabledTools, err = ActiveToolIDsForPlan(app.Settings, app.Source, meta.Locked)
	return target, err
}
