package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Test that when the focus is on a non-search field (Name), typing updates the input value.
func TestAddNetwork_TypingIntoNameFieldUpdatesValue(t *testing.T) {
	c := NewAddNetworkComponent()

	// Move focus to Name field (index 1) and apply focus states
	c.focusIndex = 1
	c.updateFocus()

	if !c.nameInput.Focused() {
		t.Fatalf("expected nameInput to be focused")
	}

	// Type a single rune 'X'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}}
	_, _ = c.Update(msg)

	if got := c.nameInput.Value(); got != "X" {
		t.Fatalf("nameInput value = %q, want %q", got, "X")
	}
}
