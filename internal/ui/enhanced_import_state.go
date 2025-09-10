package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/logger"
)

// ImportPhase represents the current phase of the import process
type ImportPhase int

const (
	PhaseFileSelection ImportPhase = iota
	PhaseImporting
	PhasePasswordInput
	PhaseComplete
	PhaseCancelled
)

// String returns a string representation of the import phase
func (p ImportPhase) String() string {
	switch p {
	case PhaseFileSelection:
		return "File Selection"
	case PhaseImporting:
		return "Importing"
	case PhasePasswordInput:
		return "Password Input"
	case PhaseComplete:
		return "Complete"
	case PhaseCancelled:
		return "Cancelled"
	default:
		return "Unknown"
	}
}

// BatchImportServiceInterface defines the interface for batch import operations
type BatchImportServiceInterface interface {
	CreateImportJobsFromFiles(files []string) ([]wallet.ImportJob, error)
	CreateImportJobsFromDirectory(dir string) ([]wallet.ImportJob, error)
	ValidateImportJobs(jobs []wallet.ImportJob) error
	ImportBatch(
		jobs []wallet.ImportJob,
		progressChan chan<- wallet.ImportProgress,
		passwordRequestChan chan<- wallet.PasswordRequest,
		passwordResponseChan <-chan wallet.PasswordResponse,
	) []wallet.ImportResult
	GetImportSummary(results []wallet.ImportResult) wallet.ImportSummary
}

// EnhancedImportState manages the complete state of the enhanced import process
type EnhancedImportState struct {
	// Current phase of the import process
	Phase ImportPhase

	// File selection state
	SelectedFiles []string
	SelectedDir   string

	// Import job management
	ImportJobs []wallet.ImportJob
	Results    []wallet.ImportResult

	// Progress tracking
	CurrentProgress wallet.ImportProgress

	// Password popup state
	PasswordPopup   *PasswordPopupModel
	ShowingPopup    bool
	PendingPassword *wallet.PasswordRequest

	// Component models
	FilePicker   *EnhancedFilePickerModel
	ProgressBar  *ImportProgressModel
	Completion   *ImportCompletionModel
	BatchService BatchImportServiceInterface

	// Communication channels
	progressChan         chan wallet.ImportProgress
	passwordRequestChan  chan wallet.PasswordRequest
	passwordResponseChan chan wallet.PasswordResponse

	// State management
	mu             sync.RWMutex
	startTime      time.Time
	completed      bool
	cancelled      bool
	errorMessage   string
	pendingCommand tea.Cmd

	// Cleanup tracking
	cleanupFuncs []func()
}

// NewEnhancedImportState creates a new enhanced import state manager
func NewEnhancedImportState(batchService BatchImportServiceInterface, styles Styles) *EnhancedImportState {
	state := &EnhancedImportState{
		Phase:                PhaseFileSelection,
		SelectedFiles:        []string{},
		ImportJobs:           []wallet.ImportJob{},
		Results:              []wallet.ImportResult{},
		BatchService:         batchService,
		progressChan:         make(chan wallet.ImportProgress, 500), // Increased buffer for better throughput
		passwordRequestChan:  make(chan wallet.PasswordRequest, 1),
		passwordResponseChan: make(chan wallet.PasswordResponse, 1),
		cleanupFuncs:         []func(){},
	}

	// Initialize file picker
	filePicker := NewEnhancedFilePicker()
	// Set the current directory to the keystores directory if it exists
	if keystoreDir := "keystores"; fileExists(keystoreDir) {
		filePicker.CurrentDirectory = keystoreDir
	}
	state.FilePicker = &filePicker

	// Initialize progress bar (will be configured when import starts)
	progressBar := NewImportProgressModel(0, styles)
	state.ProgressBar = &progressBar

	return state
}

// GetCurrentPhase returns the current import phase (thread-safe)
func (s *EnhancedImportState) GetCurrentPhase() ImportPhase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Phase
}

