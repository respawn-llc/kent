package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"core/shared/config"
)

type Manager struct {
	mutationMu sync.Mutex
	store      Store
	refresher  *OAuthRefresher
}

func NewManager(store Store, refresher *OAuthRefresher) *Manager {
	return &Manager{store: store, refresher: refresher}
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
	if err := (Method{Type: MethodOAuth, OAuth: &credential}).Validate(); err != nil {
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

func (m *Manager) CurrentOAuth(ctx context.Context, id config.ConnectionID) (OAuthMethod, error) {
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
	if m.refresher == nil {
		return credential, nil
	}
	updated, refreshed, err := m.refresher.MaybeRefresh(ctx, Method{Type: MethodOAuth, OAuth: &credential})
	if err != nil {
		return OAuthMethod{}, fmt.Errorf("connection %s: %w", id, err)
	}
	if !refreshed {
		return credential, nil
	}
	if err := updated.Validate(); err != nil {
		return OAuthMethod{}, err
	}
	if updated.Type != MethodOAuth {
		return OAuthMethod{}, errors.New("OAuth refresh returned a non-OAuth credential")
	}
	if err := m.save(ctx, state, id, *updated.OAuth); err != nil {
		return OAuthMethod{}, err
	}
	return *updated.OAuth, nil
}

func (m *Manager) save(ctx context.Context, state State, id config.ConnectionID, credential OAuthMethod) error {
	if m.store == nil {
		return errors.New("OAuth credential store is required")
	}
	state.Connections[id] = credential
	return m.store.Save(ctx, state)
}
