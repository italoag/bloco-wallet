package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type ConfigCommittedWarning struct {
	Cause error
}

func (warning *ConfigCommittedWarning) Error() string {
	if warning == nil || warning.Cause == nil {
		return "configuration committed but durability confirmation failed"
	}
	return "configuration committed but durability confirmation failed: " + warning.Cause.Error()
}

func (warning *ConfigCommittedWarning) Unwrap() error {
	if warning == nil {
		return nil
	}
	return warning.Cause
}

func IsConfigCommitted(err error) bool {
	var warning *ConfigCommittedWarning
	return errors.As(err, &warning)
}

func ensurePrivateConfigDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("configuration directory must be absolute")
	}
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("configuration directory must be a regular directory")
		}
		return secureConfigPermissions(path, true)
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err = os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("configuration directory changed during creation")
	}
	return secureConfigPermissions(path, true)
}

func validatePrivateConfigPath(path string, allowMissing bool) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("configuration path must be absolute")
	}
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return fmt.Errorf("configuration parent must be a regular directory")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && allowMissing {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("configuration must be a regular file")
	}
	if info.Size() > 1<<20 {
		return fmt.Errorf("configuration exceeds size policy")
	}
	return nil
}

func writeAtomicConfig(path string, data []byte, replace bool) error {
	if len(data) == 0 {
		return fmt.Errorf("configuration data is empty")
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("configuration exceeds size policy")
	}
	if err := validatePrivateConfigPath(path, true); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	var destinationInfo os.FileInfo
	if replace {
		destinationInfo, err = os.Lstat(path)
		if err != nil {
			return err
		}
	}
	temporary, err := os.CreateTemp(parent, ".bloco-config-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := secureConfigPermissions(temporaryPath, false); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := validatePrivateConfigPath(path, true); err != nil {
		return err
	}
	currentParent, err := os.Lstat(parent)
	if err != nil || !os.SameFile(parentInfo, currentParent) {
		return fmt.Errorf("configuration parent changed before commit")
	}
	if replace {
		currentDestination, err := os.Lstat(path)
		if err != nil || !os.SameFile(destinationInfo, currentDestination) {
			return fmt.Errorf("configuration changed before commit")
		}
	}
	commitErr := commitConfigFile(temporaryPath, path, replace)
	if commitErr != nil && !IsConfigCommitted(commitErr) {
		return commitErr
	}
	if commitErr == nil {
		keepTemporary = false
	}
	syncErr := syncConfigDirectory(parent)
	if commitErr != nil || syncErr != nil {
		return &ConfigCommittedWarning{Cause: errors.Join(commitErr, syncErr)}
	}
	return nil
}
