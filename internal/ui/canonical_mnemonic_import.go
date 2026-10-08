package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"
)

type canonicalBatchFileRef struct {
	name string
	path string
}

func canonicalBatchFileRefsFromKeystores(items []wallet.KeystoreBatchItem) []canonicalBatchFileRef {
	files := make([]canonicalBatchFileRef, 0, len(items))
	for _, item := range items {
		files = append(files, canonicalBatchFileRef{name: item.Name, path: item.SourcePath})
	}
	return files
}

func canonicalBatchFileRefsFromMnemonics(items []wallet.MnemonicBatchItem) []canonicalBatchFileRef {
	files := make([]canonicalBatchFileRef, 0, len(items))
	for _, item := range items {
		files = append(files, canonicalBatchFileRef{name: item.Name, path: item.SourcePath})
	}
	return files
}

func canonicalBatchResultLines(logDirectory, logPattern string, files []canonicalBatchFileRef, results []wallet.BatchImportResult) (wallet.AccountSummary, []string) {
	var summary wallet.AccountSummary
	failures := 0
	imported := 0
	alreadyImported := 0
	resultLines := make([]string, 0, len(results)+2)
	for _, result := range results {
		name := "batch"
		if result.Index >= 0 && result.Index < len(files) {
			name = files[result.Index].name
		}
		if result.Err != nil {
			failures++
			resultLines = append(resultLines, localization.T("canonical_result_error", map[string]interface{}{"Name": name, "Err": canonicalBatchFailureReason(result.Err)}))
		} else if result.AlreadyImported {
			alreadyImported++
			resultLines = append(resultLines, localization.T("canonical_result_already", map[string]interface{}{"Name": name}))
		} else if result.Summary != nil {
			imported++
			resultLines = append(resultLines, localization.T("canonical_result_imported", map[string]interface{}{"Name": name, "Address": result.Summary.Address}))
			if summary.AccountID == "" {
				summary = *result.Summary
			}
		}
	}
	resultLines = append(resultLines, localization.T("canonical_result_summary", map[string]interface{}{
		"Found": len(files), "Processed": len(results), "Imported": imported, "Already": alreadyImported, "Failed": failures}))
	if failures > 0 {
		if logDirectory == "" {
			resultLines = append(resultLines, localization.Get("canonical_warn_log_dir"))
		} else if logPath, logErr := writeCanonicalBatchFailureLog(logDirectory, logPattern, files, results); logErr != nil {
			resultLines = append(resultLines, localization.Get("canonical_warn_log_write"))
		} else {
			resultLines = append(resultLines, localization.T("canonical_failure_log", map[string]interface{}{"Path": logPath}))
		}
	}
	return summary, resultLines
}

func writeCanonicalBatchFailureLog(directory, pattern string, files []canonicalBatchFileRef, results []wallet.BatchImportResult) (string, error) {
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	fail := func(cause error) (string, error) {
		_ = file.Close()
		_ = os.Remove(path)
		return "", cause
	}
	if err := file.Chmod(0o600); err != nil {
		return fail(err)
	}
	encoder := json.NewEncoder(file)
	summary := struct {
		Type            string `json:"type"`
		Total           int    `json:"total"`
		Imported        int    `json:"imported"`
		AlreadyImported int    `json:"already_imported"`
		Failed          int    `json:"failed"`
	}{Type: "summary", Total: len(files)}
	for _, result := range results {
		switch {
		case result.Err != nil:
			summary.Failed++
		case result.AlreadyImported:
			summary.AlreadyImported++
		default:
			summary.Imported++
		}
	}
	if err := encoder.Encode(summary); err != nil {
		return fail(err)
	}
	record := struct {
		Type   string `json:"type"`
		Path   string `json:"path,omitempty"`
		Reason string `json:"reason"`
	}{Type: "failure"}
	for _, result := range results {
		if result.Err == nil {
			continue
		}
		record.Path = ""
		if result.Index >= 0 && result.Index < len(files) {
			record.Path = files[result.Index].path
		}
		record.Reason = canonicalBatchFailureReason(result.Err)
		if err := encoder.Encode(record); err != nil {
			return fail(err)
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func readCanonicalMnemonicBatch(directory string) (items []wallet.MnemonicBatchItem, err error) {
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
			clearCanonicalMnemonicItems(items)
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
	byName := make(map[string]os.DirEntry, len(entries))
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".mnemonic" && extension != ".seedphrase" && extension != ".phrase" {
			continue
		}
		names = append(names, entry.Name())
		byName[entry.Name()] = entry
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("batch directory contains no mnemonic files")
	}
	if len(names) > canonicalBatchLimit {
		return nil, fmt.Errorf("batch exceeds %d files", canonicalBatchLimit)
	}
	sort.Strings(names)
	items = make([]wallet.MnemonicBatchItem, 0, len(names))
	for _, fileName := range names {
		item := wallet.MnemonicBatchItem{
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
		data, readErr := readCanonicalFileAt(directoryFile, fileName, entryInfo, wallet.MaxMnemonicImportBytes)
		if readErr != nil {
			item.PreflightErr = fmt.Errorf("%w: %v", errCanonicalBatchSourceRead, readErr)
			items = append(items, item)
			continue
		}
		item.Mnemonic = data
		items = append(items, item)
	}
	return items, nil
}

func clearCanonicalMnemonicItems(items []wallet.MnemonicBatchItem) {
	for index := range items {
		clear(items[index].Mnemonic)
	}
}
