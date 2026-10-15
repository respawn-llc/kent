package auth

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"core/shared/config"
)

type Manager struct {
	mutationMu sync.Mutex
	store      Store
	refreshers map[config.ConnectionProtocol]*OAuthRefresher
}

func NewManager(store Store, refreshers map[config.ConnectionProtocol]*OAuthRefresher) *Manager {
	return &Manager{store: store, refreshers: maps.Clone(refreshers)}
}

// Load observes persisted credentials without refreshing or waiting for a
// network exchange owned by a concurrent mutation.
func (m *Manager) Load(ctx context.Context) (State, error) {
	if m.store == nil {
		return EmptyState(), nil
	}
	state, err := m.store.Load(ctx)
	if err != nil {
		return State{}, err
	}
	return state, state.Validate()
}

func (m *Manager) SaveOAuth(ctx context.Context, id config.ConnectionID, credential OAuthMethod) error {
	if _, err := config.ParseConnectionID(string(id)); err != nil {
		return err
	}
	if err := credential.Validate(); err != nil {
		return err
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.Load(ctx)
	if err != nil {
		return err
	}
	return m.save(ctx, state, id, credential)
}

func (m *Manager) CurrentOAuth(ctx context.Context, id config.ConnectionID, protocol config.ConnectionProtocol) (OAuthMethod, error) {
	if _, err := config.ParseConnectionID(string(id)); err != nil {
		return OAuthMethod{}, err
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.Load(ctx)
	if err != nil {
		return OAuthMethod{}, err
	}
	credential, present := state.Connections[id]
	if !present {
		return OAuthMethod{}, fmt.Errorf("connection %s: %w; sign in to this connection", id, ErrAuthNotConfigured)
	}
	if m.refreshers == nil {
		return credential, nil
	}
	refresher, present := m.refreshers[protocol]
	if !present || refresher == nil {
		return OAuthMethod{}, fmt.Errorf("connection %s has no OAuth refresher for protocol %s", id, protocol)
	}
	updated, refreshed, err := refresher.MaybeRefresh(ctx, credential)
	if err != nil {
		return OAuthMethod{}, fmt.Errorf("connection %s: %w", id, err)
	}
	if !refreshed {
		return credential, nil
	}
	if err := updated.Validate(); err != nil {
		return OAuthMethod{}, err
	}
	if err := m.save(ctx, state, id, updated); err != nil {
		return OAuthMethod{}, err
	}
	return updated, nil
}

func (m *Manager) save(ctx context.Context, state State, id config.ConnectionID, credential OAuthMethod) error {
	if m.store == nil {
		return errors.New("OAuth credential store is required")
	}
	state.Connections[id] = credential
	return m.store.Save(ctx, state)
}
