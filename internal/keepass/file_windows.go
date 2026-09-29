//go:build windows

package keepass

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func openVaultFile(path string) (*os.File, os.FileInfo, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, nil, err
	}
	handle, err := windows.CreateFile(pathPointer, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	var details windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &details); err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if details.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || details.NumberOfLinks != 1 || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("%w: vault file must be a regular file", ErrInvalidRecord)
	}
	return file, info, nil
}

func createVaultTemp(dir string) (*os.File, error) {
	return os.CreateTemp(dir, ".bloco-*.tmp")
}

func installVaultFile(tempPath, target string, replace bool) (bool, error) {
	tempPointer, err := windows.UTF16PtrFromString(tempPath)
	if err != nil {
		return false, err
	}
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return false, err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	if err := windows.MoveFileEx(tempPointer, targetPointer, flags); err != nil {
		return false, err
	}
	return true, nil
}

func syncVaultDirectory(dir string) error {
	return nil
}

func lockVaultFile(path string) (func(), error) {
	lockPath := path + ".bloco.lock"
	pathPointer, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(pathPointer, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, ErrBusy
		}
		return nil, err
	}
	file := os.NewFile(uintptr(handle), lockPath)
	var details windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &details); err != nil {
		_ = file.Close()
		return nil, err
	}
	if details.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || details.NumberOfLinks != 1 {
		_ = file.Close()
		return nil, fmt.Errorf("vault lock must be a regular file")
	}
	if err := secureVaultFile(lockPath); err != nil {
		_ = file.Close()
		return nil, err
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
		_ = file.Close()
	}, nil
}

func secureVaultFile(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sddl := fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FA;;;SY)", user.User.Sid.String())
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
