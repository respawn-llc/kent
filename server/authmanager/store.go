package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
)

const authStateFileMode os.FileMode = 0o600

type Store interface {
	Load(ctx context.Context) (State, error)
	Save(ctx context.Context, state State) error
}

type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Load(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	if err := ensureSecureAuthStatePermissions(s.path); err != nil {
		return State{}, err
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EmptyState(), nil
		}
		return State{}, fmt.Errorf("read auth state: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse auth state: %w", err)
	}
	if err := state.Validate(); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *FileStore) Save(ctx context.Context, state State) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := state.Validate(); err != nil {
		return err
	}
	if err := ensureSecureAuthStatePermissions(s.path); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal auth state: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create auth tmp: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmpFile.Chmod(authStateFileMode); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("set auth tmp permissions: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write auth tmp: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close auth tmp: %w", err)
	}
	if err := ensureRegularFile(tmpPath); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace auth state: %w", err)
	}
	cleanupTmp = false

	if err := ensureSecureAuthStatePermissions(s.path); err != nil {
		return err
	}
	return nil
}

func ensureSecureAuthStatePermissions(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat auth state: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("auth state must be a regular file: %s", path)
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := os.Chmod(path, authStateFileMode); err != nil {
		return fmt.Errorf("set auth state permissions: %w", err)
	}
	return nil
}

func ensureRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("auth state file disappeared before replace")
		}
		return fmt.Errorf("stat auth state: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("auth state must be a regular file: %s", path)
	}
	return nil
}

type MemoryStore struct {
	mu    sync.Mutex
	state State
	set   bool
}

func NewMemoryStore(initial State) *MemoryStore {
	return &MemoryStore{state: State{Connections: maps.Clone(initial.Connections)}, set: true}
}

func (s *MemoryStore) Load(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if s == nil {
		return EmptyState(), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.set {
		return EmptyState(), nil
	}
	return State{Connections: maps.Clone(s.state.Connections)}, nil
}

func (s *MemoryStore) Save(ctx context.Context, state State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := state.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = State{Connections: maps.Clone(state.Connections)}
	s.set = true
	return nil
}
