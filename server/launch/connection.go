package launch

import (
	"core/server/session"
	"core/shared/config"
	"core/shared/textutil"
)

// ResolveSessionConnection projects a binding without changing Session metadata.
// Only a removed definition permits ordinary resume to select a replacement.
func ResolveSessionConnection(settings config.Settings, saved *config.ConnectionID) (config.ConnectionID, *config.ConnectionReplacement, error) {
	if saved != nil {
		if _, err := config.ParseConnectionID(string(*saved)); err != nil {
			return "", nil, err
		}
		if _, present := settings.Connections[*saved]; present {
			return *saved, nil, nil
		}
	}
	if _, err := settings.SelectedConnection(); err != nil {
		return "", nil, err
	}
	selected := *settings.Connection
	if saved == nil {
		return selected, nil, nil
	}
	return selected, &config.ConnectionReplacement{Previous: *saved, Current: selected}, nil
}

func BindSessionConnection(store *session.Store, settings *config.Settings, sources map[string]config.Origin) (*config.ConnectionReplacement, error) {
	saved := store.Meta().ConnectionID
	selected, replacement, err := ResolveSessionConnection(*settings, saved)
	if err != nil {
		return nil, err
	}
	if saved == nil || *saved != selected {
		if err := store.SetConnectionID(selected); err != nil {
			return nil, err
		}
	}
	settings.Connection = &selected
	config.InheritReviewerSettings(settings, sources)
	return replacement, nil
}

func projectSessionConnection(settings *config.Settings, source config.SourceReport, meta session.Meta) error {
	selected, _, err := ResolveSessionConnection(*settings, meta.ConnectionID)
	if err != nil {
		return err
	}
	settings.Connection = textutil.Value(selected)
	config.InheritReviewerSettings(settings, source.Sources)
	return nil
}
