package ui

import (
	"errors"
	"testing"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestNewImportProgressModel(t *testing.T) {
	styles := Styles{} // Use default styles for testing
	totalFiles := 5

	model := NewImportProgressModel(totalFiles, styles)

	assert.Equal(t, totalFiles, model.totalFiles)
	assert.Equal(t, 0, model.processedFiles)
	assert.Equal(t, "", model.currentFile)
	assert.False(t, model.completed)
	assert.False(t, model.paused)
	assert.Empty(t, model.errors)
	assert.NotNil(t, model.Model)
}

func TestImportProgressModel_Init(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})
	cmd := model.Init()
	assert.NotNil(t, cmd) // Now returns a tick command for fallback progress
}

func TestImportProgressModel_Update_ProgressMsg(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	msg := ImportProgressMsg{
		CurrentFile:    "test.json",
		ProcessedFiles: 1,
		TotalFiles:     3,
		Completed:      false,
		Paused:         false,
		PauseReason:    "",
	}

	updatedModel, cmd := model.Update(msg)

	assert.Equal(t, "test.json", updatedModel.currentFile)
	assert.Equal(t, 1, updatedModel.processedFiles)
	assert.Equal(t, 3, updatedModel.totalFiles)
	assert.False(t, updatedModel.completed)
	assert.False(t, updatedModel.paused)
	assert.NotNil(t, cmd) // Should return progress update command
}

func TestImportProgressModel_Update_ProgressMsgWithError(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	importErr := &ImportError{
		File:    "error.json",
		Error:   errors.New("test error"),
		Skipped: false,
	}

	msg := ImportProgressMsg{
		CurrentFile:    "error.json",
		ProcessedFiles: 1,
		Error:          importErr,
	}

	updatedModel, _ := model.Update(msg)

	assert.Equal(t, 1, len(updatedModel.errors))
	assert.Equal(t, "error.json", updatedModel.errors[0].File)
	assert.Equal(t, "test error", updatedModel.errors[0].Error.Error())
	assert.False(t, updatedModel.errors[0].Skipped)
}

func TestImportProgressModel_Update_FrameMsg(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	frameMsg := progress.FrameMsg{}
	updatedModel, _ := model.Update(frameMsg)

	// Should update the internal progress model
	assert.NotNil(t, updatedModel.Model)
}

func TestImportProgressModel_GetPercentage(t *testing.T) {
	model := NewImportProgressModel(4, Styles{})

	// Test initial percentage
	assert.Equal(t, 0.0, model.GetPercentage())

	// Test with progress
	model.processedFiles = 2
	assert.Equal(t, 0.5, model.GetPercentage())

	// Test with zero total files
	model.totalFiles = 0
	assert.Equal(t, 0.0, model.GetPercentage())
}

func TestImportProgressModel_StateQueries(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	// Test initial state
	assert.False(t, model.IsCompleted())
	assert.False(t, model.IsPaused())

	// Test paused state
	model.paused = true
	assert.True(t, model.IsPaused())

	// Test completed state
	model.completed = true
	assert.True(t, model.IsCompleted())
}

func TestImportProgressModel_ErrorHandling(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	// Add various errors
	model.AddError("failed1.json", errors.New("error 1"), false)
	model.AddError("skipped1.json", errors.New("error 2"), true)
	model.AddError("failed2.json", errors.New("error 3"), false)

	// Test error retrieval
	allErrors := model.GetErrors()
	assert.Equal(t, 3, len(allErrors))

	failedErrors := model.GetFailedErrors()
	assert.Equal(t, 2, len(failedErrors))
	assert.Equal(t, "failed1.json", failedErrors[0].File)
	assert.Equal(t, "failed2.json", failedErrors[1].File)

	skippedErrors := model.GetSkippedErrors()
	assert.Equal(t, 1, len(skippedErrors))
	assert.Equal(t, "skipped1.json", skippedErrors[0].File)

	// Test internal count methods
	assert.Equal(t, 2, model.getFailedCount())
	assert.Equal(t, 1, model.getSkippedCount())
}

func TestImportProgressModel_Reset(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	// Set some state
	model.processedFiles = 2
	model.currentFile = "test.json"
	model.completed = true
	model.paused = true
	model.pauseReason = "test reason"
	model.AddError("error.json", errors.New("test"), false)

	// Reset with new total
	model.Reset(5)

	assert.Equal(t, 5, model.totalFiles)
	assert.Equal(t, 0, model.processedFiles)
	assert.Equal(t, "", model.currentFile)
	assert.False(t, model.completed)
	assert.False(t, model.paused)
	assert.Equal(t, "", model.pauseReason)
	assert.Empty(t, model.errors)
}

func TestImportProgressModel_PauseResume(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	// Test pause
	model.Pause("Password required")
	assert.True(t, model.paused)
	assert.Equal(t, "Password required", model.pauseReason)

	// Test resume
	model.Resume()
	assert.False(t, model.paused)
	assert.Equal(t, "", model.pauseReason)
}

func TestImportProgressModel_UpdateProgress(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	model.UpdateProgress("current.json", 2)

	assert.Equal(t, "current.json", model.currentFile)
	assert.Equal(t, 2, model.processedFiles)
}

func TestImportProgressModel_Complete(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	// Set some state
	model.currentFile = "test.json"
	model.paused = true
	model.pauseReason = "test reason"

	model.Complete()

	assert.True(t, model.completed)
	assert.False(t, model.paused)
	assert.Equal(t, "", model.pauseReason)
	assert.Equal(t, "", model.currentFile)
}

