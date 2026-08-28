//go:build linux

package daemon

import (
	"net"
	"syscall"
)

// platformPeerUID returns the peer's UID via SO_PEERCRED.
func platformPeerUID(conn net.Conn) (uint32, bool) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, false
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, false
	}
	var uid uint32
	controlErr := raw.Control(func(fd uintptr) {
		credentials, credErr := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if credErr == nil {
			uid = credentials.Uid
		}
	})
	if controlErr != nil {
		return 0, false
	}
	return uid, true
}

func currentUID() uint32 {
	return uint32(syscall.Getuid())
}
