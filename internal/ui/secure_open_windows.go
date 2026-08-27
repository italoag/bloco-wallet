//go:build windows

package ui

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func openPathNoFollow(path string, directory bool) (*os.File, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func openFileAtNoFollow(directory *os.File, name string) (*os.File, error) {
	return openPathNoFollow(filepath.Join(directory.Name(), name), false)
}
