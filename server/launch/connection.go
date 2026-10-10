package launch

import (
	"core/server/session"
	"core/shared/config"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
)

type ConnectionRotation struct {
	counters sync.Map
}

func ConnectionMembership(settings config.Settings) ([]config.ConnectionID, string, error) {
	members, err := settings.ConnectionMembers()
	if err != nil {
		return nil, "", err
	}
	canonical := slices.Clone(members)
	slices.Sort(canonical)
	key, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	return members, string(key), nil
}

func (r *ConnectionRotation) Prepare(settings config.Settings, allocate bool) (config.ConnectionID, error) {
	members, key, err := ConnectionMembership(settings)
	if err != nil {
		return "", err
	}
	if !allocate {
		return members[0], nil
	}
	if r == nil {
		if len(members) == 1 {
			return members[0], nil
		}
		return "", fmt.Errorf("server connection rotation is required for allocating a connection set")
	}
	counter, _ := r.counters.LoadOrStore(key, new(atomic.Uint64))
	index := counter.(*atomic.Uint64).Add(1) - 1
	return members[index%uint64(len(members))], nil
}

func (p RunPromptPreparationContext) prepareConnection(settings *config.Settings, source config.SourceReport) error {
	id, _, err := p.Rotation.ResolveSessionConnection(*settings, p.ConnectionID, p.AllocateConnection)
	if err != nil {
		return err
	}
	settings.Connection = config.SingleConnection(id)
	config.InheritReviewerSettings(settings, source.Sources)
	return nil
}

// ResolveSessionConnection projects a binding without changing Session metadata.
// Only a removed definition permits ordinary resume to select a replacement.
func ResolveSessionConnection(settings config.Settings, saved *config.ConnectionID) (config.ConnectionID, *config.ConnectionReplacement, error) {
	return (*ConnectionRotation)(nil).ResolveSessionConnection(settings, saved, false)
}

func (r *ConnectionRotation) ResolveSessionConnection(settings config.Settings, saved *config.ConnectionID, allocate bool) (config.ConnectionID, *config.ConnectionReplacement, error) {
	if saved != nil {
		if _, err := config.ParseConnectionID(string(*saved)); err != nil {
			return "", nil, err
		}
		if _, present := settings.Connections[*saved]; present {
			return *saved, nil, nil
		}
	}
	selected, err := r.Prepare(settings, allocate)
	if err != nil {
		return "", nil, err
	}
	if saved == nil {
		return selected, nil, nil
	}
	return selected, &config.ConnectionReplacement{Previous: *saved, Current: selected}, nil
}

func BindSessionConnection(store *session.Store, settings *config.Settings, sources map[string]config.Origin) (*config.ConnectionReplacement, error) {
	return (*ConnectionRotation)(nil).BindSessionConnection(store, settings, sources)
}

func (r *ConnectionRotation) BindSessionConnection(store *session.Store, settings *config.Settings, sources map[string]config.Origin) (*config.ConnectionReplacement, error) {
	saved := store.Meta().ConnectionID
	selected, replacement, err := r.ResolveSessionConnection(*settings, saved, settings.Connection != nil && len(*settings.Connection) > 1)
	if err != nil {
		return nil, err
	}
	if saved == nil || *saved != selected {
		if err := store.SetConnectionID(selected); err != nil {
			return nil, err
		}
	}
	settings.Connection = config.SingleConnection(selected)
	config.InheritReviewerSettings(settings, sources)
	return replacement, nil
}
