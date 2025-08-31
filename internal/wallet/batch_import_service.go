package wallet

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/keystore"
)

// ImportJob represents a single keystore import job
type ImportJob struct {
	KeystorePath   string // Path to the keystore file
	PasswordPath   string // Path to the password file (if exists)
	ManualPassword string // Manual password (if no password file)
	WalletName     string // Name for the imported wallet
	RequiresInput  bool   // Whether this job requires manual password input
}

// ImportResult represents the result of a single import operation
type ImportResult struct {
	Job     ImportJob      // The original import job
	Success bool           // Whether the import was successful
	Wallet  *WalletDetails // The imported wallet details (if successful)
	Error   error          // Error that occurred (if any)
	Skipped bool           // Whether the import was skipped by user
}

// ImportProgress represents the current progress of a batch import operation
type ImportProgress struct {
	CurrentFile     string        // Currently processing file
	TotalFiles      int           // Total number of files to process
	ProcessedFiles  int           // Number of files processed so far
	Percentage      float64       // Completion percentage (0-100)
	Errors          []ImportError // List of errors encountered
	PendingPassword bool          // Whether waiting for password input
	PendingFile     string        // File waiting for password input
	StartTime       time.Time     // When the import started
	ElapsedTime     time.Duration // Time elapsed since start
}

// ImportError represents an error that occurred during import
type ImportError struct {
	File    string // File that caused the error
	Error   error  // The actual error
	Skipped bool   // Whether this was a user skip vs actual error
}

// PasswordRequest represents a request for manual password input
type PasswordRequest struct {
	KeystoreFile string // The keystore file needing a password
	AttemptCount int    // Number of attempts made so far
	ErrorMessage string // Error message from previous attempt (if any)
	IsRetry      bool   // Whether this is a retry after failed attempt
}

// PasswordResponse represents the user's response to a password request
type PasswordResponse struct {
	Password  string // The provided password
	Cancelled bool   // Whether the user cancelled
	Skip      bool   // Whether the user chose to skip this file
}

// PasswordInputError represents errors that occur during password input
type PasswordInputError struct {
	Type    PasswordInputErrorType
	Message string
	File    string
}

// Error implements the error interface
func (e *PasswordInputError) Error() string {
	return e.Message
}

// IsSkipped returns true if this error represents a user skip action
func (e *PasswordInputError) IsSkipped() bool {
	return e.Type == PasswordInputSkipped
}

// IsCancelled returns true if this error represents a user cancellation
func (e *PasswordInputError) IsCancelled() bool {
	return e.Type == PasswordInputCancelled
}

// PasswordInputErrorType represents different types of password input errors
type PasswordInputErrorType int

const (
	PasswordInputCancelled PasswordInputErrorType = iota
	PasswordInputSkipped
	PasswordInputTimeout
	PasswordInputInvalid
	PasswordInputMaxAttemptsExceeded
)

// String returns a string representation of the password input error type
func (t PasswordInputErrorType) String() string {
	switch t {
	case PasswordInputCancelled:
		return "CANCELLED"
	case PasswordInputSkipped:
		return "SKIPPED"
	case PasswordInputTimeout:
		return "TIMEOUT"
	case PasswordInputInvalid:
		return "INVALID"
	case PasswordInputMaxAttemptsExceeded:
		return "MAX_ATTEMPTS_EXCEEDED"
	default:
		return "UNKNOWN"
	}
}

// BatchImportService handles batch import operations with password integration
type BatchImportService struct {
	walletService   *WalletService
	passwordMgr     *PasswordFileManager
	errorAggregator *ErrorAggregator
	mu              sync.RWMutex // Protects concurrent access to service state
}

// NewBatchImportService creates a new BatchImportService instance
func NewBatchImportService(walletService *WalletService) *BatchImportService {
	return &BatchImportService{
		walletService: walletService,
		passwordMgr:   NewPasswordFileManager(),
	}
}

