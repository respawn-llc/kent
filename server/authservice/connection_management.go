package authservice

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync/atomic"

	"core/server/auth"
	"core/server/chatcontext"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"

	"google.golang.org/protobuf/types/known/emptypb"
)

type pendingConnection struct {
	id         config.ConnectionID
	definition config.ProviderConnection
	credential atomic.Pointer[auth.OAuthMethod]
	finishing  bool // guarded by the owner's writeMu
}

func (s *BootstrapService) GetConnections(_ context.Context, req *authpb.GetConnectionsRequest) (*authpb.ConnectionCatalog, error) {
	if req == nil {
		return nil, errors.New("connection catalog request is required")
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: s.connections.root})
	if err != nil {
		return nil, err
	}
	result := &authpb.ConnectionCatalog{}
	if app.Settings.Connection != nil {
		id := string(*app.Settings.Connection)
		result.DefaultConnectionId = &id
	}
	for _, id := range slices.Sorted(maps.Keys(app.Settings.Connections)) {
		result.Connections = append(result.Connections, protoapi.ConnectionToProto(id, app.Settings.Connections[id]))
	}
	if pending := s.pending.Load(); pending != nil {
		result.PendingSetup = protoapi.ConnectionToProto(pending.id, pending.definition)
	}
	if req.WorkspaceRoot != nil {
		workspace, err := chatcontext.NewFixedRootWorkspaceResolver(s.connections.root, "", config.LoadOptions{}).Resolve(*req.WorkspaceRoot)
		if err != nil {
			return nil, err
		}
		source := workspace.Source.Sources["connection"]
		if source.File != nil && source.File.Layer != config.FileGlobal && workspace.Settings.Connection != nil {
			id := string(*workspace.Settings.Connection)
			result.WorkspaceConnectionId = &id
		}
	}
	return result, nil
}

func (s *BootstrapService) ConfigureConnection(_ context.Context, req *authpb.ConfigureConnectionRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, errors.New("connection change is required")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	path, err := config.ResolveSettingsFilePathInRoot(s.connections.root)
	if err != nil {
		return nil, err
	}
	switch change := req.Change.(type) {
	case *authpb.ConfigureConnectionRequest_PendingSetup:
		if pending := s.pending.Load(); pending != nil && pending.finishing {
			return nil, errors.New("setup Finish has already been accepted")
		}
		if err := requireMissingSettings(path); err != nil {
			return nil, err
		}
		id, definition, err := protoapi.ConnectionFromProto(change.PendingSetup)
		if err != nil {
			return nil, err
		}
		s.pending.Store(&pendingConnection{id: id, definition: definition})
	case *authpb.ConfigureConnectionRequest_DiscardSetup:
		if pending := s.pending.Load(); pending != nil && pending.finishing {
			return nil, errors.New("setup Finish has already been accepted")
		}
		s.pending.Store(nil)
	case *authpb.ConfigureConnectionRequest_Add:
		id, definition, err := protoapi.ConnectionFromProto(change.Add)
		if err != nil {
			return nil, err
		}
		if definition.Protocol == config.ConnectionChatGPT {
			return nil, errors.New("complete subscription sign-in before adding this connection")
		}
		if err := config.AddProviderConnection(path, id, definition); err != nil {
			return nil, err
		}
	case *authpb.ConfigureConnectionRequest_Reference:
		if change.Reference == nil {
			return nil, errors.New("connection reference edit is required")
		}
		if err := config.SetProviderConnectionEnvironment(path, config.ConnectionID(change.Reference.ConnectionId), change.Reference.EnvironmentVariable); err != nil {
			return nil, err
		}
	case *authpb.ConfigureConnectionRequest_DefaultConnectionId:
		if err := config.SetDefaultProviderConnection(path, config.ConnectionID(change.DefaultConnectionId)); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("connection change is required")
	}
	return &emptypb.Empty{}, nil
}

// PendingSettings is a completed-state projection and never waits for sign-in
// or Finish. Wizard facts and finalization share this connection selection.
func (s *BootstrapService) PendingSettings(settings config.Settings) (config.Settings, error) {
	pending := s.pending.Load()
	if pending == nil {
		return config.Settings{}, errors.New("select the first provider connection")
	}
	return pending.settings(settings), nil
}

func (p *pendingConnection) settings(settings config.Settings) config.Settings {
	settings.Connection = &p.id
	settings.Connections = map[config.ConnectionID]config.ProviderConnection{p.id: p.definition}
	return settings
}