// TransitionToPhase transitions to a new import phase with validation
func (s *EnhancedImportState) TransitionToPhase(newPhase ImportPhase) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate phase transition
	if !s.isValidTransition(s.Phase, newPhase) {
		return fmt.Errorf("invalid phase transition from %s to %s", s.Phase, newPhase)
	}

	s.Phase = newPhase

	// Perform phase-specific setup
	switch newPhase {
	case PhaseFileSelection:
		s.setupFileSelectionPhase()
	case PhaseImporting:
		s.setupImportingPhase()
	case PhasePasswordInput:
		s.setupPasswordInputPhase()
	case PhaseComplete:
		s.setupCompletePhase()
	case PhaseCancelled:
		s.setupCancelledPhase()
	}

	return nil
}

// isValidTransition validates if a phase transition is allowed
func (s *EnhancedImportState) isValidTransition(from, to ImportPhase) bool {
	// Allow transition to the same phase (for setup purposes)
	if from == to {
		return true
	}

	switch from {
	case PhaseFileSelection:
		return to == PhaseImporting || to == PhaseCancelled
	case PhaseImporting:
		return to == PhasePasswordInput || to == PhaseComplete || to == PhaseCancelled
	case PhasePasswordInput:
		return to == PhaseImporting || to == PhaseComplete || to == PhaseCancelled
	case PhaseComplete:
		return to == PhaseFileSelection || to == PhaseCancelled // Allow restart
	case PhaseCancelled:
		return to == PhaseFileSelection // Allow restart after cancellation
	default:
		return false
	}
}

// setupFileSelectionPhase initializes the file selection phase
func (s *EnhancedImportState) setupFileSelectionPhase() {
	// Reset state for new import session
	s.SelectedFiles = []string{}
	s.SelectedDir = ""
	s.ImportJobs = []wallet.ImportJob{}
	s.Results = []wallet.ImportResult{}
	s.completed = false
	s.cancelled = false
	s.errorMessage = ""
	s.ShowingPopup = false
	s.PendingPassword = nil

	// Reset file picker
	if s.FilePicker != nil {
		s.FilePicker.ClearAll()
	}
}

// setupImportingPhase initializes the importing phase
func (s *EnhancedImportState) setupImportingPhase() {
	s.startTime = time.Now()
	s.ShowingPopup = false

	// Configure progress bar with total file count
	if s.ProgressBar != nil {
		s.ProgressBar.Reset(len(s.ImportJobs))
	}

	// Initialize current progress
	s.CurrentProgress = wallet.ImportProgress{
		CurrentFile:     "",
		TotalFiles:      len(s.ImportJobs),
		ProcessedFiles:  0,
		Percentage:      0.0,
		Errors:          []wallet.ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       s.startTime,
		ElapsedTime:     0,
	}
}

// setupPasswordInputPhase initializes the password input phase
func (s *EnhancedImportState) setupPasswordInputPhase() {
	s.ShowingPopup = true

	// Create password popup if we have a pending request
	if s.PendingPassword != nil {
		popup := NewPasswordPopupModel(s.PendingPassword.KeystoreFile, 3)
		if s.PendingPassword.IsRetry && s.PendingPassword.ErrorMessage != "" {
			popup.SetError(s.PendingPassword.ErrorMessage)
		}
		s.PasswordPopup = &popup
	}
}

// setupCompletePhase initializes the completion phase
func (s *EnhancedImportState) setupCompletePhase() {
	s.completed = true
	s.ShowingPopup = false
	s.PendingPassword = nil

	// Mark progress as complete
	if s.ProgressBar != nil {
		s.ProgressBar.Complete()
	}

	// Initialize completion component
	if s.BatchService != nil && len(s.Results) > 0 {
		summary := s.BatchService.GetImportSummary(s.Results)
		completion := NewImportCompletionModel(summary, s.Results, s.startTime, Styles{})
		s.Completion = &completion
	}
}

// setupCancelledPhase initializes the cancelled phase
func (s *EnhancedImportState) setupCancelledPhase() {
	s.cancelled = true
	s.ShowingPopup = false
	s.PendingPassword = nil

	// Perform cleanup
	s.performCleanup()
}