func TestImportProgressModel_GetSummaryText(t *testing.T) {
	model := NewImportProgressModel(5, Styles{})

	// Test incomplete state
	summary := model.GetSummaryText()
	assert.Empty(t, summary)

	// Set up completed state with mixed results
	model.processedFiles = 5
	model.AddError("failed.json", errors.New("error"), false)
	model.AddError("skipped.json", errors.New("error"), true)
	model.Complete()

	summary = model.GetSummaryText()
	assert.Contains(t, summary, "Import completed")
	assert.Contains(t, summary, "Total files: 5")
	assert.Contains(t, summary, "Successful: 3") // 5 - 2 errors
	assert.Contains(t, summary, "Failed: 1")
	assert.Contains(t, summary, "Skipped: 1")
}

func TestImportProgressModel_View(t *testing.T) {
	styles := createStyles()
	model := NewImportProgressModel(3, styles)

	// Test empty view
	model.totalFiles = 0
	view := model.View()
	assert.Empty(t, view)

	// Test initial view
	model.totalFiles = 3
	view = model.View()
	assert.Contains(t, view, "Import Progress")
	assert.Contains(t, view, "0/3 files (0.0%)")

	// Test active import view
	model.UpdateProgress("importing.json", 1)
	view = model.View()
	assert.Contains(t, view, "Processing: importing.json")
	assert.Contains(t, view, "1/3 files (33.3%)")
	assert.Contains(t, view, "⏳ Importing...")

	// Test paused view
	model.Pause("Password required")
	view = model.View()
	assert.Contains(t, view, "⏸ Import paused")
	assert.Contains(t, view, "Password required")

	// Test completed view
	model.Resume()
	model.processedFiles = 3
	model.Complete()
	view = model.View()
	assert.Contains(t, view, "✓ Import completed")
	assert.Contains(t, view, "Success: 3, Failed: 0, Skipped: 0")
}

func TestImportProgressModel_ViewWithErrors(t *testing.T) {
	styles := createStyles()
	model := NewImportProgressModel(5, styles)

	// Add multiple errors
	for i := 0; i < 5; i++ {
		model.AddError("error.json", errors.New("test error"), i%2 == 0)
	}

	view := model.View()
	assert.Contains(t, view, "Errors:")
	assert.Contains(t, view, "... and 2 more errors") // Shows only last 3
}

func TestImportError(t *testing.T) {
	err := ImportError{
		File:    "test.json",
		Error:   errors.New("test error"),
		Skipped: true,
	}

	assert.Equal(t, "test.json", err.File)
	assert.Equal(t, "test error", err.Error.Error())
	assert.True(t, err.Skipped)
}

func TestImportProgressMsg(t *testing.T) {
	importErr := &ImportError{
		File:    "error.json",
		Error:   errors.New("test error"),
		Skipped: false,
	}

	msg := ImportProgressMsg{
		CurrentFile:    "current.json",
		ProcessedFiles: 2,
		TotalFiles:     5,
		Error:          importErr,
		Completed:      false,
		Paused:         true,
		PauseReason:    "Password input",
	}

	assert.Equal(t, "current.json", msg.CurrentFile)
	assert.Equal(t, 2, msg.ProcessedFiles)
	assert.Equal(t, 5, msg.TotalFiles)
	assert.NotNil(t, msg.Error)
	assert.False(t, msg.Completed)
	assert.True(t, msg.Paused)
	assert.Equal(t, "Password input", msg.PauseReason)
}

// Mock style removed - using createStyles() instead

// Benchmark tests
func BenchmarkImportProgressModel_Update(b *testing.B) {
	model := NewImportProgressModel(1000, Styles{})
	msg := ImportProgressMsg{
		CurrentFile:    "test.json",
		ProcessedFiles: 500,
		TotalFiles:     1000,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.Update(msg)
	}
}

func BenchmarkImportProgressModel_View(b *testing.B) {
	styles := createStyles()
	model := NewImportProgressModel(100, styles)
	model.UpdateProgress("test.json", 50)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.View()
	}
}

// Integration test with actual BubbleTea program
func TestImportProgressModel_Integration(t *testing.T) {
	model := NewImportProgressModel(3, Styles{})

	// Simulate a complete import workflow
	steps := []ImportProgressMsg{
		{CurrentFile: "file1.json", ProcessedFiles: 0, TotalFiles: 3},
		{CurrentFile: "file1.json", ProcessedFiles: 1, TotalFiles: 3},
		{CurrentFile: "file2.json", ProcessedFiles: 1, TotalFiles: 3, Paused: true, PauseReason: "Password required"},
		{CurrentFile: "file2.json", ProcessedFiles: 1, TotalFiles: 3, Paused: false},
		{CurrentFile: "file2.json", ProcessedFiles: 2, TotalFiles: 3},
		{CurrentFile: "file3.json", ProcessedFiles: 2, TotalFiles: 3},
		{Error: &ImportError{File: "file3.json", Error: errors.New("invalid password"), Skipped: true}},
		{ProcessedFiles: 3, TotalFiles: 3, Completed: true},
	}

	for _, step := range steps {
		var cmd tea.Cmd
		model, cmd = model.Update(step)

		// Verify state transitions
		if step.Paused {
			assert.True(t, model.IsPaused())
		}
		if step.Completed {
			assert.True(t, model.IsCompleted())
		}
		if step.Error != nil {
			assert.Greater(t, len(model.GetErrors()), 0)
		}

		// Commands should be generated for progress updates
		if step.ProcessedFiles > 0 || step.Error != nil {
			assert.NotNil(t, cmd)
		}
	}

	// Verify final state
	assert.True(t, model.IsCompleted())
	assert.Equal(t, 3, model.processedFiles)
	assert.Equal(t, 1, len(model.GetSkippedErrors()))
	assert.Equal(t, 0, len(model.GetFailedErrors()))
}
