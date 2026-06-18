package config

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"strings"
)

// canonicalRootForIdentity returns the stable comparison/hash key for a
// persistence root. It cleans separators and, on platforms whose default
// filesystem is case-insensitive (macOS, Windows), folds case so the same
// directory spelled with different casing for the server and client resolves to
// the same identity instead of a false ErrServerRootMismatch. Case-sensitive
// platforms (Linux) preserve the caller's spelling. The rare case-sensitive
// volume on darwin/windows trades a possible hash collision (fail-open: accept)
// for never falsely rejecting the same directory, which is the safer default for
// an isolation identity check.
func canonicalRootForIdentity(root string) string {
	cleaned := filepath.Clean(root)
	switch runtime.GOOS {
	case "darwin", "windows":
		return strings.ToLower(cleaned)
	default:
		return cleaned
	}
}

// PersistenceRootHash returns a short, stable identifier for a persistence
// root. It is derived from the canonicalized root path (see
// canonicalRootForIdentity) and is used both to scope the local RPC socket
// directory and to stamp protocol.ServerIdentity so clients can confirm an
// attached server actually serves the requested root rather than a different
// instance reachable on the same TCP endpoint.
//
// Client and server must derive the hash from the same absolute root for the
// comparison to hold; both use the root resolved by config.Load. Casing is
// folded on case-insensitive default filesystems so the same directory spelled
// differently does not produce diverging ids.
func PersistenceRootHash(persistenceRoot string) string {
	trimmedRoot := strings.TrimSpace(persistenceRoot)
	if trimmedRoot == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(canonicalRootForIdentity(trimmedRoot)))
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
	return canonicalRootForIdentity(resolved) == canonicalRootForIdentity(defaultRoot), nil
}
