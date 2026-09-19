package authservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"core/server/auth"
	"core/server/llm"
	"core/shared/config"
)

type ConnectionResolver struct {
	root        string
	manager     *auth.Manager
	environment func(string) (string, bool)
}

type ResolvedConnection struct {
	ID         config.ConnectionID
	Definition config.ProviderConnection
	Auth       llm.DispatchAuthProvider
}

func NewConnectionResolver(root string, manager *auth.Manager, environment func(string) (string, bool)) *ConnectionResolver {
	if environment == nil {
		environment = os.LookupEnv
	}
	return &ConnectionResolver{root: root, manager: manager, environment: environment}
}

func (r *ConnectionResolver) Resolve(settings config.Settings) (ResolvedConnection, error) {
	definition, err := settings.SelectedConnection()
	if err != nil {
		return ResolvedConnection{}, err
	}
	id := *settings.Connection
	return ResolvedConnection{ID: id, Definition: definition, Auth: connectionAuth{owner: r, id: id}}, nil
}

func (r *ConnectionResolver) resolveTarget(raw *string) (ResolvedConnection, error) {
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: r.root})
	if err != nil {
		return ResolvedConnection{}, err
	}
	if raw != nil {
		id, err := config.ParseConnectionID(*raw)
		if err != nil {
			return ResolvedConnection{}, err
		}
		app.Settings.Connection = &id
	}
	return r.Resolve(app.Settings)
}

func (r *ConnectionResolver) apiKey(id config.ConnectionID, name string) (string, error) {
	value, present := r.environment(name)
	if !present || value == "" {
		return "", fmt.Errorf("connection %s requires server environment variable %s; set it in %s or the server launch environment, then restart the server", id, name, filepath.Join(r.root, ".env"))
	}
	return value, nil
}

type connectionSnapshot struct {
	connection ResolvedConnection
	oauth      *auth.OAuthMethod
	failure    error
}

func (r *ConnectionResolver) snapshot(ctx context.Context, raw *string) (connectionSnapshot, error) {
	connection, err := r.resolveTarget(raw)
	if err != nil {
		return connectionSnapshot{}, err
	}
	snapshot := connectionSnapshot{connection: connection}
	if connection.Definition.Protocol == config.ConnectionChatGPT {
		if r.manager == nil {
			snapshot.failure = auth.ErrAuthNotConfigured
			return snapshot, nil
		}
		state, err := r.manager.Load(ctx)
		if err != nil {
			return connectionSnapshot{}, err
		}
		if credential, present := state.Connections[connection.ID]; present {
			snapshot.oauth = &credential
		} else {
			snapshot.failure = fmt.Errorf("connection %s: %w; sign in to this connection", connection.ID, auth.ErrAuthNotConfigured)
		}
	} else if name := connection.Definition.EnvironmentVariable; name != nil {
		_, snapshot.failure = r.apiKey(connection.ID, *name)
	}
	return snapshot, nil
}

type connectionAuth struct {
	owner *ConnectionResolver
	id    config.ConnectionID
}

func (a connectionAuth) ResolveDispatchAuth(ctx context.Context) (*llm.DispatchAuth, error) {
	// Definition edits change credential references for future dispatches;
	// a runtime client retains only the connection identity.
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: a.owner.root})
	if err != nil {
		return nil, err
	}
	definition, present := app.Settings.Connections[a.id]
	if !present {
		return nil, fmt.Errorf("provider connection %q is no longer defined", a.id)
	}
	switch definition.Protocol {
	case config.ConnectionResponses:
		if definition.EnvironmentVariable == nil {
			return nil, nil
		}
		name := *definition.EnvironmentVariable
		value, err := a.owner.apiKey(a.id, name)
		if err != nil {
			return nil, err
		}
		return &llm.DispatchAuth{Header: "Bearer " + value}, nil
	case config.ConnectionChatGPT:
		if a.owner.manager == nil {
			return nil, fmt.Errorf("connection %s: %w", a.id, auth.ErrAuthNotConfigured)
		}
		credential, err := a.owner.manager.CurrentOAuth(ctx, a.id)
		if err != nil {
			return nil, err
		}
		return &llm.DispatchAuth{
			Header: "Bearer " + credential.AccessToken,
			Mode:   llm.OpenAIAuthMode{IsOAuth: true, AccountID: credential.AccountID},
		}, nil
	default:
		return nil, fmt.Errorf("connection %s has unsupported protocol %q", a.id, definition.Protocol)
	}
}
