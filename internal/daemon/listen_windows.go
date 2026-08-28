//go:build windows

package daemon

import (
	"fmt"
	"net"
	"time"
)

// listen binds a loopback TCP listener with an ephemeral port. The capability
// token is mandatory for every request; the listener never binds a wildcard
// address.
func listen(address string) (net.Listener, string, error) {
	if address != "" && address != "loopback" {
		return nil, "", fmt.Errorf("daemon: unsupported address on windows")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("daemon: listen: %w", err)
	}
	return listener, listener.Addr().String(), nil
}

func acquireSocketLock(address string) error { return nil }

func shortDialTimeout() time.Duration { return 250 * time.Millisecond }
