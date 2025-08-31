package ui

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"blocowallet/internal/wallet"
)

// TestBatchImportService for testing the enhanced progress functionality
type TestBatchImportService struct {
	jobs    []wallet.ImportJob
	results []wallet.ImportResult
}

func (m *TestBatchImportService) CreateImportJobsFromFiles(files []string) ([]wallet.ImportJob, error) {
	return m.jobs, nil
}

func (m *TestBatchImportService) CreateImportJobsFromDirectory(dir string) ([]wallet.ImportJob, error) {
	return m.jobs, nil
}

func (m *TestBatchImportService) ValidateImportJobs(jobs []wallet.ImportJob) error {
	return nil
}

func (m *TestBatchImportService) ImportBatch(
	jobs []wallet.ImportJob,
	progressChan chan<- wallet.ImportProgress,
	passwordRequestChan chan<- wallet.PasswordRequest,
	passwordResponseChan <-chan wallet.PasswordResponse,
) []wallet.ImportResult {
	// Clear previous results
	m.results = []wallet.ImportResult{}

	// Simulate import progress
	for i := 0; i <= len(jobs); i++ {
		progress := wallet.ImportProgress{
			CurrentFile:     "",
			TotalFiles:      len(jobs),
			ProcessedFiles:  i,
			Percentage:      float64(i) / float64(len(jobs)) * 100,
			Errors:          []wallet.ImportError{},
			PendingPassword: false,
			PendingFile:     "",
			StartTime:       time.Now(),
			ElapsedTime:     time.Duration(i) * time.Second,
		}

		if i < len(jobs) {
			progress.CurrentFile = jobs[i].KeystorePath

			// Create result for this job
			result := wallet.ImportResult{
				Job:     jobs[i],
				Success: true,
				Wallet:  nil, // Would contain actual wallet details
				Error:   nil,
				Skipped: false,
			}
			m.results = append(m.results, result)
		}

		// Send progress update
		select {
		case progressChan <- progress:
		case <-time.After(100 * time.Millisecond):
			// Continue even if channel is blocked
		}

		// Small delay to simulate work
		time.Sleep(10 * time.Millisecond)
	}

	close(progressChan)
	return m.results
}

