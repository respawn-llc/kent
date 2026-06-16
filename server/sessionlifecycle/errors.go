package sessionlifecycle

import "errors"

// Sentinel errors produced by the session lifecycle service. Callers and tests
// match these with errors.Is rather than comparing rendered message text, which
// is free to change without affecting behavior.
var (
	// errLifecycleClientClosed is returned when a loopback lifecycle client is
	// invoked after it has been closed (or was never initialized).
	errLifecycleClientClosed = errors.New("session lifecycle client is closed")

	// errPersistenceRootRequired is returned when an operation that needs the
	// persistence root (e.g. workspace retargeting) is invoked without one.
	errPersistenceRootRequired = errors.New("persistence root is required")
)
