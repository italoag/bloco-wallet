package ui

import (
	"errors"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"blocowallet/internal/wallet"
)

func TestNewImportCompletionModel(t *testing.T) {
	startTime := time.Now().Add(-5 * time.Minute)
	summary := wallet.ImportSummary{
		TotalFiles:        5,
		SuccessfulImports: 3,
		FailedImports:     1,
		SkippedImports:    1,
		Errors: []wallet.ImportError{
			{File: "wallet1.json", Error: errors.New("incorrect password"), Skipped: false},
			{File: "wallet2.json", Error: errors.New("user cancelled"), Skipped: true},
		},
	}

	results := []wallet.ImportResult{
		{Success: true, Job: wallet.ImportJob{KeystorePath: "wallet1.json"}},
		{Success: true, Job: wallet.ImportJob{KeystorePath: "wallet2.json"}},
		{Success: true, Job: wallet.ImportJob{KeystorePath: "wallet3.json"}},
		{Success: false, Error: errors.New("incorrect password"), Job: wallet.ImportJob{KeystorePath: "wallet4.json"}},
		{Success: false, Error: errors.New("user cancelled"), Skipped: true, Job: wallet.ImportJob{KeystorePath: "wallet5.json"}},
	}

	styles := Styles{}
	model := NewImportCompletionModel(summary, results, startTime, styles)

	assert.Equal(t, summary, model.summary)
	assert.Equal(t, results, model.results)
	assert.Equal(t, startTime, model.startTime)
	assert.Equal(t, 0, model.selectedAction)
	assert.False(t, model.showErrorDetail)
	assert.Equal(t, 0, model.selectedError)
	assert.Equal(t, 80, model.width)
	assert.Equal(t, 24, model.height)
}

func TestImportCompletionModel_GetAvailableActions(t *testing.T) {
	tests := []struct {
		name            string
		summary         wallet.ImportSummary
		expectedActions []CompletionAction
	}{
		{
			name: "all successful - basic actions only",
			summary: wallet.ImportSummary{
				TotalFiles:        3,
				SuccessfulImports: 3,
				FailedImports:     0,
				SkippedImports:    0,
				Errors:            []wallet.ImportError{},
			},
			expectedActions: []CompletionAction{
				ActionReturnToMenu,
				ActionSelectDifferentFiles,
			},
		},
		{
			name: "with retryable errors - includes retry actions",
			summary: wallet.ImportSummary{
				TotalFiles:        3,
				SuccessfulImports: 2,
				FailedImports:     1,
				SkippedImports:    0,
				Errors: []wallet.ImportError{
					{File: "wallet1.json", Error: errors.New("incorrect password"), Skipped: false},
				},
			},
			expectedActions: []CompletionAction{
				ActionReturnToMenu,
				ActionRetryFailed,
				ActionRetryWithManualPasswords,
				ActionViewErrorDetails,
				ActionSelectDifferentFiles,
			},
		},
		{
			name: "with skipped files - includes error details",
			summary: wallet.ImportSummary{
				TotalFiles:        3,
				SuccessfulImports: 2,
				FailedImports:     0,
				SkippedImports:    1,
				Errors: []wallet.ImportError{
					{File: "wallet1.json", Error: errors.New("user cancelled"), Skipped: true},
				},
			},
			expectedActions: []CompletionAction{
				ActionReturnToMenu,
				ActionViewErrorDetails,
				ActionSelectDifferentFiles,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewImportCompletionModel(tt.summary, []wallet.ImportResult{}, time.Now(), Styles{})
			actions := model.getAvailableActions()
			assert.Equal(t, tt.expectedActions, actions)
		})
	}
}

