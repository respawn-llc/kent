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
