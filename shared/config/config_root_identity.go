package config

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// PersistenceRootHash returns a short, stable identifier for a persistence
// root. It is derived from the cleaned root path and is used both to scope the
// local RPC socket directory and to stamp protocol.ServerIdentity so clients can
// confirm an attached server actually serves the requested root rather than a
// different instance reachable on the same TCP endpoint.
//
// Client and server must derive the hash from the same absolute root for the
// comparison to hold; both use the root resolved by config.Load.
func PersistenceRootHash(persistenceRoot string) string {
	trimmedRoot := strings.TrimSpace(persistenceRoot)
	if trimmedRoot == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(filepath.Clean(trimmedRoot)))
	return hex.EncodeToString(hash[:8])
}

// IsDefaultPersistenceRoot reports whether the supplied root resolves to the
// default persistence root (<home>/.kent). An explicit --persistence-root or
// KENT_PERSISTENCE_ROOT that points at the default carries no cross-root
// isolation risk, so callers skip root-id attach validation for it and stay
// compatible with servers that predate persistence-root identity stamping
// (which report an empty id). An empty root is treated as the default.
func IsDefaultPersistenceRoot(persistenceRoot string) (bool, error) {
	trimmed := strings.TrimSpace(persistenceRoot)
	if trimmed == "" {
		return true, nil
	}
	defaultRoot, err := NormalizePersistenceRoot(DefaultPersistence)
	if err != nil {
		return false, err
	}
	resolved, err := NormalizePersistenceRoot(trimmed)
	if err != nil {
		return false, err
	}
	return filepath.Clean(resolved) == filepath.Clean(defaultRoot), nil
}