// CreateImportJobsFromFiles creates import jobs from a list of keystore file paths
func (bis *BatchImportService) CreateImportJobsFromFiles(keystorePaths []string) ([]ImportJob, error) {
	if len(keystorePaths) == 0 {
		return nil, fmt.Errorf("no keystore files provided")
	}

	var jobs []ImportJob

	for _, keystorePath := range keystorePaths {
		// Validate that the file exists and is accessible
		if _, err := os.Stat(keystorePath); os.IsNotExist(err) {
			return nil, fmt.Errorf("keystore file not found: %s", keystorePath)
		} else if err != nil {
			return nil, fmt.Errorf("cannot access keystore file %s: %v", keystorePath, err)
		}

		// Generate wallet name from filename
		baseName := filepath.Base(keystorePath)
		walletName := strings.TrimSuffix(baseName, filepath.Ext(baseName))

		// Check for password file
		passwordPath, err := bis.passwordMgr.FindPasswordFile(keystorePath)
		requiresInput := err != nil // If we can't find password file, manual input required

		job := ImportJob{
			KeystorePath:  keystorePath,
			PasswordPath:  passwordPath,
			WalletName:    walletName,
			RequiresInput: requiresInput,
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// CreateImportJobsFromDirectory creates import jobs by scanning a directory for keystore files
func (bis *BatchImportService) CreateImportJobsFromDirectory(dirPath string) ([]ImportJob, error) {
	if dirPath == "" {
		return nil, fmt.Errorf("directory path cannot be empty")
	}

	// Validate directory exists
	dirInfo, err := os.Stat(dirPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("directory not found: %s", dirPath)
	} else if err != nil {
		return nil, fmt.Errorf("cannot access directory %s: %v", dirPath, err)
	}

	if !dirInfo.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", dirPath)
	}

	// Scan directory for keystore files
	keystoreFiles, scanErrors, err := bis.ScanDirectoryForKeystores(dirPath)
	if err != nil {
		return nil, fmt.Errorf("error scanning directory %s: %v", dirPath, err)
	}

	if len(keystoreFiles) == 0 {
		if len(scanErrors) > 0 {
			return nil, fmt.Errorf("no valid keystore files found in directory: %s (found %d invalid files)", dirPath, len(scanErrors))
		}
		return nil, fmt.Errorf("no valid keystore files found in directory: %s", dirPath)
	}

	// Create import jobs from found files
	return bis.CreateImportJobsFromFiles(keystoreFiles)
}

// ScanDirectoryForKeystores recursively scans a directory for valid keystore files
func (bis *BatchImportService) ScanDirectoryForKeystores(dirPath string) ([]string, []DirectoryScanError, error) {
	var keystoreFiles []string
	var scanErrors []DirectoryScanError

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Record access errors but continue scanning
			scanErrors = append(scanErrors, DirectoryScanError{
				Path:  path,
				Error: fmt.Errorf("access error: %w", err),
				Type:  ScanErrorAccess,
			})
			return nil // Continue walking despite access errors
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if it's a JSON file
		if strings.ToLower(filepath.Ext(path)) == ".json" {
			// Validate if it's a proper keystore file
			if bis.isValidKeystoreFile(path) {
				keystoreFiles = append(keystoreFiles, path)
			} else {
				// Record invalid keystore files for reporting
				scanErrors = append(scanErrors, DirectoryScanError{
					Path:  path,
					Error: fmt.Errorf("invalid keystore format"),
					Type:  ScanErrorInvalidKeystore,
				})
			}
		}

		return nil
	})

	if err != nil {
		return nil, scanErrors, fmt.Errorf("directory walk failed: %w", err)
	}

	return keystoreFiles, scanErrors, nil
}