// StartImport begins the import process with the selected files
func (s *EnhancedImportState) StartImport() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Phase != PhaseFileSelection {
		return fmt.Errorf("cannot start import from phase %s", s.Phase)
	}

	// Validate we have files selected
	if len(s.SelectedFiles) == 0 && s.SelectedDir == "" {
		return fmt.Errorf("no files or directory selected for import")
	}

	// Create import jobs
	var jobs []wallet.ImportJob
	var err error

	if s.SelectedDir != "" {
		// Import from directory
		jobs, err = s.BatchService.CreateImportJobsFromDirectory(s.SelectedDir)
	} else {
		// Import from selected files
		jobs, err = s.BatchService.CreateImportJobsFromFiles(s.SelectedFiles)
	}

	if err != nil {
		return fmt.Errorf("failed to create import jobs: %w", err)
	}

	// Validate import jobs
	if err := s.BatchService.ValidateImportJobs(jobs); err != nil {
		return fmt.Errorf("import job validation failed: %w", err)
	}

	s.ImportJobs = jobs

	// Transition to importing phase (call internal method to avoid double lock)
	return s.transitionToPhaseInternal(PhaseImporting)
}

// transitionToPhaseInternal transitions to a new phase without acquiring the lock
func (s *EnhancedImportState) transitionToPhaseInternal(newPhase ImportPhase) error {
	// Validate phase transition
	if !s.isValidTransition(s.Phase, newPhase) {
		return fmt.Errorf("invalid phase transition from %s to %s", s.Phase, newPhase)
	}

	s.Phase = newPhase

	// Perform phase-specific setup
	switch newPhase {
	case PhaseFileSelection:
		s.setupFileSelectionPhase()
	case PhaseImporting:
		s.setupImportingPhase()
	case PhasePasswordInput:
		s.setupPasswordInputPhase()
	case PhaseComplete:
		s.setupCompletePhase()
	case PhaseCancelled:
		s.setupCancelledPhase()
	}

	return nil
}

// ProcessImportBatch processes the import jobs in a separate goroutine
func (s *EnhancedImportState) ProcessImportBatch() tea.Cmd {
	return func() tea.Msg {
		// Process the batch import
		results := s.BatchService.ImportBatch(
			s.ImportJobs,
			s.progressChan,
			s.passwordRequestChan,
			s.passwordResponseChan,
		)

		return ImportBatchCompleteMsg{Results: results}
	}
}

// HandlePasswordRequest handles a password request from the batch service
func (s *EnhancedImportState) HandlePasswordRequest(request wallet.PasswordRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.PendingPassword = &request

	// Transition to password input phase
	return s.transitionToPhaseInternal(PhasePasswordInput)
}

// SubmitPassword submits a password response and transitions back to importing
func (s *EnhancedImportState) SubmitPassword(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Phase != PhasePasswordInput {
		return fmt.Errorf("cannot submit password from phase %s", s.Phase)
	}

	// Send password response
	response := wallet.PasswordResponse{
		Password:  password,
		Cancelled: false,
		Skip:      false,
	}

	select {
	case s.passwordResponseChan <- response:
		// Successfully sent response
	default:
		return fmt.Errorf("failed to send password response - channel unavailable")
	}

	// Clear pending password and transition back to importing
	s.PendingPassword = nil
	return s.transitionToPhaseInternal(PhaseImporting)
}

// CancelPasswordInput cancels the current password input
func (s *EnhancedImportState) CancelPasswordInput() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Phase != PhasePasswordInput {
		return fmt.Errorf("cannot cancel password input from phase %s", s.Phase)
	}

	// Send cancellation response
	response := wallet.PasswordResponse{
		Password:  "",
		Cancelled: true,
		Skip:      false,
	}

	select {
	case s.passwordResponseChan <- response:
		// Successfully sent cancellation
	default:
		return fmt.Errorf("failed to send password cancellation - channel unavailable")
	}

	// Clear pending password and transition back to importing
	s.PendingPassword = nil
	return s.transitionToPhaseInternal(PhaseImporting)
}

// SkipPasswordInput skips the current file and continues with import
func (s *EnhancedImportState) SkipPasswordInput() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Phase != PhasePasswordInput {
		return fmt.Errorf("cannot skip password input from phase %s", s.Phase)
	}

	// Send skip response
	response := wallet.PasswordResponse{
		Password:  "",
		Cancelled: false,
		Skip:      true,
	}

	select {
	case s.passwordResponseChan <- response:
		// Successfully sent skip
	default:
		return fmt.Errorf("failed to send password skip - channel unavailable")
	}

	// Clear pending password and transition back to importing
	s.PendingPassword = nil
	return s.transitionToPhaseInternal(PhaseImporting)
}

