//go:build !unix

package startup

import (
	"net"

	"core/shared/config"
)

func listenLocalSocket(config.App) (net.Listener, func(), bool, error) {
	return nil, nil, false, nil
}
