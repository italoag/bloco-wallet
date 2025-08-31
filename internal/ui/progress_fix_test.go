package ui

import (
	"testing"
	"time"

	"blocowallet/internal/wallet"

	"github.com/stretchr/testify/assert"
)

func TestProgressBarUpdateMechanism(t *testing.T) {
	t.Run("Progress updates are received and processed", func(t *testing.T) {
		// Create progress model
		styles := Styles{}
		progressModel := NewImportProgressModel(3, styles)

		// Test initial state
		assert.Equal(t, 3, progressModel.totalFiles)
		assert.Equal(t, 0, progressModel.processedFiles)
		assert.False(t, progressModel.completed)

		// Send progress update
		progressMsg := ImportProgressMsg{
			CurrentFile:    "test1.json",
			ProcessedFiles: 1,
			TotalFiles:     3,
			Completed:      false,
		}

		updatedModel, cmd := progressModel.Update(progressMsg)
		assert.NotNil(t, cmd)
		assert.Equal(t, "test1.json", updatedModel.currentFile)
		assert.Equal(t, 1, updatedModel.processedFiles)
		assert.Equal(t, 3, updatedModel.totalFiles)
		assert.False(t, updatedModel.completed)
	})

	t.Run("Fallback progress estimation works", func(t *testing.T) {
		// Create progress model
		styles := Styles{}
		progressModel := NewImportProgressModel(2, styles)

		// Set last update time to simulate no recent updates
		progressModel.lastUpdateTime = time.Now().Add(-5 * time.Second)
		progressModel.startTime = time.Now().Add(-10 * time.Second)

		// Send tick message to trigger fallback estimation
		tickMsg := TickMsg{Time: time.Now()}
		updatedModel, cmd := progressModel.Update(tickMsg)

		assert.NotNil(t, cmd)
		assert.True(t, updatedModel.estimatedProgress > 0)
		assert.True(t, updatedModel.fallbackEnabled)
	})

	t.Run("Enhanced import state updates progress correctly", func(t *testing.T) {
		// Create mock batch service
		mockService := &MockBatchImportService{}
		styles := Styles{}

		// Create enhanced import state
		state := NewEnhancedImportState(mockService, styles)

		// Test progress update
		progress := wallet.ImportProgress{
			CurrentFile:    "test.json",
			TotalFiles:     2,
			ProcessedFiles: 1,
			Percentage:     50.0,
		}

		// Update progress
		state.UpdateProgress(progress)

		// Verify progress was updated
		assert.Equal(t, progress, state.CurrentProgress)
		assert.NotNil(t, state.ProgressBar)
	})

	t.Run("Progress channel communication works", func(t *testing.T) {
		// Create mock batch service
		mockService := &MockBatchImportService{}
		styles := Styles{}

		// Create enhanced import state
		state := NewEnhancedImportState(mockService, styles)

		// Get progress channel
		progressChan := state.GetProgressChan()
		assert.NotNil(t, progressChan)

		// Test that channel is ready for reading (non-blocking)
		select {
		case <-progressChan:
			t.Error("Progress channel should be empty initially")
		default:
			// Expected - channel is empty
		}
	})

	t.Run("Debug logging is enabled", func(t *testing.T) {
		// Create progress model
		styles := Styles{}
		progressModel := NewImportProgressModel(1, styles)

		// Send progress update (this should trigger debug logging)
		progressMsg := ImportProgressMsg{
			CurrentFile:    "debug_test.json",
			ProcessedFiles: 1,
			TotalFiles:     1,
			Completed:      true,
		}

		// Update should not panic and should return valid model
		updatedModel, cmd := progressModel.Update(progressMsg)
		assert.NotNil(t, updatedModel)
		assert.NotNil(t, cmd)
		assert.True(t, updatedModel.completed)
	})
}

func TestProgressListeningCommands(t *testing.T) {
	t.Run("listenForProgressUpdates returns valid command", func(t *testing.T) {
		// Create CLI model with enhanced import state
		mockService := &MockBatchImportService{}
		styles := Styles{}

		model := &CLIModel{
			enhancedImportState: NewEnhancedImportState(mockService, styles),
		}

		// Get progress listening command
		cmd := model.listenForProgressUpdates()
		assert.NotNil(t, cmd)
	})

	t.Run("listenForPasswordRequests returns valid command", func(t *testing.T) {
		// Create CLI model with enhanced import state
		mockService := &MockBatchImportService{}
		styles := Styles{}

		model := &CLIModel{
			enhancedImportState: NewEnhancedImportState(mockService, styles),
		}

		// Get password listening command
		cmd := model.listenForPasswordRequests()
		assert.NotNil(t, cmd)
	})

	t.Run("Commands return nil when import state is nil", func(t *testing.T) {
		model := &CLIModel{
			enhancedImportState: nil,
		}

		// Both commands should return nil
		progressCmd := model.listenForProgressUpdates()
		passwordCmd := model.listenForPasswordRequests()

		assert.Nil(t, progressCmd)
		assert.Nil(t, passwordCmd)
	})
}