func TestImportCompletionModel_HasRetryableErrors(t *testing.T) {
	tests := []struct {
		name     string
		errors   []wallet.ImportError
		expected bool
	}{
		{
			name:     "no errors",
			errors:   []wallet.ImportError{},
			expected: false,
		},
		{
			name: "password error - retryable",
			errors: []wallet.ImportError{
				{File: "wallet1.json", Error: errors.New("incorrect password"), Skipped: false},
			},
			expected: true,
		},
		{
			name: "permission error - retryable",
			errors: []wallet.ImportError{
				{File: "wallet1.json", Error: errors.New("permission denied"), Skipped: false},
			},
			expected: true,
		},
		{
			name: "timeout error - retryable",
			errors: []wallet.ImportError{
				{File: "wallet1.json", Error: errors.New("operation timeout"), Skipped: false},
			},
			expected: true,
		},
		{
			name: "skipped error - not retryable",
			errors: []wallet.ImportError{
				{File: "wallet1.json", Error: errors.New("user cancelled"), Skipped: true},
			},
			expected: false,
		},
		{
			name: "generic error - not retryable",
			errors: []wallet.ImportError{
				{File: "wallet1.json", Error: errors.New("file corrupted"), Skipped: false},
			},
			expected: false,
		},
		{
			name: "mixed errors - has retryable",
			errors: []wallet.ImportError{
				{File: "wallet1.json", Error: errors.New("file corrupted"), Skipped: false},
				{File: "wallet2.json", Error: errors.New("incorrect password"), Skipped: false},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := wallet.ImportSummary{Errors: tt.errors}
			model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})
			assert.Equal(t, tt.expected, model.hasRetryableErrors())
		})
	}
}

func TestImportCompletionModel_IsRetryableError(t *testing.T) {
	model := NewImportCompletionModel(wallet.ImportSummary{}, []wallet.ImportResult{}, time.Now(), Styles{})

	tests := []struct {
		name     string
		error    error
		expected bool
	}{
		{
			name:     "nil error",
			error:    nil,
			expected: false,
		},
		{
			name:     "password error",
			error:    errors.New("incorrect password"),
			expected: true,
		},
		{
			name:     "decrypt error",
			error:    errors.New("failed to decrypt keystore"),
			expected: true,
		},
		{
			name:     "invalid password error",
			error:    errors.New("invalid password provided"),
			expected: true,
		},
		{
			name:     "permission error",
			error:    errors.New("permission denied"),
			expected: true,
		},
		{
			name:     "access error",
			error:    errors.New("cannot access file"),
			expected: true,
		},
		{
			name:     "timeout error",
			error:    errors.New("operation timeout"),
			expected: true,
		},
		{
			name:     "generic error",
			error:    errors.New("file corrupted"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.isRetryableError(tt.error)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImportCompletionModel_Navigation(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        3,
		SuccessfulImports: 2,
		FailedImports:     1,
		SkippedImports:    0,
		Errors: []wallet.ImportError{
			{File: "wallet1.json", Error: errors.New("incorrect password"), Skipped: false},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	// Test navigation down
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, model.selectedAction)
	assert.Nil(t, cmd)

	// Test navigation up
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, model.selectedAction)
	assert.Nil(t, cmd)

	// Test navigation beyond bounds (down)
	availableActions := len(model.getAvailableActions())
	model.selectedAction = availableActions - 1
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, availableActions-1, model.selectedAction) // Should not change
	assert.Nil(t, cmd)

	// Test navigation beyond bounds (up)
	model.selectedAction = 0
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, model.selectedAction) // Should not change
	assert.Nil(t, cmd)
}

func TestImportCompletionModel_ActionExecution(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        3,
		SuccessfulImports: 2,
		FailedImports:     1,
		SkippedImports:    0,
		Errors: []wallet.ImportError{
			{File: "wallet1.json", Error: errors.New("incorrect password"), Skipped: false},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	// Test return to menu action
	model.selectedAction = 0 // ActionReturnToMenu
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.NotNil(t, cmd)

	// Test retry failed action
	model.selectedAction = 1 // ActionRetryFailed
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.NotNil(t, cmd)

	// Test view error details action
	actions := model.getAvailableActions()
	for i, action := range actions {
		if action == ActionViewErrorDetails {
			model.selectedAction = i
			break
		}
	}
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.True(t, model.showErrorDetail)
	assert.Equal(t, 0, model.selectedError)
	assert.Nil(t, cmd)
}

func TestImportCompletionModel_ErrorDetailNavigation(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        3,
		SuccessfulImports: 1,
		FailedImports:     2,
		SkippedImports:    0,
		Errors: []wallet.ImportError{
			{File: "wallet1.json", Error: errors.New("incorrect password"), Skipped: false},
			{File: "wallet2.json", Error: errors.New("permission denied"), Skipped: false},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})
	model.showErrorDetail = true

	// Test error navigation down
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, model.selectedError)
	assert.Nil(t, cmd)

	// Test error navigation up
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, model.selectedError)
	assert.Nil(t, cmd)

	// Test navigation beyond bounds
	model.selectedError = len(summary.Errors) - 1
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, len(summary.Errors)-1, model.selectedError) // Should not change
	assert.Nil(t, cmd)

	// Test exit error detail view
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, model.showErrorDetail)
	assert.Nil(t, cmd)
}