// CompleteImport marks the import as complete with results
func (s *EnhancedImportState) CompleteImport(results []wallet.ImportResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Results = results
	return s.transitionToPhaseInternal(PhaseComplete)
}

// CancelImport cancels the entire import process
func (s *EnhancedImportState) CancelImport() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.transitionToPhaseInternal(PhaseCancelled)
}

// UpdateProgress updates the current import progress with validation
func (s *EnhancedImportState) UpdateProgress(progress wallet.ImportProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate progress update
	if err := s.validateProgressUpdate(progress); err != nil {
		if uiLogger != nil {
			uiLogger.Warn("Invalid import progress update", logger.Error(err))
		}
		return
	}

	s.CurrentProgress = progress

	// Update progress bar if available
	if s.ProgressBar != nil {
		progressMsg := ImportProgressMsg{
			CurrentFile:    progress.CurrentFile,
			ProcessedFiles: progress.ProcessedFiles,
			TotalFiles:     progress.TotalFiles,
			Completed:      progress.ProcessedFiles >= progress.TotalFiles,
			Paused:         progress.PendingPassword,
			PauseReason:    "Waiting for password input",
		}

		// Add the most recent error if any
		if len(progress.Errors) > 0 {
			lastError := progress.Errors[len(progress.Errors)-1]
			progressMsg.Error = &ImportError{
				File:    lastError.File,
				Error:   lastError.Error,
				Skipped: lastError.Skipped,
			}
		}

		// Update the progress bar and store any returned command
		updatedProgressBar, cmd := s.ProgressBar.Update(progressMsg)
		s.ProgressBar = &updatedProgressBar
		s.pendingCommand = cmd
	}
}

// GetProgressChan returns the progress channel for listening to updates
func (s *EnhancedImportState) GetProgressChan() <-chan wallet.ImportProgress {
	return s.progressChan
}

// GetPasswordRequestChan returns the password request channel
func (s *EnhancedImportState) GetPasswordRequestChan() <-chan wallet.PasswordRequest {
	return s.passwordRequestChan
}

// GetPendingCommand returns and clears any pending command
func (s *EnhancedImportState) GetPendingCommand() tea.Cmd {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := s.pendingCommand
	s.pendingCommand = nil
	return cmd
}

// validateProgressUpdate validates a progress update for consistency
func (s *EnhancedImportState) validateProgressUpdate(progress wallet.ImportProgress) error {
	if progress.TotalFiles <= 0 {
		return fmt.Errorf("total files must be positive: %d", progress.TotalFiles)
	}

	if progress.ProcessedFiles < 0 {
		return fmt.Errorf("processed files cannot be negative: %d", progress.ProcessedFiles)
	}

	if progress.ProcessedFiles > progress.TotalFiles {
		return fmt.Errorf("processed files exceeds total: %d > %d",
			progress.ProcessedFiles, progress.TotalFiles)
	}

	// Validate percentage consistency
	expectedPercentage := float64(progress.ProcessedFiles) / float64(progress.TotalFiles) * 100
	if progress.ProcessedFiles == progress.TotalFiles {
		expectedPercentage = 100.0
	}

	tolerance := 1.0 // Allow 1% tolerance for floating point precision
	if progress.Percentage < 0 || progress.Percentage > 100 {
		return fmt.Errorf("percentage out of range: %.2f", progress.Percentage)
	}

	if abs(progress.Percentage-expectedPercentage) > tolerance {
		return fmt.Errorf("percentage inconsistent: %.2f vs expected %.2f",
			progress.Percentage, expectedPercentage)
	}

	// Validate against previous progress (if any)
	if s.CurrentProgress.TotalFiles > 0 {
		// Total files should remain consistent within the same batch
		if progress.TotalFiles != s.CurrentProgress.TotalFiles {
			return fmt.Errorf("total files changed during import: %d -> %d",
				s.CurrentProgress.TotalFiles, progress.TotalFiles)
		}

		// Processed files should not decrease (unless it's a reset to 0)
		if progress.ProcessedFiles < s.CurrentProgress.ProcessedFiles && progress.ProcessedFiles != 0 {
			return fmt.Errorf("processed files decreased: %d -> %d",
				s.CurrentProgress.ProcessedFiles, progress.ProcessedFiles)
		}
	}

	return nil
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// IsCompleted returns whether the import process is completed
func (s *EnhancedImportState) IsCompleted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.completed
}

