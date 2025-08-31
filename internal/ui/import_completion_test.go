package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"blocowallet/internal/wallet"
)

func TestNewImportCompletionModel(t *testing.T) {
	startTime := time.Now().Add(-5 * time.Second)

	summary := wallet.ImportSummary{
		TotalFiles:        3,
		SuccessfulImports: 2,
		FailedImports:     1,
		SkippedImports:    0,
		Errors: []wallet.ImportError{
			{
				File:    "failed.json",
				Error:   fmt.Errorf("invalid password"),
				Skipped: false,
			},
		},
	}

	results := []wallet.ImportResult{
		{
			Job:     wallet.ImportJob{KeystorePath: "success1.json"},
			Success: true,
			Skipped: false,
		},
		{
			Job:     wallet.ImportJob{KeystorePath: "success2.json"},
			Success: true,
			Skipped: false,
		},
		{
			Job:     wallet.ImportJob{KeystorePath: "failed.json"},
			Success: false,
			Skipped: false,
		},
	}

	model := NewImportCompletionModel(summary, results, startTime, Styles{})

	assert.Equal(t, summary, model.summary)
	assert.Equal(t, results, model.results)
	assert.Equal(t, startTime, model.startTime)
	assert.False(t, model.showingErrors)
	assert.Equal(t, 0, model.selectedAction)
	assert.True(t, len(model.availableActions) > 0)
}

func TestImportCompletionModel_InitializeActions(t *testing.T) {
	tests := []struct {
		name            string
		summary         wallet.ImportSummary
		expectedActions []string
	}{
		{
			name: "successful import only",
			summary: wallet.ImportSummary{
				TotalFiles:        2,
				SuccessfulImports: 2,
				FailedImports:     0,
				SkippedImports:    0,
				Errors:            []wallet.ImportError{},
			},
			expectedActions: []string{"ENTER", "S"}, // Return to menu, Select different files
		},
		{
			name: "with failed imports",
			summary: wallet.ImportSummary{
				TotalFiles:        3,
				SuccessfulImports: 2,
				FailedImports:     1,
				SkippedImports:    0,
				Errors: []wallet.ImportError{
					{File: "failed.json", Error: fmt.Errorf("error"), Skipped: false},
				},
			},
			expectedActions: []string{"ENTER", "S", "F", "A", "E"}, // + Retry failed, Retry all, View errors
		},
		{
			name: "with skipped imports",
			summary: wallet.ImportSummary{
				TotalFiles:        3,
				SuccessfulImports: 2,
				FailedImports:     0,
				SkippedImports:    1,
				Errors: []wallet.ImportError{
					{File: "skipped.json", Error: fmt.Errorf("skipped"), Skipped: true},
				},
			},
			expectedActions: []string{"ENTER", "S", "K", "A", "E"}, // + Retry skipped, Retry all, View errors
		},
		{
			name: "with both failed and skipped",
			summary: wallet.ImportSummary{
				TotalFiles:        4,
				SuccessfulImports: 2,
				FailedImports:     1,
				SkippedImports:    1,
				Errors: []wallet.ImportError{
					{File: "failed.json", Error: fmt.Errorf("error"), Skipped: false},
					{File: "skipped.json", Error: fmt.Errorf("skipped"), Skipped: true},
				},
			},
			expectedActions: []string{"ENTER", "S", "F", "K", "A", "E"}, // All actions
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewImportCompletionModel(tt.summary, []wallet.ImportResult{}, time.Now(), Styles{})

			// Check that we have the expected number of actions
			assert.Equal(t, len(tt.expectedActions), len(model.availableActions))

			// Check that all expected action keys are present
			actionKeys := make([]string, len(model.availableActions))
			for i, action := range model.availableActions {
				actionKeys[i] = action.Key
			}

			for _, expectedKey := range tt.expectedActions {
				assert.Contains(t, actionKeys, expectedKey, "Expected action key %s not found", expectedKey)
			}
		})
	}
}

