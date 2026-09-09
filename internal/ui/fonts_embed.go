package ui

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// embeddedFontFS holds the repository TDF fonts so a fresh install has the
// full font catalog available before the user adds custom fonts.
//
//go:embed fonts/*.tdf
var embeddedFontFS embed.FS

// seedUserFonts copies the embedded fonts into dir, skipping files that
// already exist so user-modified fonts are never overwritten. It returns the
// number of fonts written.
func seedUserFonts(dir string) (int, error) {
	entries, err := fs.ReadDir(embeddedFontFS, "fonts")
	if err != nil {
		return 0, fmt.Errorf("failed to read embedded fonts: %w", err)
	}

	written := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		target := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		}
		data, err := embeddedFontFS.ReadFile("fonts/" + entry.Name())
		if err != nil {
			return written, fmt.Errorf("failed to read embedded font %s: %w", entry.Name(), err)
		}
		if err := writeFileSecure(target, data); err != nil {
			return written, fmt.Errorf("failed to write font %s: %w", entry.Name(), err)
		}
		written++
	}
	return written, nil
}

// writeFileSecure writes data to path with user-only permissions, using an
// atomic rename so partial files are never observed.
func writeFileSecure(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
