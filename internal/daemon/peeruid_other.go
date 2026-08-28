//go:build !linux

package daemon

import "net"

// platformPeerUID is unsupported on this platform; the 0600 socket
// permissions are the remaining boundary.
func platformPeerUID(conn net.Conn) (uint32, bool) {
	return 0, false
}

func currentUID() uint32 {
	return 0
}