// IsCancelled returns whether the import process was cancelled
func (s *EnhancedImportState) IsCancelled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cancelled
}

// GetResults returns the import results (thread-safe)
func (s *EnhancedImportState) GetResults() []wallet.ImportResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]wallet.ImportResult{}, s.Results...) // Return copy
}

// GetSummary returns a summary of the import results
func (s *EnhancedImportState) GetSummary() wallet.ImportSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.BatchService != nil {
		return s.BatchService.GetImportSummary(s.Results)
	}

	// Fallback summary calculation
	summary := wallet.ImportSummary{
		TotalFiles: len(s.Results),
	}

	for _, result := range s.Results {
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

// AddCleanupFunc adds a cleanup function to be called when the import is cancelled or completed
func (s *EnhancedImportState) AddCleanupFunc(cleanup func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupFuncs = append(s.cleanupFuncs, cleanup)
}

// performCleanup executes all registered cleanup functions
func (s *EnhancedImportState) performCleanup() {
	for _, cleanup := range s.cleanupFuncs {
		if cleanup != nil {
			cleanup()
		}
	}
	s.cleanupFuncs = []func(){}

	// Close channels if they're still open
	s.closeChannelsSafely()
}

// closeChannelsSafely closes communication channels safely
func (s *EnhancedImportState) closeChannelsSafely() {
	// Note: We don't close channels here as they might still be in use
	// The batch service is responsible for closing the progress channel
	// The password channels are managed by the UI layer
}

// Cleanup performs final cleanup when the state manager is no longer needed
func (s *EnhancedImportState) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.performCleanup()
}

// HandleCompletionUpdate handles updates from the completion component
func (s *EnhancedImportState) HandleCompletionUpdate(msg tea.Msg) tea.Cmd {
	if s.Phase != PhaseComplete || s.Completion == nil {
		return nil
	}

	var cmd tea.Cmd
	*s.Completion, cmd = s.Completion.Update(msg)

	// Handle completion-specific messages
	switch msg := msg.(type) {
	case RetryImportMsg:
		return s.handleRetryRequest(msg.Strategy)
	case RetrySpecificFileMsg:
		return s.handleRetrySpecificFile(msg.File)
	case ReturnToMenuMsg:
		return s.handleReturnToMenu()
	case SelectDifferentFilesMsg:
		return s.handleSelectDifferentFiles()
	}

	return cmd
}

// handleRetryRequest handles retry requests from the completion phase
func (s *EnhancedImportState) handleRetryRequest(strategy string) tea.Cmd {
	return func() tea.Msg {
		return RetryImportRequestMsg{
			Strategy: strategy,
			Files:    s.getRetryableFiles(strategy),
		}
	}
}

// handleRetrySpecificFile handles retry requests for a specific file
func (s *EnhancedImportState) handleRetrySpecificFile(file string) tea.Cmd {
	return func() tea.Msg {
		return RetryImportRequestMsg{
			Strategy: "retry_specific",
			Files:    []string{file},
		}
	}
}

// handleReturnToMenu handles return to menu requests
func (s *EnhancedImportState) handleReturnToMenu() tea.Cmd {
	return func() tea.Msg {
		return ReturnToFileSelectionMsg{}
	}
}

// handleSelectDifferentFiles handles requests to select different files
func (s *EnhancedImportState) handleSelectDifferentFiles() tea.Cmd {
	return func() tea.Msg {
		return ReturnToFileSelectionMsg{}
	}
}