// isValidKeystoreFile performs comprehensive validation to check if a JSON file is a valid keystore
func (bis *BatchImportService) isValidKeystoreFile(filePath string) bool {
	// Read the entire file for proper validation
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}

	// Use the KeystoreValidator for comprehensive validation
	validator := &KeystoreValidator{}
	_, err = validator.ValidateKeystoreV3(data)

	// Return true only if the file passes full keystore validation
	return err == nil
}

// DirectoryScanError represents an error encountered during directory scanning
type DirectoryScanError struct {
	Path  string        // File path that caused the error
	Error error         // The actual error
	Type  ScanErrorType // Type of scan error
}

// ScanErrorType represents different types of directory scan errors
type ScanErrorType int

const (
	ScanErrorAccess ScanErrorType = iota
	ScanErrorInvalidKeystore
	ScanErrorReadFailure
)

// String returns a string representation of the scan error type
func (t ScanErrorType) String() string {
	switch t {
	case ScanErrorAccess:
		return "ACCESS_ERROR"
	case ScanErrorInvalidKeystore:
		return "INVALID_KEYSTORE"
	case ScanErrorReadFailure:
		return "READ_FAILURE"
	default:
		return "UNKNOWN_ERROR"
	}
}

// GetKeystoreDiscoveryReport generates a report of keystore discovery results
func (bis *BatchImportService) GetKeystoreDiscoveryReport(dirPath string) (*KeystoreDiscoveryReport, error) {
	keystoreFiles, scanErrors, err := bis.ScanDirectoryForKeystores(dirPath)
	if err != nil {
		return nil, err
	}

	report := &KeystoreDiscoveryReport{
		DirectoryPath:   dirPath,
		ValidKeystores:  keystoreFiles,
		ScanErrors:      scanErrors,
		TotalFilesFound: len(keystoreFiles) + len(scanErrors),
		ValidFilesCount: len(keystoreFiles),
		ErrorFilesCount: len(scanErrors),
	}

	// Count password files for each valid keystore
	for _, keystorePath := range keystoreFiles {
		passwordPath, err := bis.passwordMgr.FindPasswordFile(keystorePath)
		if err == nil && passwordPath != "" {
			report.PasswordFilesFound++
		}
	}

	return report, nil
}

// KeystoreDiscoveryReport represents the results of scanning a directory for keystores
type KeystoreDiscoveryReport struct {
	DirectoryPath      string               // The directory that was scanned
	ValidKeystores     []string             // List of valid keystore file paths
	ScanErrors         []DirectoryScanError // List of errors encountered during scanning
	TotalFilesFound    int                  // Total number of files processed
	ValidFilesCount    int                  // Number of valid keystore files found
	ErrorFilesCount    int                  // Number of files with errors
	PasswordFilesFound int                  // Number of corresponding password files found
}

// ImportBatch processes a batch of import jobs with progress reporting and password handling
func (bis *BatchImportService) ImportBatch(
	jobs []ImportJob,
	progressChan chan<- ImportProgress,
	passwordRequestChan chan<- PasswordRequest,
	passwordResponseChan <-chan PasswordResponse,
) []ImportResult {
	bis.mu.Lock()
	defer bis.mu.Unlock()

	if len(jobs) == 0 {
		close(progressChan)
		return []ImportResult{}
	}

	startTime := time.Now()
	results := make([]ImportResult, 0, len(jobs))
	var errors []ImportError

	// Initialize error aggregator for this batch
	bis.errorAggregator = NewErrorAggregator(len(jobs))

	// Send initial progress
	progress := ImportProgress{
		CurrentFile:     "",
		TotalFiles:      len(jobs),
		ProcessedFiles:  0,
		Percentage:      0.0,
		Errors:          errors,
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       startTime,
		ElapsedTime:     0,
	}

	bis.sendProgressUpdate(progress, progressChan)

	// Process each job
	for i, job := range jobs {
		// Update progress for current file
		progress.CurrentFile = filepath.Base(job.KeystorePath)
		progress.ProcessedFiles = i
		progress.Percentage = float64(i) / float64(len(jobs)) * 100
		progress.ElapsedTime = time.Since(startTime)

		bis.sendProgressUpdate(progress, progressChan)

		// Process the import job
		result := bis.processImportJob(job, passwordRequestChan, passwordResponseChan, &progress, progressChan)
		results = append(results, result)

		// Track errors and skipped files using error aggregator
		if !result.Success {
			userAction := UserActionNone
			if result.Skipped {
				userAction = UserActionSkip
			}
			bis.errorAggregator.AddError(result.Error, job.KeystorePath, userAction)

			errors = append(errors, ImportError{
				File:    job.KeystorePath,
				Error:   result.Error,
				Skipped: result.Skipped,
			})
		} else {
			bis.errorAggregator.AddSuccess()
		}

		progress.Errors = errors
	}

	// Send final progress
	progress.CurrentFile = ""
	progress.ProcessedFiles = len(jobs)
	progress.Percentage = 100.0
	progress.PendingPassword = false
	progress.PendingFile = ""
	progress.ElapsedTime = time.Since(startTime)

	bis.sendProgressUpdate(progress, progressChan)

	close(progressChan)
	return results
}

