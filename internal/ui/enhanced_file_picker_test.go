package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEnhancedFilePicker(t *testing.T) {
	picker := NewEnhancedFilePicker()

	assert.Equal(t, ".", picker.CurrentDirectory)
	assert.True(t, picker.MultiSelect)
	assert.True(t, picker.DirAllowed)
	assert.True(t, picker.FileAllowed)
	assert.Equal(t, []string{".json"}, picker.AllowedTypes)
	assert.Empty(t, picker.SelectedFiles)
	assert.NotNil(t, picker.selectedItems)
}

func TestEnhancedFilePickerSetHeight(t *testing.T) {
	picker := NewEnhancedFilePicker()
	picker.SetHeight(15)

	assert.Equal(t, 15, picker.height)
}

func TestEnhancedFilePickerSetFileFilter(t *testing.T) {
	picker := NewEnhancedFilePicker()

	filter := func(filename string) bool {
		return filepath.Ext(filename) == ".json"
	}

	picker.SetFileFilter(filter)
	assert.NotNil(t, picker.FileFilter)
}

func TestEnhancedFilePickerSetAllowedTypes(t *testing.T) {
	picker := NewEnhancedFilePicker()
	types := []string{".json", ".txt", ".md"}

	picker.SetAllowedTypes(types)
	assert.Equal(t, types, picker.AllowedTypes)
}

func TestCanSelectFile(t *testing.T) {
	picker := NewEnhancedFilePicker()

	tests := []struct {
		name     string
		filename string
		types    []string
		filter   func(string) bool
		expected bool
	}{
		{
			name:     "JSON file with default types",
			filename: "wallet.json",
			types:    []string{".json"},
			expected: true,
		},
		{
			name:     "TXT file with JSON types",
			filename: "readme.txt",
			types:    []string{".json"},
			expected: false,
		},
		{
			name:     "JSON file with multiple types",
			filename: "config.json",
			types:    []string{".json", ".txt"},
			expected: true,
		},
		{
			name:     "Case insensitive extension",
			filename: "WALLET.JSON",
			types:    []string{".json"},
			expected: true,
		},
		{
			name:     "Empty types allows all",
			filename: "any.file",
			types:    []string{},
			expected: true,
		},
		{
			name:     "Custom filter overrides types",
			filename: "test.txt",
			types:    []string{".json"},
			filter: func(name string) bool {
				return filepath.Ext(name) == ".txt"
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			picker.SetAllowedTypes(tt.types)
			if tt.filter != nil {
				picker.SetFileFilter(tt.filter)
			} else {
				picker.FileFilter = nil
			}

			result := picker.canSelectFile(tt.filename)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToggleSelection(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "enhanced_picker_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Create test files
	testFile := filepath.Join(tempDir, "test.json")
	err = os.WriteFile(testFile, []byte("{}"), 0644)
	require.NoError(t, err)

	testDir := filepath.Join(tempDir, "subdir")
	err = os.Mkdir(testDir, 0755)
	require.NoError(t, err)

	picker := NewEnhancedFilePicker()
	picker.CurrentDirectory = tempDir

	// Simulate reading directory
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	picker.files = entries

	// Test selecting a file
	picker.cursor = 0 // Assuming first entry is the JSON file
	if picker.files[0].Name() == "test.json" {
		picker.ToggleSelection()

		expectedPath := filepath.Join(tempDir, "test.json")
		assert.True(t, picker.IsSelected(expectedPath))
		assert.Contains(t, picker.SelectedFiles, expectedPath)

		// Test deselecting
		picker.ToggleSelection()
		assert.False(t, picker.IsSelected(expectedPath))
		assert.NotContains(t, picker.SelectedFiles, expectedPath)
	}
}

func TestSelectAll(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "enhanced_picker_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Create test files
	files := []string{"wallet1.json", "wallet2.json", "readme.txt"}
	for _, file := range files {
		err = os.WriteFile(filepath.Join(tempDir, file), []byte("test"), 0644)
		require.NoError(t, err)
	}

	picker := NewEnhancedFilePicker()
	picker.CurrentDirectory = tempDir

	// Simulate reading directory
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	picker.files = entries

	picker.SelectAll()

	// Should select only JSON files based on default filter
	assert.Equal(t, 2, len(picker.SelectedFiles))

	expectedFiles := []string{
		filepath.Join(tempDir, "wallet1.json"),
		filepath.Join(tempDir, "wallet2.json"),
	}

	for _, expectedFile := range expectedFiles {
		assert.Contains(t, picker.SelectedFiles, expectedFile)
		assert.True(t, picker.IsSelected(expectedFile))
	}
}

func TestClearAll(t *testing.T) {
	picker := NewEnhancedFilePicker()

	// Add some selections
	picker.SelectedFiles = []string{"/path/to/file1.json", "/path/to/file2.json"}
	picker.selectedItems["/path/to/file1.json"] = true
	picker.selectedItems["/path/to/file2.json"] = true
	picker.SelectedDir = "/path/to/dir"

	picker.ClearAll()

	assert.Empty(t, picker.SelectedFiles)
	assert.Empty(t, picker.selectedItems)
	assert.Empty(t, picker.SelectedDir)
}

func TestKeyboardNavigation(t *testing.T) {
	picker := NewEnhancedFilePicker()
	picker.files = make([]os.DirEntry, 10) // Mock 10 files
	picker.height = 10

	// Test down navigation
	initialCursor := picker.cursor
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, initialCursor+1, picker.cursor)

	// Test up navigation
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, initialCursor, picker.cursor)

	// Test go to top
	picker.cursor = 5
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	assert.Equal(t, 0, picker.cursor)

	// Test go to last
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	assert.Equal(t, len(picker.files)-1, picker.cursor)
}

func TestMultiSelectKeyboardActions(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "enhanced_picker_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Create test files
	err = os.WriteFile(filepath.Join(tempDir, "test.json"), []byte("{}"), 0644)
	require.NoError(t, err)

	picker := NewEnhancedFilePicker()
	picker.CurrentDirectory = tempDir

	// Simulate reading directory
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	picker.files = entries

	// Test space key for selection
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeySpace})
	expectedPath := filepath.Join(tempDir, "test.json")
	assert.True(t, picker.IsSelected(expectedPath))

	// Test Ctrl+C for clear all
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.False(t, picker.IsSelected(expectedPath))
	assert.Empty(t, picker.SelectedFiles)
}

