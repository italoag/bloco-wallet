package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"blocowallet/internal/wallet"
)

func canonicalBatchFileRefsFromPrivateKeys(items []wallet.PrivateKeyBatchItem) []canonicalBatchFileRef {
	files := make([]canonicalBatchFileRef, 0, len(items))
	for _, item := range items {
		files = append(files, canonicalBatchFileRef{name: item.Name, path: item.SourcePath})
	}
	return files
}

func readCanonicalPrivateKeyBatch(directory string) (items []wallet.PrivateKeyBatchItem, err error) {
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("batch directory must be absolute")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("batch path must be a regular directory")
	}
	directoryFile, err := openPathNoFollow(directory, true)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := directoryFile.Close(); closeErr != nil {
			if err == nil {
				err = closeErr
			}
			clearCanonicalPrivateKeyItems(items)
			items = nil
		}
	}()
	openedInfo, err := directoryFile.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.IsDir() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("batch directory changed while opening")
	}
	entries, err := directoryFile.ReadDir(canonicalBatchDirectoryLimit + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > canonicalBatchDirectoryLimit {
		return nil, fmt.Errorf("batch exceeds %d directory entries", canonicalBatchDirectoryLimit)
	}
	names := make([]string, 0, len(entries))
	byName := make(map[string]os.DirEntry, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".key", ".privatekey", ".pk":
		default:
			continue
		}
		names = append(names, entry.Name())
		byName[entry.Name()] = entry
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("batch directory contains no private key files")
	}
	if len(names) > canonicalBatchLimit {
		return nil, fmt.Errorf("batch exceeds %d files", canonicalBatchLimit)
	}
	sort.Strings(names)
	items = make([]wallet.PrivateKeyBatchItem, 0, len(names))
	for _, fileName := range names {
		item := wallet.PrivateKeyBatchItem{
			Name:       strings.TrimSuffix(fileName, filepath.Ext(fileName)),
			SourcePath: filepath.Join(directory, fileName),
		}
		entryInfo, infoErr := byName[fileName].Info()
		if infoErr != nil {
			item.PreflightErr = fmt.Errorf("%w: %v", errCanonicalBatchSourceRead, infoErr)
			items = append(items, item)
			continue
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() {
			item.PreflightErr = fmt.Errorf("%w", errCanonicalBatchSourceRead)
			items = append(items, item)
			continue
		}
		data, readErr := readCanonicalFileAt(directoryFile, fileName, entryInfo, wallet.MaxPrivateKeyImportBytes)
		if readErr != nil {
			item.PreflightErr = fmt.Errorf("%w: %v", errCanonicalBatchSourceRead, readErr)
			items = append(items, item)
			continue
		}
		item.PrivateKey = data
		items = append(items, item)
	}
	return items, nil
}

func clearCanonicalPrivateKeyItems(items []wallet.PrivateKeyBatchItem) {
	for index := range items {
		clear(items[index].PrivateKey)
	}
}
