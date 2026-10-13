package runtime

import (
	"context"

	"core/server/metadata"
)

type ProjectWorkspaceContextReader interface {
	ReadSessionProjectWorkspaceContext(
		context.Context,
		string,
	) (metadata.SessionProjectWorkspaceContext, error)
}
