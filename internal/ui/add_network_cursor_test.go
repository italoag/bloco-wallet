package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchInputCursorFollowsTypedText(t *testing.T) {
	component := NewAddNetworkComponent()
	component.isSearchFocused = true
	component.focusIndex = 0
	component.updateFocus()

	for _, r := range "eth" {
		component.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	assert.Equal(t, "eth", component.searchInput.Value())
	assert.Equal(t, 3, component.searchInput.Position(), "cursor must follow typed text")

	// Moving the cursor mid-text and typing must insert at the cursor.
	component.Update(tea.KeyMsg{Type: tea.KeyLeft})
	component.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.Equal(t, "etxh", component.searchInput.Value())
	assert.Equal(t, 3, component.searchInput.Position())
}

func TestSearchInputBackspaceKeepsCursorConsistent(t *testing.T) {
	component := NewAddNetworkComponent()
	component.isSearchFocused = true
	component.focusIndex = 0
	component.updateFocus()

	for _, r := range "eth" {
		component.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	component.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	require.Equal(t, "et", component.searchInput.Value())
	assert.Equal(t, 2, component.searchInput.Position())
}
