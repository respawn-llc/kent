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
	selected, err := settings.Connection.ConcreteID()
	if err != nil {
		return ResolvedConnection{}, err
	}
	id := *selected
	return ResolvedConnection{ID: id, Definition: definition, Auth: connectionAuth{owner: r, id: id}}, nil
}

func (r *ConnectionResolver) resolveTarget(raw *string) (ResolvedConnection, config.Settings, error) {
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: r.root})
	if err != nil {
		return ResolvedConnection{}, config.Settings{}, err
	}
	if raw != nil {
		app.Settings.Connection = config.SingleConnection(config.ConnectionID(*raw))
	}
	connection, err := r.Resolve(app.Settings)
	return connection, app.Settings, err
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
	settings   config.Settings
	oauth      *auth.OAuthMethod
	failure    error
	pending    *pendingConnection
}

func (r *ConnectionResolver) snapshot(ctx context.Context, raw *string) (connectionSnapshot, error) {
	connection, settings, err := r.resolveTarget(raw)
	if err != nil {
		return connectionSnapshot{}, err
	}
	snapshot := connectionSnapshot{connection: connection, settings: settings}
	if connection.Definition.Protocol.IsSubscription() {
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
		return nil, &llm.AuthError{ConnectionID: &a.id, Err: err}
	}
	definition, present := app.Settings.Connections[a.id]
	if !present {
		return nil, &llm.AuthError{ConnectionID: &a.id, Err: fmt.Errorf("provider connection %q is no longer defined", a.id)}
	}
	return a.owner.ResolveDefinitionAuth(ctx, a.id, definition)
}

// ResolveDefinitionAuth keeps an invocation's destination and credential reference
// bound to the same selected definition. Ordinary chat reloads before calling it.
func (r *ConnectionResolver) ResolveDefinitionAuth(ctx context.Context, id config.ConnectionID, definition config.ProviderConnection) (result *llm.DispatchAuth, err error) {
	defer func() {
		if err != nil {
			err = &llm.AuthError{ConnectionID: &id, Err: err}
		}
	}()
	if err := definition.Validate(); err != nil {
		return nil, err
	}
	switch definition.Protocol {
	case config.ConnectionResponses, config.ConnectionGrokAPIKey:
		if definition.EnvironmentVariable == nil {
			return nil, nil
		}
		name := *definition.EnvironmentVariable
		value, err := r.apiKey(id, name)
		if err != nil {
			return nil, err
		}
		return &llm.DispatchAuth{Header: "Bearer " + value}, nil
	case config.ConnectionChatGPT, config.ConnectionGrokCLIProxy, config.ConnectionGrokOAuthAPI:
		if r.manager == nil {
			return nil, fmt.Errorf("connection %s: %w", id, auth.ErrAuthNotConfigured)
		}
		credential, err := r.manager.CurrentOAuth(ctx, id, definition.Protocol)
		if err != nil {
			return nil, err
		}
		mode := llm.OpenAIAuthMode{IsOAuth: true}
		if definition.Protocol == config.ConnectionChatGPT {
			mode.AccountID = credential.AccountID
		}
		return &llm.DispatchAuth{
			Header: "Bearer " + credential.AccessToken,
			Mode:   mode,
		}, nil
	default:
		return nil, fmt.Errorf("connection %s has unsupported protocol %q", id, definition.Protocol)
	}
}
