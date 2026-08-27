package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const sourceIdentityKeyFile = "source-identity.key"

func loadOrCreateSourceIdentityKey(appDirectory string) ([]byte, error) {
	path := filepath.Join(appDirectory, sourceIdentityKeyFile)
	key, err := readSourceIdentityKey(path)
	if err == nil {
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		clear(key)
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		clear(key)
		if os.IsExist(err) {
			return readSourceIdentityKey(path)
		}
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		clear(key)
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		clear(key)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		clear(key)
		return nil, err
	}
	if err := syncPrivateDirectory(appDirectory); err != nil {
		clear(key)
		return nil, err
	}
	return key, nil
}

func readSourceIdentityKey(path string) (key []byte, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != 32 {
		return nil, fmt.Errorf("source identity key is not a private regular 32-byte file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("source identity key changed while opening")
	}
	if err := file.Chmod(0600); err != nil {
		return nil, err
	}
	key, err = io.ReadAll(io.LimitReader(file, 33))
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		clear(key)
		return nil, fmt.Errorf("source identity key has invalid length")
	}
	return key, nil
}