// processImportJob processes a single import job with enhanced error handling
func (bis *BatchImportService) processImportJob(
	job ImportJob,
	passwordRequestChan chan<- PasswordRequest,
	passwordResponseChan <-chan PasswordResponse,
	progress *ImportProgress,
	progressChan chan<- ImportProgress,
) ImportResult {
	var password string
	var err error

	// Try to get password from file first
	if job.PasswordPath != "" {
		password, err = bis.passwordMgr.ReadPasswordFile(job.PasswordPath)
		if err != nil {
			// Password file exists but can't be read, fall back to manual input
			job.RequiresInput = true
			password = "" // Clear any partial password
		}
	}

	// If we need manual password input
	if job.RequiresInput && password == "" {
		password, err = bis.requestManualPassword(job.KeystorePath, passwordRequestChan, passwordResponseChan, progress, progressChan)
		if err != nil {
			// Check if this is a password input error
			if passwordErr, ok := err.(*PasswordInputError); ok {
				return ImportResult{
					Job:     job,
					Success: false,
					Wallet:  nil,
					Error:   passwordErr,
					Skipped: passwordErr.IsSkipped() || passwordErr.IsCancelled(),
				}
			}

			// Other types of errors during password input
			return ImportResult{
				Job:     job,
				Success: false,
				Wallet:  nil,
				Error:   err,
				Skipped: false,
			}
		}
	}

	// Use manual password if provided in job (overrides file password)
	if job.ManualPassword != "" {
		password = job.ManualPassword
	}

	// Validate we have a password before attempting import
	if password == "" {
		return ImportResult{
			Job:     job,
			Success: false,
			Wallet:  nil,
			Error:   fmt.Errorf("no password available for keystore import"),
			Skipped: false,
		}
	}

	// Attempt the import with progress tracking
	walletDetails, err := bis.walletService.ImportWalletFromKeystoreV3WithProgress(job.WalletName, job.KeystorePath, password, progressChan)
	if err != nil {
		return ImportResult{
			Job:     job,
			Success: false,
			Wallet:  nil,
			Error:   fmt.Errorf("keystore import failed: %w", err),
			Skipped: false,
		}
	}

	return ImportResult{
		Job:     job,
		Success: true,
		Wallet:  walletDetails,
		Error:   nil,
		Skipped: false,
	}
}

