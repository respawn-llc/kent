//go:build !darwin && !linux && !windows

package sleepguard

type platformGuardImpl struct{}

func (p *platformGuardImpl) start() {}
func (p *platformGuardImpl) stop()  {}
