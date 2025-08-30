package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestNewPasswordPopupModel(t *testing.T) {
	keystoreFile := "test-wallet.json"
	maxRetries := 3

	model := NewPasswordPopupModel(keystoreFile, maxRetries)

	assert.Equal(t, keystoreFile, model.keystoreFile)
	assert.Equal(t, maxRetries, model.maxRetries)
	assert.Equal(t, 0, model.retryCount)
	assert.False(t, model.cancelled)
	assert.False(t, model.confirmed)
	assert.Empty(t, model.errorMessage)
	assert.Equal(t, 60, model.width)
	assert.Equal(t, 12, model.height)
}

func TestPasswordPopupModel_Update_EnterKey(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)

	// Set a password value
	model.SetValue("testpassword")

	// Send enter key
	updatedModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, updatedModel.confirmed)
	assert.False(t, updatedModel.cancelled)
	assert.NotNil(t, cmd)
}

func TestPasswordPopupModel_Update_EnterKeyEmptyPassword(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)

	// Don't set any password value (empty)

	// Send enter key
	updatedModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.False(t, updatedModel.confirmed)
	assert.False(t, updatedModel.cancelled)
	// Should not quit with empty password
}

func TestPasswordPopupModel_Update_EscapeKey(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)

	// Send escape key
	updatedModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.True(t, updatedModel.cancelled)
	assert.False(t, updatedModel.confirmed)
	assert.NotNil(t, cmd)
}

func TestPasswordPopupModel_Update_CtrlC(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)

	// Send Ctrl+C
	updatedModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	assert.True(t, updatedModel.cancelled)
	assert.False(t, updatedModel.confirmed)
	assert.NotNil(t, cmd)
}

func TestPasswordPopupModel_Update_CtrlS_Skip(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)

	// Send Ctrl+S (skip file)
	updatedModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	assert.True(t, updatedModel.cancelled)
	assert.False(t, updatedModel.confirmed)
	assert.NotNil(t, cmd)
}

func TestPasswordPopupModel_SetError(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)
	model.SetValue("wrongpassword")

	errorMsg := "Invalid password"
	model.SetError(errorMsg)

	assert.Equal(t, errorMsg, model.errorMessage)
	assert.Equal(t, 1, model.retryCount)
	assert.Empty(t, model.Value()) // Password should be cleared
}

func TestPasswordPopupModel_GetResult_Confirmed(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)
	model.SetValue("mypassword")
	model.confirmed = true

	result := model.GetResult()

	assert.Equal(t, "mypassword", result.Password)
	assert.False(t, result.Cancelled)
	assert.False(t, result.Skip)
}

func TestPasswordPopupModel_GetResult_Cancelled(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)
	model.cancelled = true

	result := model.GetResult()

	assert.Empty(t, result.Password)
	assert.True(t, result.Cancelled)
	assert.True(t, result.Skip)
}

func TestPasswordPopupModel_GetResult_NotCompleted(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)

	result := model.GetResult()

	assert.Empty(t, result.Password)
	assert.False(t, result.Cancelled)
	assert.False(t, result.Skip)
}

func TestPasswordPopupModel_IsCompleted(t *testing.T) {
	tests := []struct {
		name      string
		cancelled bool
		confirmed bool
		expected  bool
	}{
		{"not completed", false, false, false},
		{"cancelled", true, false, true},
		{"confirmed", false, true, true},
		{"both flags set", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewPasswordPopupModel("test.json", 3)
			model.cancelled = tt.cancelled
			model.confirmed = tt.confirmed

			assert.Equal(t, tt.expected, model.IsCompleted())
		})
	}
}

func TestPasswordPopupModel_HasExceededMaxRetries(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)

	// Initially should not exceed
	assert.False(t, model.HasExceededMaxRetries())

	// Add retries
	model.SetError("error 1")
	assert.False(t, model.HasExceededMaxRetries()) // 1/3

	model.SetError("error 2")
	assert.False(t, model.HasExceededMaxRetries()) // 2/3

	model.SetError("error 3")
	assert.True(t, model.HasExceededMaxRetries()) // 3/3
}

func TestPasswordPopupModel_Reset(t *testing.T) {
	model := NewPasswordPopupModel("old-file.json", 3)

	// Set some state
	model.SetValue("password")
	model.SetError("some error")
	model.cancelled = true
	model.confirmed = true

	// Reset with new file
	newFile := "new-file.json"
	model.Reset(newFile)

	assert.Equal(t, newFile, model.keystoreFile)
	assert.Empty(t, model.errorMessage)
	assert.Equal(t, 0, model.retryCount)
	assert.False(t, model.cancelled)
	assert.False(t, model.confirmed)
	assert.Empty(t, model.Value())
}

func TestPasswordPopupModel_View(t *testing.T) {
	model := NewPasswordPopupModel("test-wallet.json", 3)

	view := model.View()

	// Check that key elements are present in the view
	assert.Contains(t, view, "Password Required")
	assert.Contains(t, view, "test-wallet.json")
	assert.Contains(t, view, "Enter: Confirm")
	assert.Contains(t, view, "Esc: Cancel")
	assert.Contains(t, view, "Ctrl+S: Skip file")
}

func TestPasswordPopupModel_View_WithError(t *testing.T) {
	model := NewPasswordPopupModel("test-wallet.json", 3)
	model.SetError("Invalid password provided")

	view := model.View()

	assert.Contains(t, view, "Invalid password provided")
	assert.Contains(t, view, "Attempts remaining: 2")
}

func TestPasswordPopupModel_View_MaxRetriesReached(t *testing.T) {
	model := NewPasswordPopupModel("test-wallet.json", 2)
	model.SetError("error 1")
	model.SetError("error 2")

	view := model.View()

	assert.Contains(t, view, "Maximum attempts reached")
}

func TestPasswordPopupModel_PasswordMasking(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)

	// Verify that the textinput is configured for password masking
	assert.Equal(t, '•', model.Model.EchoCharacter)
	// Note: EchoMode is not directly accessible, but we can verify the character is set
}

func TestPasswordPopupModel_CharacterLimit(t *testing.T) {
	model := NewPasswordPopupModel("test.json", 3)

	// Verify character limit is set
	assert.Equal(t, 256, model.Model.CharLimit)
}

func TestPasswordPopupModel_Integration_FullFlow(t *testing.T) {
	model := NewPasswordPopupModel("integration-test.json", 2)

	// Test wrong password flow
	model.SetValue("wrongpassword")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.True(t, model.confirmed)

	// Simulate error response and retry
	model.confirmed = false
	model.SetError("Authentication failed")
	assert.Equal(t, 1, model.retryCount)
	assert.Empty(t, model.Value())

	// Try again with correct password
	model.SetValue("correctpassword")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.True(t, model.confirmed)

	result := model.GetResult()
	assert.Equal(t, "correctpassword", result.Password)
	assert.False(t, result.Cancelled)
}

func TestPasswordPopupModel_Integration_CancelFlow(t *testing.T) {
	model := NewPasswordPopupModel("cancel-test.json", 3)

	// Start entering password then cancel
	model.SetValue("partialpassword")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.True(t, model.cancelled)
	assert.False(t, model.confirmed)

	result := model.GetResult()
	assert.Empty(t, result.Password)
	assert.True(t, result.Cancelled)
	assert.True(t, result.Skip)
}
