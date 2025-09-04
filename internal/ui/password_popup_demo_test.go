package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestNewPasswordPopupDemo(t *testing.T) {
	demo := NewPasswordPopupDemo()

	assert.False(t, demo.finished)
	assert.Nil(t, demo.result)
	assert.Equal(t, "demo-wallet.json", demo.popup.keystoreFile)
}

func TestPasswordPopupDemo_Init(t *testing.T) {
	demo := NewPasswordPopupDemo()

	cmd := demo.Init()
	assert.NotNil(t, cmd)
}

func TestPasswordPopupDemo_Update_NotFinished(t *testing.T) {
	demo := NewPasswordPopupDemo()

	// Send a key to the popup
	model, cmd := demo.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	demoModel := model.(PasswordPopupDemo)
	assert.False(t, demoModel.finished)
	assert.NotNil(t, cmd)
}

func TestPasswordPopupDemo_Update_Finished_Quit(t *testing.T) {
	demo := NewPasswordPopupDemo()
	demo.finished = true

	// Send quit command
	model, cmd := demo.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	demoModel := model.(PasswordPopupDemo)
	assert.True(t, demoModel.finished)
	assert.NotNil(t, cmd) // Should be tea.Quit
}

func TestPasswordPopupDemo_Update_Finished_Reset(t *testing.T) {
	demo := NewPasswordPopupDemo()
	demo.finished = true
	result := PasswordPopupResult{Password: "test"}
	demo.result = &result

	// Send reset command
	model, cmd := demo.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	demoModel := model.(PasswordPopupDemo)
	assert.False(t, demoModel.finished)
	assert.Nil(t, demoModel.result)
	assert.Equal(t, "another-wallet.json", demoModel.popup.keystoreFile)
	assert.NotNil(t, cmd)
}

func TestPasswordPopupDemo_Update_PopupCompleted(t *testing.T) {
	demo := NewPasswordPopupDemo()

	// Simulate popup completion by setting the popup state
	demo.popup.Model.SetValue("testpassword")
	demo.popup.confirmed = true

	// Update should detect completion
	model, _ := demo.Update(tea.KeyMsg{Type: tea.KeyEnter})

	demoModel := model.(PasswordPopupDemo)
	assert.True(t, demoModel.finished)
	assert.NotNil(t, demoModel.result)
	assert.Equal(t, "testpassword", demoModel.result.Password)
	assert.False(t, demoModel.result.Cancelled)
}

func TestPasswordPopupDemo_View_NotFinished(t *testing.T) {
	demo := NewPasswordPopupDemo()

	view := demo.View()

	// Should show the popup view
	assert.Contains(t, view, "Password Required")
	assert.Contains(t, view, "demo-wallet.json")
}

func TestPasswordPopupDemo_View_Finished_Success(t *testing.T) {
	demo := NewPasswordPopupDemo()
	demo.finished = true
	demo.result = &PasswordPopupResult{
		Password:  "mypassword",
		Cancelled: false,
	}

	view := demo.View()

	assert.Contains(t, view, "✅ Password entered: mypassword")
	assert.Contains(t, view, "Press 'r' to reset")
	assert.Contains(t, view, "Press 'q' to quit")
}

func TestPasswordPopupDemo_View_Finished_Cancelled(t *testing.T) {
	demo := NewPasswordPopupDemo()
	demo.finished = true
	demo.result = &PasswordPopupResult{
		Cancelled: true,
		Skip:      true,
	}

	view := demo.View()

	assert.Contains(t, view, "❌ Password input was cancelled/skipped")
	assert.Contains(t, view, "Press 'r' to reset")
	assert.Contains(t, view, "Press 'q' to quit")
}

func TestPasswordPopupDemo_Integration_FullFlow(t *testing.T) {
	demo := NewPasswordPopupDemo()

	// Initial state
	assert.False(t, demo.finished)
	assert.Nil(t, demo.result)

	// Enter password
	demo.popup.Model.SetValue("integration-test")
	model, _ := demo.Update(tea.KeyMsg{Type: tea.KeyEnter})
	demo = model.(PasswordPopupDemo)

	// Should be finished with result
	assert.True(t, demo.finished)
	assert.NotNil(t, demo.result)
	assert.Equal(t, "integration-test", demo.result.Password)

	// Reset
	model, _ = demo.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	demo = model.(PasswordPopupDemo)

	// Should be reset
	assert.False(t, demo.finished)
	assert.Nil(t, demo.result)
	assert.Equal(t, "another-wallet.json", demo.popup.keystoreFile)
}