func TestImportCompletionModel_KeyboardNavigation(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        3,
		SuccessfulImports: 1,
		FailedImports:     1,
		SkippedImports:    1,
		Errors: []wallet.ImportError{
			{File: "failed.json", Error: fmt.Errorf("error"), Skipped: false},
			{File: "skipped.json", Error: fmt.Errorf("skipped"), Skipped: true},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	// Test navigation down
	initialSelection := model.selectedAction
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, initialSelection+1, model.selectedAction)

	// Test navigation up
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, initialSelection, model.selectedAction)

	// Test navigation at boundaries
	model.selectedAction = 0
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, model.selectedAction) // Should stay at 0

	model.selectedAction = len(model.availableActions) - 1
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, len(model.availableActions)-1, model.selectedAction) // Should stay at max
}

func TestImportCompletionModel_ActionExecution(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        2,
		SuccessfulImports: 1,
		FailedImports:     1,
		SkippedImports:    0,
		Errors: []wallet.ImportError{
			{File: "failed.json", Error: fmt.Errorf("error"), Skipped: false},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	// Test retry failed action
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	assert.NotNil(t, cmd)

	// Execute the command and check the message
	msg := cmd()
	retryMsg, ok := msg.(RetryImportMsg)
	require.True(t, ok)
	assert.Equal(t, "retry_failed", retryMsg.Strategy)

	// Test return to menu action
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.NotNil(t, cmd)

	msg = cmd()
	_, ok = msg.(ReturnToMenuMsg)
	assert.True(t, ok)
}

func TestImportCompletionModel_ErrorDetailsView(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        2,
		SuccessfulImports: 1,
		FailedImports:     1,
		SkippedImports:    0,
		Errors: []wallet.ImportError{
			{File: "failed1.json", Error: fmt.Errorf("password error"), Skipped: false},
			{File: "failed2.json", Error: fmt.Errorf("format error"), Skipped: false},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	// Enter error details view
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.True(t, model.showingErrors)
	assert.Equal(t, 0, model.errorIndex)

	// Navigate between errors
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, model.errorIndex)

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, model.errorIndex)

	// Test boundary navigation
	model.errorIndex = 0
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, model.errorIndex) // Should stay at 0

	model.errorIndex = len(summary.Errors) - 1
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, len(summary.Errors)-1, model.errorIndex) // Should stay at max

	// Exit error details view
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, model.showingErrors)
	assert.Equal(t, 0, model.errorIndex)
}

func TestImportCompletionModel_GetRetryableFiles(t *testing.T) {
	results := []wallet.ImportResult{
		{
			Job:     wallet.ImportJob{KeystorePath: "success.json"},
			Success: true,
			Skipped: false,
		},
		{
			Job:     wallet.ImportJob{KeystorePath: "failed.json"},
			Success: false,
			Skipped: false,
		},
		{
			Job:     wallet.ImportJob{KeystorePath: "skipped.json"},
			Success: false,
			Skipped: true,
		},
	}

	model := NewImportCompletionModel(wallet.ImportSummary{}, results, time.Now(), Styles{})

	// Test retry failed strategy
	failedFiles := model.GetRetryableFiles("retry_failed")
	assert.Equal(t, []string{"failed.json"}, failedFiles)

	// Test retry skipped strategy
	skippedFiles := model.GetRetryableFiles("retry_skipped")
	assert.Equal(t, []string{"skipped.json"}, skippedFiles)

	// Test retry all strategy
	allFiles := model.GetRetryableFiles("retry_all")
	assert.ElementsMatch(t, []string{"failed.json", "skipped.json"}, allFiles)

	// Test unknown strategy
	unknownFiles := model.GetRetryableFiles("unknown")
	assert.Empty(t, unknownFiles)
}