// getRetryableFiles returns files that can be retried based on strategy
func (s *EnhancedImportState) getRetryableFiles(strategy string) []string {
	var files []string

	switch strategy {
	case "retry_failed":
		// Return all failed (non-skipped) files
		for _, result := range s.Results {
			if !result.Success && !result.Skipped {
				files = append(files, result.Job.KeystorePath)
			}
		}
	case "manual_passwords":
		// Return all failed files that might need manual password input
		for _, result := range s.Results {
			if !result.Success && s.isPasswordRelatedError(result.Error) {
				files = append(files, result.Job.KeystorePath)
			}
		}
	case "retry_specific":
		// This is handled separately in handleRetrySpecificFile
	}

	return files
}

// isPasswordRelatedError checks if an error is password-related
func (s *EnhancedImportState) isPasswordRelatedError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := strings.ToLower(err.Error())
	return strings.Contains(errorMsg, "password") ||
		strings.Contains(errorMsg, "decrypt") ||
		strings.Contains(errorMsg, "incorrect") ||
		strings.Contains(errorMsg, "invalid")
}

// RestartWithFiles restarts the import process with new files
func (s *EnhancedImportState) RestartWithFiles(files []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset to file selection phase
	if err := s.transitionToPhaseInternal(PhaseFileSelection); err != nil {
		return err
	}

	// Set the new files
	s.SelectedFiles = files
	s.SelectedDir = ""

	return nil
}

// GetCompletionSummary returns the completion summary if available
func (s *EnhancedImportState) GetCompletionSummary() *wallet.ImportSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.Completion != nil {
		summary := s.Completion.GetSummary()
		return &summary
	}

	return nil
}

// GetStateInfo returns current state information for debugging
func (s *EnhancedImportState) GetStateInfo() StateInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return StateInfo{
		Phase:           s.Phase,
		SelectedFiles:   len(s.SelectedFiles),
		SelectedDir:     s.SelectedDir,
		ImportJobs:      len(s.ImportJobs),
		Results:         len(s.Results),
		ShowingPopup:    s.ShowingPopup,
		PendingPassword: s.PendingPassword != nil,
		Completed:       s.completed,
		Cancelled:       s.cancelled,
		ErrorMessage:    s.errorMessage,
	}
}

// StateInfo provides debugging information about the current state
type StateInfo struct {
	Phase           ImportPhase
	SelectedFiles   int
	SelectedDir     string
	ImportJobs      int
	Results         int
	ShowingPopup    bool
	PendingPassword bool
	Completed       bool
	Cancelled       bool
	ErrorMessage    string
}

// String returns a string representation of the state info
func (si StateInfo) String() string {
	return fmt.Sprintf("Phase: %s, Files: %d, Dir: %s, Jobs: %d, Results: %d, Popup: %t, Pending: %t, Complete: %t, Cancelled: %t",
		si.Phase, si.SelectedFiles, si.SelectedDir, si.ImportJobs, si.Results, si.ShowingPopup, si.PendingPassword, si.Completed, si.Cancelled)
}

// Custom messages for the BubbleTea update loop
type ImportBatchCompleteMsg struct {
	Results []wallet.ImportResult
}

type ImportProgressUpdateMsg struct {
	Progress wallet.ImportProgress
}

type PasswordRequestMsg struct {
	Request wallet.PasswordRequest
}

// ContinueListeningMsg indicates that listening should continue
type ContinueListeningMsg struct{}

// Completion phase messages
type CompletionUpdateMsg struct {
	Action CompletionAction
}

type RetryImportRequestMsg struct {
	Strategy string
	Files    []string
}

type ReturnToFileSelectionMsg struct{}

type ViewErrorDetailsMsg struct {
	ErrorIndex int
}

// Init implements the tea.Model interface
func (s *EnhancedImportState) Init() tea.Cmd {
	// Initialize file picker if we're in file selection phase
	if s.Phase == PhaseFileSelection && s.FilePicker != nil {
		return s.FilePicker.Init()
	}
	return nil
}

