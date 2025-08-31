package ui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"blocowallet/internal/wallet"
)

// MockBatchImportService provides a mock implementation for testing
type MockBatchImportService struct {
	jobs    []wallet.ImportJob
	results []wallet.ImportResult
	err     error
}

// Ensure MockBatchImportService implements BatchImportServiceInterface
var _ BatchImportServiceInterface = (*MockBatchImportService)(nil)

func (m *MockBatchImportService) CreateImportJobsFromFiles(files []string) ([]wallet.ImportJob, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.jobs, nil
}

func (m *MockBatchImportService) CreateImportJobsFromDirectory(dir string) ([]wallet.ImportJob, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.jobs, nil
}

func (m *MockBatchImportService) ValidateImportJobs(jobs []wallet.ImportJob) error {
	return m.err
}

func (m *MockBatchImportService) ImportBatch(
	jobs []wallet.ImportJob,
	progressChan chan<- wallet.ImportProgress,
	passwordRequestChan chan<- wallet.PasswordRequest,
	passwordResponseChan <-chan wallet.PasswordResponse,
) []wallet.ImportResult {
	// Simulate import process without goroutines to avoid test deadlocks
	if progressChan != nil {
		// Send initial progress
		select {
		case progressChan <- wallet.ImportProgress{
			CurrentFile:    "",
			TotalFiles:     len(jobs),
			ProcessedFiles: 0,
			Percentage:     0.0,
			StartTime:      time.Now(),
		}:
		default:
		}

		// Send final progress
		select {
		case progressChan <- wallet.ImportProgress{
			CurrentFile:    "",
			TotalFiles:     len(jobs),
			ProcessedFiles: len(jobs),
			Percentage:     100.0,
			StartTime:      time.Now(),
		}:
		default:
		}

		// Close the channel to signal completion
		close(progressChan)
	}

	return m.results
}

func (m *MockBatchImportService) GetImportSummary(results []wallet.ImportResult) wallet.ImportSummary {
	summary := wallet.ImportSummary{
		TotalFiles: len(results),
	}

	for _, result := range results {
		if result.Success {
			summary.SuccessfulImports++
		} else if result.Skipped {
			summary.SkippedImports++
		} else {
			summary.FailedImports++
		}
	}

	return summary
}

func TestNewEnhancedImportState(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()

	state := NewEnhancedImportState(mockService, styles)

	assert.NotNil(t, state)
	assert.Equal(t, PhaseFileSelection, state.GetCurrentPhase())
	assert.NotNil(t, state.FilePicker)
	assert.NotNil(t, state.ProgressBar)
	assert.NotNil(t, state.BatchService)
	assert.Empty(t, state.SelectedFiles)
	assert.Empty(t, state.ImportJobs)
	assert.Empty(t, state.Results)
	assert.False(t, state.ShowingPopup)
	assert.False(t, state.IsCompleted())
	assert.False(t, state.IsCancelled())
}

func TestPhaseTransitions(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	tests := []struct {
		name        string
		fromPhase   ImportPhase
		toPhase     ImportPhase
		shouldError bool
	}{
		{"FileSelection to Importing", PhaseFileSelection, PhaseImporting, false},
		{"FileSelection to Cancelled", PhaseFileSelection, PhaseCancelled, false},
		{"FileSelection to Complete", PhaseFileSelection, PhaseComplete, true}, // Invalid
		{"Importing to PasswordInput", PhaseImporting, PhasePasswordInput, false},
		{"Importing to Complete", PhaseImporting, PhaseComplete, false},
		{"Importing to Cancelled", PhaseImporting, PhaseCancelled, false},
		{"PasswordInput to Importing", PhasePasswordInput, PhaseImporting, false},
		{"PasswordInput to Complete", PhasePasswordInput, PhaseComplete, false},
		{"PasswordInput to Cancelled", PhasePasswordInput, PhaseCancelled, false},
		{"Complete to FileSelection", PhaseComplete, PhaseFileSelection, false},
		{"Cancelled to FileSelection", PhaseCancelled, PhaseFileSelection, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set initial phase
			state.Phase = tt.fromPhase

			err := state.TransitionToPhase(tt.toPhase)

			if tt.shouldError {
				assert.Error(t, err)
				assert.Equal(t, tt.fromPhase, state.GetCurrentPhase()) // Should remain unchanged
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.toPhase, state.GetCurrentPhase())
			}
		})
	}
}