func TestConfirmAndCancel(t *testing.T) {
	picker := NewEnhancedFilePicker()

	// Test confirm
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.True(t, picker.IsConfirmed())

	// Reset and test cancel
	picker.confirmed = false
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	assert.True(t, picker.IsCancelled())
}

func TestGetResult(t *testing.T) {
	picker := NewEnhancedFilePicker()
	picker.SelectedFiles = []string{"/path/to/file.json"}
	picker.SelectedDir = "/path/to/dir"
	picker.confirmed = true

	result := picker.GetResult()
	assert.Equal(t, picker.SelectedFiles, result.Files)
	assert.Equal(t, picker.SelectedDir, result.Directory)
	assert.True(t, result.Confirmed)
	assert.False(t, result.Cancelled)
}

func TestViewportUpdate(t *testing.T) {
	picker := NewEnhancedFilePicker()
	picker.files = make([]os.DirEntry, 20) // Mock 20 files
	picker.height = 10                     // Small height to test viewport

	picker.updateViewport()

	// Should show first portion of files
	assert.Equal(t, 0, picker.viewportStart)
	assert.True(t, picker.viewportEnd < len(picker.files))

	// Move cursor beyond viewport
	picker.cursor = 15
	picker.updateViewport()

	// Viewport should adjust to show cursor
	assert.True(t, picker.cursor >= picker.viewportStart)
	assert.True(t, picker.cursor <= picker.viewportEnd)
}

func TestContainsPath(t *testing.T) {
	picker := NewEnhancedFilePicker()

	paths := []string{"/path/to/file1.json", "/path/to/file2.json"}

	assert.True(t, picker.containsPath(paths, "/path/to/file1.json"))
	assert.False(t, picker.containsPath(paths, "/path/to/file3.json"))
}

func TestDirectoryNavigation(t *testing.T) {
	// Create a temporary directory structure for testing
	tempDir, err := os.MkdirTemp("", "enhanced_picker_nav_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Create subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	err = os.Mkdir(subDir, 0755)
	require.NoError(t, err)

	picker := NewEnhancedFilePicker()
	picker.CurrentDirectory = tempDir

	// Simulate reading directory
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	picker.files = entries

	// Navigate into subdirectory
	if len(picker.files) > 0 && picker.files[0].IsDir() {
		originalDir := picker.CurrentDirectory
		picker.cursor = 0

		// Simulate Enter key to navigate into directory
		picker, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

		// Should have changed directory
		assert.NotEqual(t, originalDir, picker.CurrentDirectory)
		assert.NotNil(t, cmd) // Should return a command to read the new directory
	}
}
