package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestNewEnhancedFilePickerDemo(t *testing.T) {
	demo := NewEnhancedFilePickerDemo()

	assert.NotNil(t, demo)
	assert.True(t, demo.picker.MultiSelect)
	assert.True(t, demo.picker.DirAllowed)
	assert.True(t, demo.picker.FileAllowed)
	assert.Equal(t, []string{".json"}, demo.picker.AllowedTypes)
	assert.False(t, demo.finished)
	assert.Nil(t, demo.result)
}

func TestEnhancedFilePickerDemoInit(t *testing.T) {
	demo := NewEnhancedFilePickerDemo()
	cmd := demo.Init()

	assert.NotNil(t, cmd)
}

func TestEnhancedFilePickerDemoUpdate(t *testing.T) {
	demo := NewEnhancedFilePickerDemo()

	// Test normal update
	_, _ = demo.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.NotNil(t, demo)
	assert.Equal(t, demo, demo)

	// Test confirmation
	demo.picker.confirmed = true
	_, _ = demo.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.True(t, demo.finished)
	assert.NotNil(t, demo.result)
	assert.True(t, demo.result.Confirmed)

	// Test finished state
	_, cmd := demo.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, cmd) // Should return tea.Quit command
}

func TestEnhancedFilePickerDemoView(t *testing.T) {
	demo := NewEnhancedFilePickerDemo()

	// Test normal view
	view := demo.View()
	assert.Contains(t, view, "Enhanced File Picker Demo")
	assert.Contains(t, view, "keystore files")

	// Test finished view
	demo.finished = true
	demo.result = &FilePickerResult{
		Files:     []string{"/path/to/wallet.json"},
		Confirmed: true,
	}

	view = demo.View()
	assert.Contains(t, view, "Selection Result")
	assert.Contains(t, view, "Selection confirmed")
	assert.Contains(t, view, "wallet.json")
}

func TestEnhancedFilePickerDemoGetters(t *testing.T) {
	demo := NewEnhancedFilePickerDemo()

	// Test initial state
	assert.Empty(t, demo.GetSelectedFiles())
	assert.Empty(t, demo.GetSelectedDirectory())
	assert.False(t, demo.IsFinished())
	assert.False(t, demo.WasConfirmed())
	assert.False(t, demo.WasCancelled())

	// Test with result
	demo.result = &FilePickerResult{
		Files:     []string{"/path/to/file1.json", "/path/to/file2.json"},
		Directory: "/path/to/dir",
		Confirmed: true,
	}
	demo.finished = true

	assert.Equal(t, []string{"/path/to/file1.json", "/path/to/file2.json"}, demo.GetSelectedFiles())
	assert.Equal(t, "/path/to/dir", demo.GetSelectedDirectory())
	assert.True(t, demo.IsFinished())
	assert.True(t, demo.WasConfirmed())
	assert.False(t, demo.WasCancelled())

	// Test cancelled state
	demo.result.Confirmed = false
	demo.result.Cancelled = true

	assert.False(t, demo.WasConfirmed())
	assert.True(t, demo.WasCancelled())
}
