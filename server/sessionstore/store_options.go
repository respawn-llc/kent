package session

import (
	"time"
)

type StoreOption func(*storeOptions)

type storeOptions struct {
	observer          PersistenceObserver
	reconciler        EventLogReconciliationObserver
	resolver          PersistedSessionResolver
	contextFactWriter SessionContextFactWriter
	now               func() time.Time
}

func WithPersistenceObserver(observer PersistenceObserver) StoreOption {
	return func(options *storeOptions) {
		options.observer = observer
		if reconciler, ok := observer.(EventLogReconciliationObserver); ok {
			options.reconciler = reconciler
		}
	}
}

func WithPersistedSessionResolver(resolver PersistedSessionResolver) StoreOption {
	return func(options *storeOptions) {
		options.resolver = resolver
	}
}

func WithSessionContextFactWriter(writer SessionContextFactWriter) StoreOption {
	return func(options *storeOptions) {
		options.contextFactWriter = writer
	}
}

func WithClock(now func() time.Time) StoreOption {
	return func(options *storeOptions) {
		options.now = now
	}
}

func normalizeStoreOptions(options ...StoreOption) storeOptions {
	result := storeOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}
		option(&result)
	}
	if result.now == nil {
		result.now = func() time.Time {
			return time.Now().UTC()
		}
	}
	return result
}
