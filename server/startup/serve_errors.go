package startup

import "errors"

// errContextRequired is returned by Serve when invoked with a nil context.
// Callers and tests match it with errors.Is rather than comparing rendered
// message text.
var errContextRequired = errors.New("context is required")
