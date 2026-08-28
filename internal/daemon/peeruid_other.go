//go:build !linux && !darwin

package daemon

import "net"

// peerUID is unsupported on this platform; the 0600 socket permissions are
// the remaining boundary.
func peerUID(conn net.Conn) (uint32, bool) {
	return 0, false
}

func currentUID() uint32 {
	return 0
}