func (s *BootstrapService) FinishSetup(path string, prepare func(context.Context, config.Settings) (config.Settings, config.OnboardingWriteOptions, error)) (string, error) {
	s.finishMu.Lock()
	defer s.finishMu.Unlock()
	s.writeMu.Lock()
	pending := s.pending.Load()
	if pending == nil {
		s.writeMu.Unlock()
		return "", errors.New("select the first provider connection")
	}
	credential := pending.credential.Load()
	if pending.definition.Protocol == config.ConnectionChatGPT && credential == nil {
		s.writeMu.Unlock()
		return "", auth.ErrAuthNotConfigured
	}
	pending.finishing = true
	s.writeMu.Unlock()
	defer func() {
		s.writeMu.Lock()
		pending.finishing = false
		s.writeMu.Unlock()
	}()
	settings, options, err := prepare(s.ctx, pending.settings(config.DefaultOnboardingSettings()))
	if err != nil {
		return "", err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return "", err
	}
	if credential != nil {
		if err := s.connections.manager.SaveOAuth(s.ctx, pending.id, *credential); err != nil {
			return "", err
		}
	}
	written, err := config.WriteSettingsFileForOnboardingWithOptionsAt(path, settings, options)
	if err == nil {
		s.pending.Store(nil)
	}
	return written, err
}

func requireMissingSettings(path string) error {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	return fmt.Errorf("setup is already complete: %s", path)
}

func (s *BootstrapService) targetSnapshot(ctx context.Context, target *authpb.ConnectionTarget) (connectionSnapshot, error) {
	if target == nil {
		return connectionSnapshot{}, errors.New("connection target is required")
	}
	switch value := target.Target.(type) {
	case *authpb.ConnectionTarget_ConnectionId:
		return s.connections.snapshot(ctx, &value.ConnectionId)
	case *authpb.ConnectionTarget_PendingSetup:
		pending := s.pending.Load()
		if pending == nil {
			return connectionSnapshot{}, errors.New("select the first provider connection")
		}
		snapshot := connectionSnapshot{connection: ResolvedConnection{ID: pending.id, Definition: pending.definition}, pending: pending, oauth: pending.credential.Load()}
		if pending.definition.Protocol == config.ConnectionChatGPT && snapshot.oauth == nil {
			snapshot.failure = auth.ErrAuthNotConfigured
		}
		return snapshot, nil
	case *authpb.ConnectionTarget_AddConnection:
		id, definition, err := protoapi.ConnectionFromProto(value.AddConnection)
		if err != nil {
			return connectionSnapshot{}, err
		}
		if err := s.requireUnusedID(id); err != nil {
			return connectionSnapshot{}, err
		}
		return connectionSnapshot{connection: ResolvedConnection{ID: id, Definition: definition}, failure: auth.ErrAuthNotConfigured}, nil
	default:
		return connectionSnapshot{}, errors.New("connection target is required")
	}
}

func (s *BootstrapService) requireUnusedID(id config.ConnectionID) error {
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: s.connections.root})
	if err != nil {
		return err
	}
	if !app.Source.SettingsFileExists() {
		return errors.New("complete first-run setup before adding another connection")
	}
	if _, present := app.Settings.Connections[id]; present {
		return fmt.Errorf("connection %q already exists", id)
	}
	return nil
}

func (s *BootstrapService) commitOAuth(target *authpb.ConnectionTarget, snapshot connectionSnapshot, credential auth.OAuthMethod) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	switch value := target.Target.(type) {
	case *authpb.ConnectionTarget_PendingSetup:
		if s.pending.Load() != snapshot.pending {
			return errors.New("connection setup was changed or discarded")
		}
		snapshot.pending.credential.Store(&credential)
		return nil
	case *authpb.ConnectionTarget_AddConnection:
		if err := s.requireUnusedID(snapshot.connection.ID); err != nil {
			return err
		}
	case *authpb.ConnectionTarget_ConnectionId:
		current, err := s.connections.resolveTarget(&value.ConnectionId)
		if err != nil {
			return err
		}
		if current.Definition.Protocol != config.ConnectionChatGPT {
			return errors.New("connection no longer uses subscription sign-in")
		}
	default:
		return errors.New("connection target is required")
	}
	if err := s.connections.manager.SaveOAuth(s.ctx, snapshot.connection.ID, credential); err != nil {
		return err
	}
	if _, add := target.Target.(*authpb.ConnectionTarget_AddConnection); add {
		path, err := config.ResolveSettingsFilePathInRoot(s.connections.root)
		if err != nil {
			return err
		}
		return config.AddProviderConnection(path, snapshot.connection.ID, snapshot.connection.Definition)
	}
	return nil
}