// requestManualPassword requests manual password input from the user with retry mechanism
func (bis *BatchImportService) requestManualPassword(
	keystoreFile string,
	passwordRequestChan chan<- PasswordRequest,
	passwordResponseChan <-chan PasswordResponse,
	progress *ImportProgress,
	progressChan chan<- ImportProgress,
) (string, error) {
	const maxRetries = 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Update progress to show we're waiting for password
		progress.PendingPassword = true
		progress.PendingFile = filepath.Base(keystoreFile)

		// Send progress update
		bis.sendProgressUpdate(*progress, progressChan)

		// Create password request with appropriate error messaging
		var previousError error
		if attempt > 1 {
			// For retry attempts, assume it was an incorrect password
			previousError = fmt.Errorf("incorrect password")
		}
		request := bis.createPasswordRequest(keystoreFile, attempt, previousError)

		// Send password request
		if err := bis.sendPasswordRequest(request, passwordRequestChan); err != nil {
			// Clear pending state and return error if we can't send request
			progress.PendingPassword = false
			progress.PendingFile = ""
			return "", &PasswordInputError{
				Type:    PasswordInputTimeout,
				Message: "failed to send password request - communication error",
				File:    keystoreFile,
			}
		}

		// Wait for response with timeout
		select {
		case response := <-passwordResponseChan:
			// Clear pending password state
			progress.PendingPassword = false
			progress.PendingFile = ""

			// Send progress update to clear pending state
			bis.sendProgressUpdate(*progress, progressChan)

			// Handle user cancellation
			if response.Cancelled {
				return "", &PasswordInputError{
					Type:    PasswordInputCancelled,
					Message: "password input cancelled by user",
					File:    keystoreFile,
				}
			}

			// Handle user skip
			if response.Skip {
				return "", &PasswordInputError{
					Type:    PasswordInputSkipped,
					Message: "import skipped by user",
					File:    keystoreFile,
				}
			}

			// Validate password is not empty
			if response.Password == "" {
				if attempt < maxRetries {
					continue // Try again with empty password error
				}
				return "", &PasswordInputError{
					Type:    PasswordInputInvalid,
					Message: "empty password provided",
					File:    keystoreFile,
				}
			}

			// Test the provided password
			if bis.testKeystorePassword(keystoreFile, response.Password) {
				return response.Password, nil
			}

			// Password was incorrect
			if attempt < maxRetries {
				// Send another request with error indication for retry
				continue
			}

			// Maximum attempts reached with incorrect password
			return "", &PasswordInputError{
				Type:    PasswordInputMaxAttemptsExceeded,
				Message: fmt.Sprintf("incorrect password after %d attempts", maxRetries),
				File:    keystoreFile,
			}

		case <-time.After(5 * time.Minute): // Timeout after 5 minutes
			// Clear pending state on timeout
			progress.PendingPassword = false
			progress.PendingFile = ""

			bis.sendProgressUpdate(*progress, progressChan)

			return "", &PasswordInputError{
				Type:    PasswordInputTimeout,
				Message: "password input timeout after 5 minutes",
				File:    keystoreFile,
			}
		}
	}

	return "", &PasswordInputError{
		Type:    PasswordInputMaxAttemptsExceeded,
		Message: "maximum password attempts exceeded",
		File:    keystoreFile,
	}
}

// testKeystorePassword tests if a password can decrypt a keystore file
func (bis *BatchImportService) testKeystorePassword(keystorePath, password string) bool {
	// Read the keystore file
	keyJSON, err := os.ReadFile(keystorePath)
	if err != nil {
		return false
	}

	// Try to decrypt using the go-ethereum keystore package directly
	// This avoids creating a wallet in the database during testing
	_, err = keystore.DecryptKey(keyJSON, password)
	return err == nil
}

// createPasswordRequest creates a password request with appropriate error messaging
func (bis *BatchImportService) createPasswordRequest(keystoreFile string, attempt int, previousError error) PasswordRequest {
	request := PasswordRequest{
		KeystoreFile: keystoreFile,
		AttemptCount: attempt,
		IsRetry:      attempt > 1,
	}

	// Set appropriate error message based on previous attempt
	if attempt > 1 && previousError != nil {
		if passwordErr, ok := previousError.(*PasswordInputError); ok {
			switch passwordErr.Type {
			case PasswordInputInvalid:
				request.ErrorMessage = "Password cannot be empty. Please enter a valid password."
			default:
				request.ErrorMessage = "Incorrect password. Please try again."
			}
		} else {
			// For generic errors, assume incorrect password
			request.ErrorMessage = "Incorrect password. Please try again."
		}
	}

	return request
}