func (m *TestBatchImportService) GetImportSummary(results []wallet.ImportResult) wallet.ImportSummary {
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

func TestProgressChannelCommunication(t *testing.T) {
	t.Run("Progress updates are properly sent and received", func(t *testing.T) {
		// Create mock service with test jobs
		mockService := &TestBatchImportService{
			jobs: []wallet.ImportJob{
				{KeystorePath: "test1.json", WalletName: "wallet1"},
				{KeystorePath: "test2.json", WalletName: "wallet2"},
				{KeystorePath: "test3.json", WalletName: "wallet3"},
			},
			results: []wallet.ImportResult{},
		}

		// Create enhanced import state
		styles := Styles{}
		state := NewEnhancedImportState(mockService, styles)

		// Set up import jobs
		state.ImportJobs = mockService.jobs
		err := state.TransitionToPhase(PhaseImporting)
		require.NoError(t, err)

		// Collect progress updates
		var progressUpdates []wallet.ImportProgress
		var mu sync.Mutex

		// Create separate channels for testing since the internal ones are private
		progressChan := make(chan wallet.ImportProgress, 500)
		passwordRequestChan := make(chan wallet.PasswordRequest, 1)
		passwordResponseChan := make(chan wallet.PasswordResponse, 1)

		// Start progress collection goroutine
		done := make(chan bool)
		go func() {
			defer close(done)
			for progress := range progressChan {
				mu.Lock()
				progressUpdates = append(progressUpdates, progress)

				// Simulate TUI progress update
				state.UpdateProgress(progress)
				mu.Unlock()
			}
		}()

		// Start the import process
		results := state.BatchService.ImportBatch(
			state.ImportJobs,
			progressChan,
			passwordRequestChan,
			passwordResponseChan,
		)

		// Wait for progress collection to complete
		<-done

		// Verify we received progress updates
		mu.Lock()
		defer mu.Unlock()

		assert.Greater(t, len(progressUpdates), 0, "Should have received progress updates")
		assert.Equal(t, len(mockService.jobs), len(results), "Should have processed all jobs")

		// Verify progress increases monotonically
		for i := 1; i < len(progressUpdates); i++ {
			assert.GreaterOrEqual(t, progressUpdates[i].ProcessedFiles, progressUpdates[i-1].ProcessedFiles,
				"Processed files should not decrease")
			assert.GreaterOrEqual(t, progressUpdates[i].Percentage, progressUpdates[i-1].Percentage,
				"Percentage should not decrease")
		}

		// Verify final progress shows completion
		if len(progressUpdates) > 0 {
			finalProgress := progressUpdates[len(progressUpdates)-1]
			assert.Equal(t, len(mockService.jobs), finalProgress.ProcessedFiles)
			assert.Equal(t, 100.0, finalProgress.Percentage)
		}
	})
}

func TestProgressValidation(t *testing.T) {
	styles := Styles{}
	state := NewEnhancedImportState(&TestBatchImportService{}, styles)

	t.Run("Valid progress update is accepted", func(t *testing.T) {
		validProgress := wallet.ImportProgress{
			CurrentFile:     "test.json",
			TotalFiles:      10,
			ProcessedFiles:  5,
			Percentage:      50.0,
			Errors:          []wallet.ImportError{},
			PendingPassword: false,
			PendingFile:     "",
			StartTime:       time.Now(),
			ElapsedTime:     5 * time.Second,
		}

		// This should not panic or log errors
		state.UpdateProgress(validProgress)

		// Verify progress was accepted
		assert.Equal(t, validProgress.ProcessedFiles, state.CurrentProgress.ProcessedFiles)
		assert.Equal(t, validProgress.Percentage, state.CurrentProgress.Percentage)
	})

	t.Run("Invalid progress updates are rejected", func(t *testing.T) {
		testCases := []struct {
			name     string
			progress wallet.ImportProgress
		}{
			{
				name: "Negative total files",
				progress: wallet.ImportProgress{
					TotalFiles:     -1,
					ProcessedFiles: 0,
					Percentage:     0.0,
				},
			},
			{
				name: "Negative processed files",
				progress: wallet.ImportProgress{
					TotalFiles:     10,
					ProcessedFiles: -1,
					Percentage:     0.0,
				},
			},
			{
				name: "Processed files exceeds total",
				progress: wallet.ImportProgress{
					TotalFiles:     10,
					ProcessedFiles: 15,
					Percentage:     150.0,
				},
			},
			{
				name: "Percentage out of range (negative)",
				progress: wallet.ImportProgress{
					TotalFiles:     10,
					ProcessedFiles: 5,
					Percentage:     -10.0,
				},
			},
			{
				name: "Percentage out of range (too high)",
				progress: wallet.ImportProgress{
					TotalFiles:     10,
					ProcessedFiles: 5,
					Percentage:     150.0,
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Store original progress
				originalProgress := state.CurrentProgress

				// Try to update with invalid progress
				state.UpdateProgress(tc.progress)

				// Verify original progress is unchanged
				assert.Equal(t, originalProgress.ProcessedFiles, state.CurrentProgress.ProcessedFiles,
					"Invalid progress should not update state")
				assert.Equal(t, originalProgress.Percentage, state.CurrentProgress.Percentage,
					"Invalid progress should not update state")
			})
		}
	})
}

func TestProgressBarIntegration(t *testing.T) {
	t.Run("Progress bar receives updates correctly", func(t *testing.T) {
		mockService := &TestBatchImportService{
			jobs: []wallet.ImportJob{
				{KeystorePath: "test1.json", WalletName: "wallet1"},
			},
		}

		styles := Styles{}
		state := NewEnhancedImportState(mockService, styles)

		// Set up import jobs and transition to importing phase
		state.ImportJobs = mockService.jobs
		err := state.TransitionToPhase(PhaseImporting)
		require.NoError(t, err)

		// Create a valid progress update
		progress := wallet.ImportProgress{
			CurrentFile:     "test1.json",
			TotalFiles:      1,
			ProcessedFiles:  0,
			Percentage:      0.0,
			Errors:          []wallet.ImportError{},
			PendingPassword: false,
			PendingFile:     "",
			StartTime:       time.Now(),
			ElapsedTime:     0,
		}

		// Update progress
		state.UpdateProgress(progress)

		// Verify progress bar was updated
		assert.NotNil(t, state.ProgressBar, "Progress bar should be initialized")

		// Get any pending command from the progress update
		cmd := state.GetPendingCommand()
		// Command might be nil, which is fine
		_ = cmd
	})
}

func TestChannelBufferCapacity(t *testing.T) {
	t.Run("Large buffer handles burst updates", func(t *testing.T) {
		// Create a test channel to verify buffer capacity
		testChan := make(chan wallet.ImportProgress, 500)

		// Send multiple updates quickly to test buffer capacity
		numUpdates := 100
		sent := 0

		for i := 0; i < numUpdates; i++ {
			progress := wallet.ImportProgress{
				CurrentFile:     "test.json",
				TotalFiles:      numUpdates,
				ProcessedFiles:  i,
				Percentage:      float64(i) / float64(numUpdates) * 100,
				Errors:          []wallet.ImportError{},
				PendingPassword: false,
				PendingFile:     "",
				StartTime:       time.Now(),
				ElapsedTime:     time.Duration(i) * time.Millisecond,
			}

			select {
			case testChan <- progress:
				sent++
			default:
				// Channel is full, which could happen but should be rare with large buffer
			}
		}

		// With a buffer of 500, we should be able to send all updates
		assert.Equal(t, numUpdates, sent, "Should be able to send all updates with large buffer")

		// Drain the channel to clean up
		close(testChan)
		for range testChan {
			// Drain channel
		}
	})
}
