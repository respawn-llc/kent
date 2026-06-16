package sessionview

import "errors"

// errSessionStoreResolverRequired is returned when a session-view operation is
// invoked without a configured session store resolver. Callers and tests match
// it with errors.Is rather than comparing rendered message text.
var errSessionStoreResolverRequired = errors.New("session store resolver is required")
