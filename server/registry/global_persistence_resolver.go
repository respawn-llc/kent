package registry

import (
	"context"
	"strings"

	"core/server/session"
)

type GlobalPersistenceSessionResolver struct {
	persistenceRoot string
	storeOptions    []session.StoreOption
}

func NewGlobalPersistenceSessionResolver(persistenceRoot string, storeOptions ...session.StoreOption) GlobalPersistenceSessionResolver {
	return GlobalPersistenceSessionResolver{persistenceRoot: strings.TrimSpace(persistenceRoot), storeOptions: append([]session.StoreOption(nil), storeOptions...)}
}

func (r GlobalPersistenceSessionResolver) ResolveSessionStore(_ context.Context, sessionID string) (*session.Store, error) {
	return session.OpenByID(r.persistenceRoot, sessionID, r.storeOptions...)
}
