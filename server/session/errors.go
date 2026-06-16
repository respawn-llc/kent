package session

import "errors"

// Sentinel errors produced by the session store and its loaders. Callers and
// tests match these with errors.Is rather than comparing rendered message text,
// which is free to change without affecting behavior. Dynamic context (ids,
// paths, underlying causes) is attached via fmt.Errorf("... %w", Err...).
var (
	// ErrSessionFileSymlink is returned when a session file (metadata or events)
	// is a symlink, which is rejected for security reasons. It is exported so
	// external callers of the public snapshot/open API can detect it.
	ErrSessionFileSymlink = errors.New("session file must not be a symlink")

	// ErrReadSessionMeta wraps any failure reading the on-disk session metadata
	// file (missing, unreadable, or rejected as a symlink).
	ErrReadSessionMeta = errors.New("read session meta")

	// errPersistedSessionResolverRequired is returned by OpenByID when no
	// persisted-session resolver is configured.
	errPersistedSessionResolverRequired = errors.New("persisted session resolver is required")

	// Resolver-record validation guards. Each names a distinct way a resolver
	// can return an invalid persisted session record.
	errResolverRecordMissingSessionDir  = errors.New("resolver returned persisted session record with missing session dir")
	errResolverRecordRelativeSessionDir = errors.New("resolver returned persisted session record whose session dir is not an absolute clean path")
	errResolverRecordMissingMetadata    = errors.New("resolver returned persisted session record with missing metadata")
)
