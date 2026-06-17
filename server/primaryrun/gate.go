package primaryrun

import "core/shared/serverapi"

var ErrActivePrimaryRun = serverapi.ErrActivePrimaryRun

type Lease interface {
	Release()
}

type LeaseFunc func()

func (fn LeaseFunc) Release() {
	if fn != nil {
		fn()
	}
}

type Gate interface {
	AcquirePrimaryRun(sessionID string) (Lease, error)
}
