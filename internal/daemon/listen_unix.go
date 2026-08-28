//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// listen binds a user-private Unix socket with 0600 permissions and a
// single-instance lock file.
func listen(address string) (net.Listener, string, error) {
	if len(address) > 104 || strings.ContainsAny(address, "\x00") {
		return nil, "", fmt.Errorf("daemon: invalid socket path")
	}
	if err := acquireSocketLock(address); err != nil {
		return nil, "", err
	}
	if info, err := os.Lstat(address); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, "", fmt.Errorf("daemon: socket path is not a socket")
		}
		// Remove a stale socket only if no process is listening on it.
		conn, dialErr := net.DialTimeout("unix", address, 250*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return nil, "", fmt.Errorf("daemon: another instance is already listening")
		}
		if err := os.Remove(address); err != nil {
			return nil, "", fmt.Errorf("daemon: remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("daemon: inspect socket path: %w", err)
	}
	listener, err := net.Listen("unix", address)
	if err != nil {
		return nil, "", fmt.Errorf("daemon: listen: %w", err)
	}
	if err := os.Chmod(address, 0600); err != nil {
		_ = listener.Close()
		return nil, "", fmt.Errorf("daemon: chmod socket: %w", err)
	}
	return listener, address, nil
}

// acquireSocketLock takes an exclusive lock file next to the socket so two
// instances cannot race on the same path.
func acquireSocketLock(address string) error {
	lockPath := filepath.Join(filepath.Dir(address), "."+filepath.Base(address)+".lock")
	file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		_, writeErr := file.WriteString(strconv.Itoa(currentPID()))
		closeErr := file.Close()
		if writeErr != nil {
			_ = os.Remove(lockPath)
			return fmt.Errorf("daemon: write lock: %w", writeErr)
		}
		if closeErr != nil {
			_ = os.Remove(lockPath)
			return fmt.Errorf("daemon: close lock: %w", closeErr)
		}
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("daemon: acquire lock: %w", err)
	}
	raw, readErr := os.ReadFile(lockPath)
	if readErr == nil {
		if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw))); parseErr == nil && pid > 0 && processAlive(pid) {
			return fmt.Errorf("daemon: another instance is already running")
		}
	}
	if removeErr := os.Remove(lockPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return fmt.Errorf("daemon: remove stale lock: %w", removeErr)
	}
	return acquireSocketLock(address)
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