// sendPasswordRequest safely sends a password request through the channel
func (bis *BatchImportService) sendPasswordRequest(request PasswordRequest, passwordRequestChan chan<- PasswordRequest) error {
	select {
	case passwordRequestChan <- request:
		return nil
	default:
		return fmt.Errorf("password request channel is unavailable or closed")
	}
}

// sendProgressUpdate safely sends a progress update through the channel
func (bis *BatchImportService) sendProgressUpdate(progress ImportProgress, progressChan chan<- ImportProgress) {
	if progressChan == nil {
		return // No progress channel provided, skip update
	}

	// Use buffered send with longer timeout to ensure progress updates are delivered
	select {
	case progressChan <- progress:
		// Successfully sent progress update
	case <-time.After(500 * time.Millisecond):
		// Timeout after 500ms - this allows more time for UI to process
		// Log the dropped update for debugging
		log.Printf("Progress update dropped - channel may be blocked (file: %s, progress: %.1f%%)",
			progress.CurrentFile, progress.Percentage)
	}
}

// GetImportSummary creates a summary of import results
func (bis *BatchImportService) GetImportSummary(results []ImportResult) ImportSummary {
	summary := ImportSummary{
		TotalFiles:        len(results),
		SuccessfulImports: 0,
		FailedImports:     0,
		SkippedImports:    0,
		Errors:            []ImportError{},
	}

	for _, result := range results {
		if result.Success {
			summary.SuccessfulImports++
		} else if result.Skipped {
			summary.SkippedImports++
		} else {
			summary.FailedImports++
			summary.Errors = append(summary.Errors, ImportError{
				File:    result.Job.KeystorePath,
				Error:   result.Error,
				Skipped: result.Skipped,
			})
		}
	}

	return summary
}

// ImportSummary represents a summary of batch import results
type ImportSummary struct {
	TotalFiles        int           // Total number of files processed
	SuccessfulImports int           // Number of successful imports
	FailedImports     int           // Number of failed imports
	SkippedImports    int           // Number of skipped imports
	Errors            []ImportError // List of errors that occurred
}

// ValidateImportJobs validates a list of import jobs before processing
func (bis *BatchImportService) ValidateImportJobs(jobs []ImportJob) error {
	if len(jobs) == 0 {
		return NewKeystoreImportErrorWithRecovery(
			ErrorImportJobValidation,
			"no import jobs provided",
			"",
			true,
			"select_files_or_directory",
			nil,
		)
	}

	for i, job := range jobs {
		// Validate keystore path
		if job.KeystorePath == "" {
			return NewKeystoreImportErrorWithRecovery(
				ErrorImportJobValidation,
				fmt.Sprintf("job %d: keystore path cannot be empty", i),
				"",
				true,
				"provide_valid_keystore_path",
				nil,
			)
		}

		// Check if keystore file exists
		if _, err := os.Stat(job.KeystorePath); os.IsNotExist(err) {
			return NewKeystoreImportErrorWithRecovery(
				ErrorFileNotFound,
				fmt.Sprintf("job %d: keystore file not found: %s", i, job.KeystorePath),
				job.KeystorePath,
				true,
				"select_existing_keystore_file",
				err,
			)
		} else if err != nil {
			return NewKeystoreImportErrorWithRecovery(
				ErrorFileNotFound,
				fmt.Sprintf("job %d: cannot access keystore file %s: %v", i, job.KeystorePath, err),
				job.KeystorePath,
				true,
				"fix_file_permissions",
				err,
			)
		}

		// Validate wallet name
		if job.WalletName == "" {
			return NewKeystoreImportErrorWithRecovery(
				ErrorImportJobValidation,
				fmt.Sprintf("job %d: wallet name cannot be empty", i),
				job.KeystorePath,
				true,
				"provide_wallet_name",
				nil,
			)
		}

		// If password path is specified, validate it exists
		if job.PasswordPath != "" {
			if err := bis.passwordMgr.ValidatePasswordFile(job.PasswordPath); err != nil {
				// Mark as requiring input if password file is invalid
				jobs[i].RequiresInput = true
				jobs[i].PasswordPath = "" // Clear invalid path

				// This is not a fatal error, just log it for recovery recommendations
				if bis.errorAggregator != nil {
					bis.errorAggregator.AddError(err, job.PasswordPath, UserActionNone)
				}
			}
		}
	}

	return nil
}

