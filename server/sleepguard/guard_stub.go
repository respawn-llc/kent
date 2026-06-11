//go:build !darwin && !linux && !windows

package sleepguard

type platformGuardImpl struct{}

func (p *platformGuardImpl) start() error { return nil }
func (p *platformGuardImpl) stop()        {}