// Update implements the tea.Model interface
func (s *EnhancedImportState) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch s.Phase {
	case PhaseFileSelection:
		if s.FilePicker != nil {
			var cmd tea.Cmd
			*s.FilePicker, cmd = s.FilePicker.Update(msg)

			// Sync selected files from file picker
			s.syncSelectedFiles()

			return s, cmd
		}

	case PhaseImporting:
		if s.ProgressBar != nil {
			var cmd tea.Cmd
			*s.ProgressBar, cmd = s.ProgressBar.Update(msg)
			return s, cmd
		}

	case PhasePasswordInput:
		if s.ShowingPopup && s.PasswordPopup != nil {
			var cmd tea.Cmd
			*s.PasswordPopup, cmd = s.PasswordPopup.Update(msg)
			return s, cmd
		}

	case PhaseComplete:
		return s, s.HandleCompletionUpdate(msg)

	case PhaseCancelled:
		// Handle basic navigation in cancelled state
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "enter" || msg.String() == "esc" {
				return s, func() tea.Msg {
					return ReturnToMenuMsg{}
				}
			}
		}
	}

	return s, nil
}

// View renders the current state based on the active phase
func (s *EnhancedImportState) View() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch s.Phase {
	case PhaseFileSelection:
		if s.FilePicker != nil {
			return s.FilePicker.View()
		}
		return "File picker not initialized"

	case PhaseImporting:
		if s.ProgressBar != nil {
			return s.ProgressBar.View()
		}
		return "Progress bar not initialized"

	case PhasePasswordInput:
		if s.ShowingPopup && s.PasswordPopup != nil {
			// Show password popup overlay on top of progress bar
			popup := s.PasswordPopup.View()

			// Overlay popup on background
			return lipgloss.Place(80, 24, lipgloss.Center, lipgloss.Center, popup)
		}
		return "Password popup not initialized"

	case PhaseComplete:
		if s.Completion != nil {
			return s.Completion.View()
		}
		return s.renderCompletionView()

	case PhaseCancelled:
		return s.renderCancellationView()

	default:
		return fmt.Sprintf("Unknown phase: %s", s.Phase)
	}
}

// renderCompletionView renders the completion phase view
func (s *EnhancedImportState) renderCompletionView() string {
	summary := s.GetSummary()

	var sections []string

	// Title
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("70")).Render("✓ Import Complete")
	sections = append(sections, title)

	// Summary statistics
	stats := fmt.Sprintf("Total: %d | Success: %d | Failed: %d | Skipped: %d",
		summary.TotalFiles, summary.SuccessfulImports, summary.FailedImports, summary.SkippedImports)
	sections = append(sections, stats)

	// Elapsed time
	if !s.startTime.IsZero() {
		elapsed := time.Since(s.startTime)
		sections = append(sections, fmt.Sprintf("Completed in: %v", elapsed.Round(time.Second)))
	}

	// Show errors if any
	if len(summary.Errors) > 0 {
		sections = append(sections, "")
		sections = append(sections, "Errors:")
		for _, err := range summary.Errors {
			errorType := "Failed"
			if err.Skipped {
				errorType = "Skipped"
			}
			sections = append(sections, fmt.Sprintf("• %s: %s", errorType, err.File))
		}
	}

	// Instructions
	sections = append(sections, "")
	sections = append(sections, "Press ENTER to return to menu or R to retry failed imports")

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderCancellationView renders the cancellation phase view
func (s *EnhancedImportState) renderCancellationView() string {
	var sections []string

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("✗ Import Cancelled")
	sections = append(sections, title)

	if len(s.Results) > 0 {
		summary := s.GetSummary()
		stats := fmt.Sprintf("Processed: %d/%d files before cancellation", len(s.Results), len(s.ImportJobs))
		sections = append(sections, stats)

		if summary.SuccessfulImports > 0 {
			sections = append(sections, fmt.Sprintf("Successfully imported: %d wallets", summary.SuccessfulImports))
		}
	}

	sections = append(sections, "")
	sections = append(sections, "Press ENTER to return to menu")

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// fileExists checks if a file or directory exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// syncSelectedFiles synchronizes selected files from the file picker
func (s *EnhancedImportState) syncSelectedFiles() {
	if s.FilePicker == nil {
		return
	}

	// Get selected files from file picker
	result := s.FilePicker.GetResult()
	s.SelectedFiles = result.Files
	s.SelectedDir = result.Directory
}
