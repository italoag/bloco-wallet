package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const canonicalPathSuggestionLimit = 64

func canonicalExpandHome(value string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return value
	}
	if value == "~" {
		return home
	}
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~"+string(os.PathSeparator)) {
		expanded := filepath.Join(home, value[2:])
		if strings.HasSuffix(value, "/") || strings.HasSuffix(value, string(os.PathSeparator)) {
			if !strings.HasSuffix(expanded, string(os.PathSeparator)) {
				expanded += string(os.PathSeparator)
			}
		}
		return expanded
	}
	return value
}

func canonicalPathSuggestions(value string, directoriesOnly bool) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	expanded := canonicalExpandHome(value)
	var dirPart, base string
	if strings.HasSuffix(expanded, string(os.PathSeparator)) || strings.HasSuffix(expanded, "/") {
		dirPart = expanded
		base = ""
	} else {
		dirPart = filepath.Dir(expanded)
		base = filepath.Base(expanded)
	}
	directory, err := os.Open(dirPart)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = directory.Close() }()
	entries, err := directory.ReadDir(canonicalBatchDirectoryLimit)
	if err != nil && len(entries) == 0 {
		return nil, nil
	}
	homeBased := value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~"+string(os.PathSeparator))
	home, _ := os.UserHomeDir()
	slashStyle := strings.Contains(value, "/")
	suggestions := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, base) {
			continue
		}
		if strings.IndexFunc(name, unicode.IsControl) >= 0 {
			continue
		}
		entryType := entry.Type()
		if entryType&os.ModeSymlink != 0 {
			continue
		}
		var candidate string
		if entry.IsDir() {
			candidate = filepath.Join(dirPart, name) + string(os.PathSeparator)
		} else {
			if directoriesOnly {
				continue
			}
			if !entryType.IsRegular() || !strings.EqualFold(filepath.Ext(name), ".json") {
				continue
			}
			candidate = filepath.Join(dirPart, name)
		}
		var display string
		if homeBased && home != "" {
			if !strings.HasPrefix(candidate, home) {
				continue
			}
			suffix := candidate[len(home):]
			if slashStyle {
				suffix = filepath.ToSlash(suffix)
			}
			display = "~" + suffix
		} else {
			display = candidate
			if slashStyle {
				display = filepath.ToSlash(display)
			}
		}
		if !strings.HasPrefix(display, value) {
			continue
		}
		suggestions = append(suggestions, display)
		if len(suggestions) >= canonicalPathSuggestionLimit {
			break
		}
	}
	sort.Strings(suggestions)
	return suggestions, nil
}
