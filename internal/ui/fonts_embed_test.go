package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedUserFontsCopiesEmbeddedFonts(t *testing.T) {
	dir := t.TempDir()

	written, err := seedUserFonts(dir)
	require.NoError(t, err)
	assert.Positive(t, written, "expected embedded fonts to be seeded")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, written, "seeded file count should match returned count")

	for _, entry := range entries {
		info, err := entry.Info()
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "font files must be user-only")
	}
}

func TestSeedUserFontsDoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()

	_, err := seedUserFonts(dir)
	require.NoError(t, err)

	existing := filepath.Join(dir, "1911.tdf")
	require.NoError(t, os.WriteFile(existing, []byte("user-modified"), 0600))

	written, err := seedUserFonts(dir)
	require.NoError(t, err)
	assert.Zero(t, written, "second seeding must copy nothing")

	data, err := os.ReadFile(existing)
	require.NoError(t, err)
	assert.Equal(t, "user-modified", string(data), "existing font must not be overwritten")
}
