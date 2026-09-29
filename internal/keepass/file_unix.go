//go:build !windows

package keepass

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func openVaultFile(path string) (*os.File, os.FileInfo, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("%w: vault file must be a regular file", ErrInvalidRecord)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		_ = file.Close()
		return nil, nil, fmt.Errorf("%w: vault file must not have additional links", ErrInvalidRecord)
	}
	return file, info, nil
}

func createVaultTemp(dir string) (*os.File, error) {
	return os.CreateTemp(dir, ".bloco-*.tmp")
}

func installVaultFile(tempPath, target string, replace bool) (bool, error) {
	if replace {
		if err := os.Rename(tempPath, target); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := os.Link(tempPath, target); err != nil {
		return false, err
	}
	if err := os.Remove(tempPath); err != nil {
		return true, err
	}
	return true, nil
}

func syncVaultDirectory(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}

func lockVaultFile(path string) (func(), error) {
	lockPath := path + ".bloco.lock"
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("vault lock must be a regular file")
	}
	if err := unix.Fchmod(fd, 0600); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(fd)
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return func() {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = unix.Close(fd)
	}, nil
}

func secureVaultFile(path string) error {
	return os.Chmod(path, 0600)
}