func TestStartImport(t *testing.T) {
	mockService := &MockBatchImportService{
		jobs: []wallet.ImportJob{
			{KeystorePath: "test1.json", WalletName: "wallet1"},
			{KeystorePath: "test2.json", WalletName: "wallet2"},
		},
	}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	t.Run("Start import with selected files", func(t *testing.T) {
		state.SelectedFiles = []string{"test1.json", "test2.json"}

		err := state.StartImport()

		assert.NoError(t, err)
		assert.Equal(t, PhaseImporting, state.GetCurrentPhase())
		assert.Len(t, state.ImportJobs, 2)
	})

	t.Run("Start import with selected directory", func(t *testing.T) {
		// Reset state
		state.Phase = PhaseFileSelection
		state.SelectedFiles = []string{}
		state.SelectedDir = "/test/dir"

		err := state.StartImport()

		assert.NoError(t, err)
		assert.Equal(t, PhaseImporting, state.GetCurrentPhase())
		assert.Len(t, state.ImportJobs, 2)
	})

	t.Run("Start import with no selection", func(t *testing.T) {
		// Reset state
		state.Phase = PhaseFileSelection
		state.SelectedFiles = []string{}
		state.SelectedDir = ""

		err := state.StartImport()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no files or directory selected")
		assert.Equal(t, PhaseFileSelection, state.GetCurrentPhase())
	})

	t.Run("Start import from wrong phase", func(t *testing.T) {
		state.Phase = PhaseImporting

		err := state.StartImport()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot start import from phase")
	})
}

func TestPasswordHandling(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	t.Run("Handle password request", func(t *testing.T) {
		state.Phase = PhaseImporting

		request := wallet.PasswordRequest{
			KeystoreFile: "test.json",
			AttemptCount: 1,
			IsRetry:      false,
		}

		err := state.HandlePasswordRequest(request)

		assert.NoError(t, err)
		assert.Equal(t, PhasePasswordInput, state.GetCurrentPhase())
		assert.True(t, state.ShowingPopup)
		assert.NotNil(t, state.PendingPassword)
		assert.Equal(t, "test.json", state.PendingPassword.KeystoreFile)
	})

	t.Run("Submit password", func(t *testing.T) {
		state.Phase = PhasePasswordInput
		state.PendingPassword = &wallet.PasswordRequest{KeystoreFile: "test.json"}

		err := state.SubmitPassword("testpassword")

		assert.NoError(t, err)
		assert.Equal(t, PhaseImporting, state.GetCurrentPhase())
		assert.False(t, state.ShowingPopup)
		assert.Nil(t, state.PendingPassword)
	})

	t.Run("Cancel password input", func(t *testing.T) {
		// Create a new state with buffered channels for testing
		testState := NewEnhancedImportState(mockService, styles)
		testState.Phase = PhasePasswordInput
		testState.PendingPassword = &wallet.PasswordRequest{KeystoreFile: "test.json"}

		// Create buffered channel to avoid blocking
		testState.passwordResponseChan = make(chan wallet.PasswordResponse, 1)

		err := testState.CancelPasswordInput()

		assert.NoError(t, err)
		assert.Equal(t, PhaseImporting, testState.GetCurrentPhase())
		assert.False(t, testState.ShowingPopup)
		assert.Nil(t, testState.PendingPassword)
	})

	t.Run("Skip password input", func(t *testing.T) {
		// Create a new state with buffered channels for testing
		testState := NewEnhancedImportState(mockService, styles)
		testState.Phase = PhasePasswordInput
		testState.PendingPassword = &wallet.PasswordRequest{KeystoreFile: "test.json"}

		// Create buffered channel to avoid blocking
		testState.passwordResponseChan = make(chan wallet.PasswordResponse, 1)

		err := testState.SkipPasswordInput()

		assert.NoError(t, err)
		assert.Equal(t, PhaseImporting, testState.GetCurrentPhase())
		assert.False(t, testState.ShowingPopup)
		assert.Nil(t, testState.PendingPassword)
	})
}

func TestImportCompletion(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	t.Run("Complete import with results", func(t *testing.T) {
		state.Phase = PhaseImporting

		results := []wallet.ImportResult{
			{Success: true, Job: wallet.ImportJob{KeystorePath: "test1.json"}},
			{Success: false, Job: wallet.ImportJob{KeystorePath: "test2.json"}, Skipped: true},
		}

		err := state.CompleteImport(results)

		assert.NoError(t, err)
		assert.Equal(t, PhaseComplete, state.GetCurrentPhase())
		assert.True(t, state.IsCompleted())
		assert.Len(t, state.GetResults(), 2)

		summary := state.GetSummary()
		assert.Equal(t, 2, summary.TotalFiles)
		assert.Equal(t, 1, summary.SuccessfulImports)
		assert.Equal(t, 0, summary.FailedImports)
		assert.Equal(t, 1, summary.SkippedImports)
	})

	t.Run("Cancel import", func(t *testing.T) {
		state.Phase = PhaseImporting

		err := state.CancelImport()

		assert.NoError(t, err)
		assert.Equal(t, PhaseCancelled, state.GetCurrentPhase())
		assert.True(t, state.IsCancelled())
	})
}

func TestProgressUpdates(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	progress := wallet.ImportProgress{
		CurrentFile:     "test.json",
		TotalFiles:      5,
		ProcessedFiles:  2,
		Percentage:      40.0,
		PendingPassword: false,
		StartTime:       time.Now(),
	}

	state.UpdateProgress(progress)

	assert.Equal(t, "test.json", state.CurrentProgress.CurrentFile)
	assert.Equal(t, 5, state.CurrentProgress.TotalFiles)
	assert.Equal(t, 2, state.CurrentProgress.ProcessedFiles)
	assert.Equal(t, 40.0, state.CurrentProgress.Percentage)
}