func TestImportCompletionModel_SuggestedActions(t *testing.T) {
	model := NewImportCompletionModel(wallet.ImportSummary{}, []wallet.ImportResult{}, time.Now(), Styles{})

	tests := []struct {
		name          string
		err           wallet.ImportError
		expectedCount int
		shouldContain string
	}{
		{
			name: "password error",
			err: wallet.ImportError{
				File:    "test.json",
				Error:   fmt.Errorf("invalid password"),
				Skipped: false,
			},
			expectedCount: 3,
			shouldContain: "password",
		},
		{
			name: "skipped file",
			err: wallet.ImportError{
				File:    "test.json",
				Error:   fmt.Errorf("user cancelled"),
				Skipped: true,
			},
			expectedCount: 2,
			shouldContain: "skipped",
		},
		{
			name: "format error",
			err: wallet.ImportError{
				File:    "test.json",
				Error:   fmt.Errorf("invalid format"),
				Skipped: false,
			},
			expectedCount: 2,
			shouldContain: "format",
		},
		{
			name: "permission error",
			err: wallet.ImportError{
				File:    "test.json",
				Error:   fmt.Errorf("permission denied"),
				Skipped: false,
			},
			expectedCount: 2,
			shouldContain: "permission",
		},
		{
			name: "generic error",
			err: wallet.ImportError{
				File:    "test.json",
				Error:   fmt.Errorf("unknown error"),
				Skipped: false,
			},
			expectedCount: 2,
			shouldContain: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions := model.getSuggestedActions(tt.err)
			assert.Equal(t, tt.expectedCount, len(suggestions))

			// Check that at least one suggestion contains the expected text
			found := false
			for _, suggestion := range suggestions {
				if strings.Contains(strings.ToLower(suggestion), tt.shouldContain) {
					found = true
					break
				}
			}
			assert.True(t, found, "Expected to find suggestion containing '%s'", tt.shouldContain)
		})
	}
}

func TestImportCompletionModel_WrapText(t *testing.T) {
	model := NewImportCompletionModel(wallet.ImportSummary{}, []wallet.ImportResult{}, time.Now(), Styles{})

	tests := []struct {
		name     string
		text     string
		width    int
		expected string
	}{
		{
			name:     "short text",
			text:     "short",
			width:    10,
			expected: "short",
		},
		{
			name:     "exact width",
			text:     "exactly10c",
			width:    10,
			expected: "exactly10c",
		},
		{
			name:     "wrap needed",
			text:     "this is a long text that needs wrapping",
			width:    10,
			expected: "this is a\nlong text\nthat needs\nwrapping",
		},
		{
			name:     "single long word",
			text:     "verylongwordthatneedswrapping",
			width:    10,
			expected: "verylongwordthatneedswrapping", // Single word, no wrapping
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := model.wrapText(tt.text, tt.width)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImportCompletionModel_View(t *testing.T) {
	summary := wallet.ImportSummary{
		TotalFiles:        3,
		SuccessfulImports: 2,
		FailedImports:     1,
		SkippedImports:    0,
		Errors: []wallet.ImportError{
			{File: "failed.json", Error: fmt.Errorf("error"), Skipped: false},
		},
	}

	model := NewImportCompletionModel(summary, []wallet.ImportResult{}, time.Now(), Styles{})

	// Test main completion view
	view := model.View()
	assert.Contains(t, view, "Import Completed")
	assert.Contains(t, view, "Total: 3")
	assert.Contains(t, view, "Success: 2")
	assert.Contains(t, view, "Failed: 1")
	assert.Contains(t, view, "Available Actions")

	// Test error details view
	model.showingErrors = true
	errorView := model.View()
	assert.Contains(t, errorView, "Error Details")
	assert.Contains(t, errorView, "Error 1 of 1")
	assert.Contains(t, errorView, "failed.json")
}

func TestImportCompletionModel_GettersAndSetters(t *testing.T) {
	startTime := time.Now().Add(-5 * time.Second)
	summary := wallet.ImportSummary{TotalFiles: 3}
	results := []wallet.ImportResult{{Success: true}}

	model := NewImportCompletionModel(summary, results, startTime, Styles{})

	// Test getters
	assert.Equal(t, summary, model.GetSummary())
	assert.Equal(t, results, model.GetResults())
	assert.True(t, model.GetElapsedTime() > 0)
	assert.False(t, model.IsShowingErrors())
	assert.Equal(t, CompletionActionReturnToMenu, model.GetSelectedAction())

	// Test state changes
	model.showingErrors = true
	assert.True(t, model.IsShowingErrors())
}