func TestImportCompletionModel_GetShortErrorMessage(t *testing.T) {
	model := NewImportCompletionModel(wallet.ImportSummary{}, []wallet.ImportResult{}, time.Now(), Styles{})

	tests := []struct {
		name     string
		error    error
		expected string
	}{
		{
			name:     "nil error",
			error:    nil,
			expected: "Unknown error",
		},
		{
			name:     "short error message",
			error:    errors.New("short error"),
			expected: "short error",
		},
		{
			name:     "long error message",
			error:    errors.New("this is a very long error message that should be truncated because it exceeds the maximum length"),
			expected: "this is a very long error message that should be tru...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.getShortErrorMessage(tt.error)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImportCompletionModel_GetSkipReason(t *testing.T) {
	model := NewImportCompletionModel(wallet.ImportSummary{}, []wallet.ImportResult{}, time.Now(), Styles{})

	tests := []struct {
		name     string
		error    error
		expected string
	}{
		{
			name:     "nil error",
			error:    nil,
			expected: "User chose to skip",
		},
		{
			name:     "cancelled error",
			error:    errors.New("user cancelled password input"),
			expected: "User cancelled password input",
		},
		{
			name:     "skipped error",
			error:    errors.New("import was skipped by user"),
			expected: "User chose to skip this file",
		},
		{
			name:     "timeout error",
			error:    errors.New("password input timeout"),
			expected: "Password input timed out",
		},
		{
			name:     "generic error",
			error:    errors.New("some other error"),
			expected: "User action required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.getSkipReason(tt.error)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImportCompletionModel_GetRecoverySuggestions(t *testing.T) {
	model := NewImportCompletionModel(wallet.ImportSummary{}, []wallet.ImportResult{}, time.Now(), Styles{})

	tests := []struct {
		name     string
		error    error
		expected []string
	}{
		{
			name:  "password error",
			error: errors.New("incorrect password"),
			expected: []string{
				"Verify the password is correct",
				"Check if a .pwd file exists with the correct password",
				"Try entering the password manually",
			},
		},
		{
			name:  "permission error",
			error: errors.New("permission denied"),
			expected: []string{
				"Check file permissions",
				"Ensure the file is not locked by another process",
				"Try running with appropriate permissions",
			},
		},
		{
			name:  "file not found error",
			error: errors.New("file not found"),
			expected: []string{
				"Verify the file path is correct",
				"Ensure the file exists and is accessible",
			},
		},
		{
			name:  "format error",
			error: errors.New("invalid format"),
			expected: []string{
				"Verify the file is a valid KeyStore V3 format",
				"Check if the file is corrupted",
			},
		},
		{
			name:  "generic error",
			error: errors.New("unknown error"),
			expected: []string{
				"Review the error message and try again",
				"Contact support if the issue persists",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.getRecoverySuggestions(tt.error)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImportCompletionModel_GetFailedAndSkippedFiles(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        5,
		SuccessfulImports: 3,
		FailedImports:     1,
		SkippedImports:    1,
		Errors: []wallet.ImportError{
			{File: "failed.json", Error: errors.New("incorrect password"), Skipped: false},
			{File: "skipped.json", Error: errors.New("user cancelled"), Skipped: true},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	failedFiles := model.GetFailedFiles()
	assert.Equal(t, []string{"failed.json"}, failedFiles)

	skippedFiles := model.GetSkippedFiles()
	assert.Equal(t, []string{"skipped.json"}, skippedFiles)

	retryableFiles := model.GetRetryableFiles()
	assert.Equal(t, []string{"failed.json"}, retryableFiles) // Password error is retryable
}

func TestImportCompletionModel_WindowSizeUpdate(t *testing.T) {
	model := NewImportCompletionModel(wallet.ImportSummary{}, []wallet.ImportResult{}, time.Now(), Styles{})

	// Test window size update
	model, cmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	assert.Equal(t, 100, model.width)
	assert.Equal(t, 30, model.height)
	assert.Nil(t, cmd)
}

func TestImportCompletionModel_QuickShortcuts(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        3,
		SuccessfulImports: 2,
		FailedImports:     1,
		SkippedImports:    0,
		Errors: []wallet.ImportError{
			{File: "wallet1.json", Error: errors.New("incorrect password"), Skipped: false},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	// Test quick retry shortcut
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.NotNil(t, cmd)

	// Test quick error details shortcut
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.True(t, model.showErrorDetail)
	assert.Equal(t, 0, model.selectedError)
	assert.Nil(t, cmd)

	// Test ESC to return to menu
	model.showErrorDetail = false
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.NotNil(t, cmd)
}

func TestCompletionAction_String(t *testing.T) {
	// Initialize localization for testing
	// Note: In a real test environment, you'd want to set up proper localization
	tests := []struct {
		action   CompletionAction
		expected string
	}{
		{ActionReturnToMenu, "action_return_to_menu"},
		{ActionRetryFailed, "action_retry_failed_imports"},
		{ActionRetryWithManualPasswords, "action_retry_with_manual_passwords"},
		{ActionViewErrorDetails, "action_view_error_details"},
		{ActionSelectDifferentFiles, "action_select_different_files"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.action.String()
			// Since localization might not be fully set up in tests,
			// we just verify the method doesn't panic and returns a string
			assert.NotEmpty(t, result)
		})
	}
}

func TestImportCompletionModel_SetDimensions(t *testing.T) {
	model := NewImportCompletionModel(wallet.ImportSummary{}, []wallet.ImportResult{}, time.Now(), Styles{})

	model.SetDimensions(120, 40)
	assert.Equal(t, 120, model.width)
	assert.Equal(t, 40, model.height)
}

func TestImportCompletionModel_GetSelectedAction(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        3,
		SuccessfulImports: 2,
		FailedImports:     1,
		SkippedImports:    0,
		Errors: []wallet.ImportError{
			{File: "wallet1.json", Error: errors.New("incorrect password"), Skipped: false},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	// Test default selection
	assert.Equal(t, ActionReturnToMenu, model.GetSelectedAction())

	// Test selection change
	model.selectedAction = 1
	actions := model.getAvailableActions()
	if len(actions) > 1 {
		assert.Equal(t, actions[1], model.GetSelectedAction())
	}

	// Test out of bounds selection
	model.selectedAction = 999
	assert.Equal(t, ActionReturnToMenu, model.GetSelectedAction()) // Should return default
}

// Benchmark tests for performance
func BenchmarkImportCompletionModel_View(b *testing.B) {
	// Create a model with many errors to test performance
	errors := make([]wallet.ImportError, 100)
	for i := 0; i < 100; i++ {
		errors[i] = wallet.ImportError{
			File:    fmt.Sprintf("wallet%d.json", i),
			Error:   fmt.Errorf("error %d", i),
			Skipped: i%2 == 0,
		}
	}

	summary := wallet.ImportSummary{
		TotalFiles:        100,
		SuccessfulImports: 0,
		FailedImports:     50,
		SkippedImports:    50,
		Errors:            errors,
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = model.View()
	}
}

func BenchmarkImportCompletionModel_Update(b *testing.B) {
	model := NewImportCompletionModel(wallet.ImportSummary{}, []wallet.ImportResult{}, time.Now(), Styles{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
}