// GetErrorReport returns a comprehensive error report for the last batch operation
func (bis *BatchImportService) GetErrorReport() *ErrorReport {
	bis.mu.RLock()
	defer bis.mu.RUnlock()

	if bis.errorAggregator == nil {
		return nil
	}

	report := bis.errorAggregator.GenerateErrorReport()
	return &report
}

// GetRetryRecommendations returns recommendations for retrying failed operations
func (bis *BatchImportService) GetRetryRecommendations() []RetryRecommendation {
	bis.mu.RLock()
	defer bis.mu.RUnlock()

	if bis.errorAggregator == nil {
		return []RetryRecommendation{}
	}

	return bis.errorAggregator.GetRetryRecommendations()
}

// HasRecoverableErrors returns true if there are errors that can be recovered from
func (bis *BatchImportService) HasRecoverableErrors() bool {
	bis.mu.RLock()
	defer bis.mu.RUnlock()

	if bis.errorAggregator == nil {
		return false
	}

	recoverableErrors := bis.errorAggregator.GetRecoverableErrors()
	return len(recoverableErrors) > 0
}

// GetErrorSummary returns a summary of errors from the last batch operation
func (bis *BatchImportService) GetErrorSummary() *ErrorSummary {
	bis.mu.RLock()
	defer bis.mu.RUnlock()

	if bis.errorAggregator == nil {
		return nil
	}

	summary := bis.errorAggregator.GetErrorSummary()
	return &summary
}

// CreateRecoveryJobs creates new import jobs for recoverable errors
func (bis *BatchImportService) CreateRecoveryJobs(originalJobs []ImportJob, strategy string) ([]ImportJob, error) {
	bis.mu.RLock()
	defer bis.mu.RUnlock()

	if bis.errorAggregator == nil {
		return nil, NewKeystoreImportError(ErrorBatchImportFailed, "no error aggregator available", nil)
	}

	recoverableErrors := bis.errorAggregator.GetRecoverableErrors()
	if len(recoverableErrors) == 0 {
		return []ImportJob{}, nil
	}

	var recoveryJobs []ImportJob

	// Create a map of original jobs by file path for quick lookup
	jobMap := make(map[string]ImportJob)
	for _, job := range originalJobs {
		jobMap[job.KeystorePath] = job
	}

	// Create recovery jobs based on strategy
	for _, err := range recoverableErrors {
		if originalJob, exists := jobMap[err.File]; exists {
			recoveryJob := originalJob

			// Modify job based on recovery strategy
			switch strategy {
			case "manual_password_input":
				recoveryJob.RequiresInput = true
				recoveryJob.PasswordPath = ""
				recoveryJob.ManualPassword = ""
			case "reselect_files":
				// This would require user interaction, skip for now
				continue
			case "fix_file_permissions":
				// This would require external action, skip for now
				continue
			default:
				// Default recovery: mark as requiring manual input
				recoveryJob.RequiresInput = true
			}

			recoveryJobs = append(recoveryJobs, recoveryJob)
		}
	}

	return recoveryJobs, nil
}