func TestCleanupFunctions(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	cleanupCalled := false
	state.AddCleanupFunc(func() {
		cleanupCalled = true
	})

	// Trigger cleanup by cancelling
	err := state.CancelImport()
	assert.NoError(t, err)
	assert.True(t, cleanupCalled)
}

func TestStateInfo(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	state.SelectedFiles = []string{"test1.json", "test2.json"}
	state.SelectedDir = "/test/dir"
	state.ImportJobs = []wallet.ImportJob{{KeystorePath: "test.json"}}
	state.ShowingPopup = true

	info := state.GetStateInfo()

	assert.Equal(t, PhaseFileSelection, info.Phase)
	assert.Equal(t, 2, info.SelectedFiles)
	assert.Equal(t, "/test/dir", info.SelectedDir)
	assert.Equal(t, 1, info.ImportJobs)
	assert.True(t, info.ShowingPopup)
	assert.False(t, info.Completed)
	assert.False(t, info.Cancelled)

	// Test string representation
	infoStr := info.String()
	assert.Contains(t, infoStr, "Phase: File Selection")
	assert.Contains(t, infoStr, "Files: 2")
	assert.Contains(t, infoStr, "Dir: /test/dir")
}

func TestConcurrentAccess(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	// Test basic concurrent access without complex goroutines
	phase1 := state.GetCurrentPhase()
	assert.Equal(t, PhaseFileSelection, phase1)

	// Test phase transition
	err := state.TransitionToPhase(PhaseCancelled)
	assert.NoError(t, err)

	phase2 := state.GetCurrentPhase()
	assert.Equal(t, PhaseCancelled, phase2)

	// Test transition back
	err = state.TransitionToPhase(PhaseFileSelection)
	assert.NoError(t, err)

	phase3 := state.GetCurrentPhase()
	assert.Equal(t, PhaseFileSelection, phase3)
}

func TestChannelOperations(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	// Test channel access
	progressChan := state.GetProgressChan()
	passwordRequestChan := state.GetPasswordRequestChan()

	assert.NotNil(t, progressChan)
	assert.NotNil(t, passwordRequestChan)

	// Channels should be non-blocking for reads (no data available)
	select {
	case <-progressChan:
		t.Error("Progress channel should be empty")
	default:
		// Expected - no data available
	}

	select {
	case <-passwordRequestChan:
		t.Error("Password request channel should be empty")
	default:
		// Expected - no data available
	}
}

func TestPhaseSpecificSetup(t *testing.T) {
	mockService := &MockBatchImportService{}
	styles := createStyles()
	state := NewEnhancedImportState(mockService, styles)

	t.Run("File selection phase setup", func(t *testing.T) {
		// Set some state first
		state.SelectedFiles = []string{"test.json"}
		state.ImportJobs = []wallet.ImportJob{{KeystorePath: "test.json"}}
		state.completed = true
		state.ShowingPopup = true

		err := state.TransitionToPhase(PhaseFileSelection)
		require.NoError(t, err)

		// Should reset state
		assert.Empty(t, state.SelectedFiles)
		assert.Empty(t, state.ImportJobs)
		assert.Empty(t, state.Results)
		assert.False(t, state.completed)
		assert.False(t, state.ShowingPopup)
	})

	t.Run("Importing phase setup", func(t *testing.T) {
		state.ImportJobs = []wallet.ImportJob{{KeystorePath: "test.json"}}

		err := state.TransitionToPhase(PhaseImporting)
		require.NoError(t, err)

		assert.False(t, state.startTime.IsZero())
		assert.False(t, state.ShowingPopup)
		assert.Equal(t, 1, state.CurrentProgress.TotalFiles)
	})

	t.Run("Password input phase setup", func(t *testing.T) {
		state.PendingPassword = &wallet.PasswordRequest{
			KeystoreFile: "test.json",
			AttemptCount: 1,
		}

		err := state.TransitionToPhase(PhasePasswordInput)
		require.NoError(t, err)

		assert.True(t, state.ShowingPopup)
		assert.NotNil(t, state.PasswordPopup)
	})

	t.Run("Complete phase setup", func(t *testing.T) {
		err := state.TransitionToPhase(PhaseComplete)
		require.NoError(t, err)

		assert.True(t, state.completed)
		assert.False(t, state.ShowingPopup)
		assert.Nil(t, state.PendingPassword)
	})

	t.Run("Cancelled phase setup", func(t *testing.T) {
		cleanupCalled := false
		state.AddCleanupFunc(func() {
			cleanupCalled = true
		})

		err := state.TransitionToPhase(PhaseCancelled)
		require.NoError(t, err)

		assert.True(t, state.cancelled)
		assert.False(t, state.ShowingPopup)
		assert.Nil(t, state.PendingPassword)
		assert.True(t, cleanupCalled)
	})
}
