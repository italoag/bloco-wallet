package wallet

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ErrorAggregator collects and categorizes errors from batch operations
type ErrorAggregator struct {
	errors          []AggregatedError
	startTime       time.Time
	totalOperations int
	successCount    int
	failureCount    int
	skipCount       int
}

// AggregatedError represents an error with additional context for batch operations
type AggregatedError struct {
	OriginalError error
	File          string
	ErrorType     KeystoreErrorType
	Category      ErrorCategory
	Recoverable   bool
	RecoveryHint  string
	Timestamp     time.Time
	AttemptNumber int
	Context       map[string]interface{}
	UserAction    UserActionType // What the user did (skip, cancel, retry, etc.)
}

// ErrorCategory represents different categories of errors for better organization
type ErrorCategory int

const (
	CategoryFileSystem ErrorCategory = iota
	CategoryValidation
	CategoryPassword
	CategoryUserAction
	CategorySystem
	CategoryNetwork
	CategoryUnknown
)

// String returns a string representation of the error category
func (c ErrorCategory) String() string {
	switch c {
	case CategoryFileSystem:
		return "FILE_SYSTEM"
	case CategoryValidation:
		return "VALIDATION"
	case CategoryPassword:
		return "PASSWORD"
	case CategoryUserAction:
		return "USER_ACTION"
	case CategorySystem:
		return "SYSTEM"
	case CategoryNetwork:
		return "NETWORK"
	default:
		return "UNKNOWN"
	}
}

// GetLocalizationKey returns the localization key for the error category
func (c ErrorCategory) GetLocalizationKey() string {
	switch c {
	case CategoryFileSystem:
		return "error_category_filesystem"
	case CategoryValidation:
		return "error_category_validation"
	case CategoryPassword:
		return "error_category_password"
	case CategoryUserAction:
		return "error_category_user_action"
	case CategorySystem:
		return "error_category_system"
	case CategoryNetwork:
		return "error_category_network"
	default:
		return "error_category_unknown"
	}
}

// UserActionType represents different user actions in response to errors
type UserActionType int

const (
	UserActionNone UserActionType = iota
	UserActionSkip
	UserActionCancel
	UserActionRetry
	UserActionIgnore
	UserActionAbort
)

// String returns a string representation of the user action
func (a UserActionType) String() string {
	switch a {
	case UserActionNone:
		return "NONE"
	case UserActionSkip:
		return "SKIP"
	case UserActionCancel:
		return "CANCEL"
	case UserActionRetry:
		return "RETRY"
	case UserActionIgnore:
		return "IGNORE"
	case UserActionAbort:
		return "ABORT"
	default:
		return "UNKNOWN"
	}
}

// NewErrorAggregator creates a new error aggregator for batch operations
func NewErrorAggregator(totalOperations int) *ErrorAggregator {
	return &ErrorAggregator{
		errors:          make([]AggregatedError, 0),
		startTime:       time.Now(),
		totalOperations: totalOperations,
		successCount:    0,
		failureCount:    0,
		skipCount:       0,
	}
}

// AddError adds an error to the aggregator with context
func (ea *ErrorAggregator) AddError(err error, file string, userAction UserActionType) {
	if err == nil {
		return
	}

	aggregatedErr := AggregatedError{
		OriginalError: err,
		File:          file,
		Timestamp:     time.Now(),
		UserAction:    userAction,
		Context:       make(map[string]interface{}),
	}

	// Determine error type and category
	if keystoreErr, ok := err.(*KeystoreImportError); ok {
		aggregatedErr.ErrorType = keystoreErr.Type
		aggregatedErr.Category = categorizeKeystoreError(keystoreErr.Type)
		aggregatedErr.Recoverable = keystoreErr.IsRecoverable()
		aggregatedErr.RecoveryHint = keystoreErr.GetRecoveryHint()

		// Copy context from original error
		for k, v := range keystoreErr.GetContext() {
			aggregatedErr.Context[k] = v
		}
	} else if passwordErr, ok := err.(*PasswordFileError); ok {
		aggregatedErr.ErrorType = mapPasswordFileErrorType(passwordErr.Type)
		aggregatedErr.Category = CategoryPassword
		aggregatedErr.Recoverable = isPasswordErrorRecoverable(passwordErr.Type)
		aggregatedErr.RecoveryHint = passwordErr.Type.GetLocalizationKey() + "_recovery"
	} else if passwordInputErr, ok := err.(*PasswordInputError); ok {
		aggregatedErr.ErrorType = mapPasswordInputErrorType(passwordInputErr.Type)
		aggregatedErr.Category = CategoryUserAction
		aggregatedErr.Recoverable = isPasswordInputErrorRecoverable(passwordInputErr.Type)
		aggregatedErr.RecoveryHint = "password_input_recovery_" + strings.ToLower(passwordInputErr.Type.String())
	} else {
		// Generic error handling
		aggregatedErr.ErrorType = ErrorBatchImportFailed
		aggregatedErr.Category = CategorySystem
		aggregatedErr.Recoverable = false
		aggregatedErr.RecoveryHint = "generic_error_recovery"
	}

	ea.errors = append(ea.errors, aggregatedErr)

	// Update counters based on user action
	switch userAction {
	case UserActionSkip:
		ea.skipCount++
	case UserActionCancel, UserActionAbort:
		ea.failureCount++
	default:
		ea.failureCount++
	}
}

// AddSuccess records a successful operation
func (ea *ErrorAggregator) AddSuccess() {
	ea.successCount++
}

// GetErrorsByCategory returns errors grouped by category
func (ea *ErrorAggregator) GetErrorsByCategory() map[ErrorCategory][]AggregatedError {
	categoryMap := make(map[ErrorCategory][]AggregatedError)

	for _, err := range ea.errors {
		categoryMap[err.Category] = append(categoryMap[err.Category], err)
	}

	return categoryMap
}

// GetRecoverableErrors returns only errors that can be recovered from
func (ea *ErrorAggregator) GetRecoverableErrors() []AggregatedError {
	var recoverable []AggregatedError

	for _, err := range ea.errors {
		if err.Recoverable {
			recoverable = append(recoverable, err)
		}
	}

	return recoverable
}

// GetErrorSummary returns a summary of all errors
func (ea *ErrorAggregator) GetErrorSummary() ErrorSummary {
	categoryCount := make(map[ErrorCategory]int)
	recoverableCount := 0

	for _, err := range ea.errors {
		categoryCount[err.Category]++
		if err.Recoverable {
			recoverableCount++
		}
	}

	return ErrorSummary{
		TotalOperations:   ea.totalOperations,
		SuccessCount:      ea.successCount,
		FailureCount:      ea.failureCount,
		SkipCount:         ea.skipCount,
		TotalErrors:       len(ea.errors),
		RecoverableErrors: recoverableCount,
		CategoryBreakdown: categoryCount,
		ElapsedTime:       time.Since(ea.startTime),
		StartTime:         ea.startTime,
	}
}

// ErrorSummary provides a comprehensive summary of batch operation results
type ErrorSummary struct {
	TotalOperations   int
	SuccessCount      int
	FailureCount      int
	SkipCount         int
	TotalErrors       int
	RecoverableErrors int
	CategoryBreakdown map[ErrorCategory]int
	ElapsedTime       time.Duration
	StartTime         time.Time
}

// GetSuccessRate returns the success rate as a percentage
func (es *ErrorSummary) GetSuccessRate() float64 {
	if es.TotalOperations == 0 {
		return 0.0
	}
	return float64(es.SuccessCount) / float64(es.TotalOperations) * 100.0
}

// GetFailureRate returns the failure rate as a percentage
func (es *ErrorSummary) GetFailureRate() float64 {
	if es.TotalOperations == 0 {
		return 0.0
	}
	return float64(es.FailureCount) / float64(es.TotalOperations) * 100.0
}

// GetSkipRate returns the skip rate as a percentage
func (es *ErrorSummary) GetSkipRate() float64 {
	if es.TotalOperations == 0 {
		return 0.0
	}
	return float64(es.SkipCount) / float64(es.TotalOperations) * 100.0
}

// GetMostCommonErrorCategory returns the most common error category
func (es *ErrorSummary) GetMostCommonErrorCategory() ErrorCategory {
	maxCount := 0
	var mostCommon ErrorCategory = CategoryUnknown

	for category, count := range es.CategoryBreakdown {
		if count > maxCount {
			maxCount = count
			mostCommon = category
		}
	}

	return mostCommon
}

// GetRetryRecommendations returns recommendations for retrying failed operations
func (ea *ErrorAggregator) GetRetryRecommendations() []RetryRecommendation {
	var recommendations []RetryRecommendation

	recoverableErrors := ea.GetRecoverableErrors()

	// Group recoverable errors by type for better recommendations
	errorTypeGroups := make(map[KeystoreErrorType][]AggregatedError)
	for _, err := range recoverableErrors {
		errorTypeGroups[err.ErrorType] = append(errorTypeGroups[err.ErrorType], err)
	}

	// Generate recommendations for each error type group
	for errorType, errors := range errorTypeGroups {
		recommendation := RetryRecommendation{
			ErrorType:     errorType,
			AffectedFiles: make([]string, len(errors)),
			Priority:      getRetryPriority(errorType),
			Strategy:      getRetryStrategy(errorType),
			Description:   getRetryDescription(errorType),
		}

		for i, err := range errors {
			recommendation.AffectedFiles[i] = err.File
		}

		recommendations = append(recommendations, recommendation)
	}

	// Sort recommendations by priority (highest first)
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Priority > recommendations[j].Priority
	})

	return recommendations
}

// RetryRecommendation provides guidance for retrying failed operations
type RetryRecommendation struct {
	ErrorType     KeystoreErrorType
	AffectedFiles []string
	Priority      int    // Higher number = higher priority
	Strategy      string // Recommended retry strategy
	Description   string // Human-readable description
}

// categorizeKeystoreError categorizes a keystore error type
func categorizeKeystoreError(errorType KeystoreErrorType) ErrorCategory {
	switch {
	case errorType.IsFileSystemRelated():
		return CategoryFileSystem
	case errorType.IsValidationRelated():
		return CategoryValidation
	case errorType.IsPasswordRelated():
		return CategoryPassword
	case errorType.IsUserActionRelated():
		return CategoryUserAction
	default:
		return CategorySystem
	}
}

// mapPasswordFileErrorType maps password file error types to keystore error types
func mapPasswordFileErrorType(pfErrorType PasswordFileErrorType) KeystoreErrorType {
	switch pfErrorType {
	case PasswordFileNotFound:
		return ErrorPasswordFileNotFound
	case PasswordFileUnreadable:
		return ErrorPasswordFileUnreadable
	case PasswordFileEmpty:
		return ErrorPasswordFileEmpty
	case PasswordFileInvalid:
		return ErrorPasswordFileInvalid
	case PasswordFileOversized:
		return ErrorPasswordFileOversized
	case PasswordFileCorrupted:
		return ErrorPasswordFileCorrupted
	default:
		return ErrorPasswordFileInvalid
	}
}

// mapPasswordInputErrorType maps password input error types to keystore error types
func mapPasswordInputErrorType(piErrorType PasswordInputErrorType) KeystoreErrorType {
	switch piErrorType {
	case PasswordInputCancelled:
		return ErrorPasswordInputCancelled
	case PasswordInputSkipped:
		return ErrorPasswordInputSkipped
	case PasswordInputTimeout:
		return ErrorPasswordInputTimeout
	case PasswordInputMaxAttemptsExceeded:
		return ErrorMaxAttemptsExceeded
	default:
		return ErrorPasswordInputTimeout
	}
}

// isPasswordErrorRecoverable determines if a password file error is recoverable
func isPasswordErrorRecoverable(errorType PasswordFileErrorType) bool {
	switch errorType {
	case PasswordFileNotFound, PasswordFileUnreadable, PasswordFileEmpty:
		return true // Can be recovered by manual password input
	case PasswordFileInvalid, PasswordFileOversized, PasswordFileCorrupted:
		return false // File is fundamentally broken
	default:
		return false
	}
}

// isPasswordInputErrorRecoverable determines if a password input error is recoverable
func isPasswordInputErrorRecoverable(errorType PasswordInputErrorType) bool {
	switch errorType {
	case PasswordInputTimeout, PasswordInputInvalid:
		return true // Can retry password input
	case PasswordInputCancelled, PasswordInputSkipped:
		return true // User can change their mind
	case PasswordInputMaxAttemptsExceeded:
		return false // Need to reset or use different approach
	default:
		return false
	}
}

// getRetryPriority returns the priority for retrying a specific error type
func getRetryPriority(errorType KeystoreErrorType) int {
	switch errorType {
	case ErrorPasswordFileNotFound, ErrorPasswordInputTimeout:
		return 10 // High priority - likely user input issues
	case ErrorFileNotFound, ErrorDirectoryScanFailed:
		return 8 // High priority - file selection issues
	case ErrorIncorrectPassword, ErrorPasswordInputCancelled:
		return 7 // Medium-high priority - password issues
	case ErrorPasswordFileUnreadable, ErrorPasswordFileEmpty:
		return 6 // Medium priority - file access issues
	case ErrorImportJobValidation:
		return 5 // Medium priority - validation issues
	default:
		return 1 // Low priority - other errors
	}
}

// getRetryStrategy returns the recommended retry strategy for an error type
func getRetryStrategy(errorType KeystoreErrorType) string {
	switch errorType {
	case ErrorPasswordFileNotFound, ErrorPasswordInputTimeout:
		return "manual_password_input"
	case ErrorFileNotFound:
		return "reselect_files"
	case ErrorDirectoryScanFailed:
		return "reselect_directory"
	case ErrorIncorrectPassword:
		return "correct_password"
	case ErrorPasswordFileUnreadable:
		return "fix_file_permissions"
	case ErrorPasswordFileEmpty:
		return "provide_password_file_content"
	default:
		return "manual_review"
	}
}

// getRetryDescription returns a human-readable description for retry strategy
func getRetryDescription(errorType KeystoreErrorType) string {
	// Return localization keys for UI layer to handle
	switch errorType {
	case ErrorPasswordFileNotFound:
		return "retry_description_password_file_not_found"
	case ErrorPasswordInputTimeout:
		return "retry_description_password_input_timeout"
	case ErrorFileNotFound:
		return "retry_description_file_not_found"
	case ErrorDirectoryScanFailed:
		return "retry_description_directory_scan_failed"
	case ErrorIncorrectPassword:
		return "retry_description_incorrect_password"
	case ErrorPasswordFileUnreadable:
		return "retry_description_password_file_unreadable"
	case ErrorPasswordFileEmpty:
		return "retry_description_password_file_empty"
	default:
		return "retry_description_generic"
	}
}

// GenerateErrorReport generates a comprehensive error report for logging or display
func (ea *ErrorAggregator) GenerateErrorReport() ErrorReport {
	summary := ea.GetErrorSummary()
	errorsByCategory := ea.GetErrorsByCategory()
	retryRecommendations := ea.GetRetryRecommendations()

	return ErrorReport{
		Summary:              summary,
		ErrorsByCategory:     errorsByCategory,
		RetryRecommendations: retryRecommendations,
		GeneratedAt:          time.Now(),
	}
}

// ErrorReport provides a comprehensive report of batch operation errors
type ErrorReport struct {
	Summary              ErrorSummary
	ErrorsByCategory     map[ErrorCategory][]AggregatedError
	RetryRecommendations []RetryRecommendation
	GeneratedAt          time.Time
}

// GetFormattedSummary returns a formatted summary string for display
func (er *ErrorReport) GetFormattedSummary() string {
	var parts []string

	parts = append(parts, fmt.Sprintf("Total Operations: %d", er.Summary.TotalOperations))
	parts = append(parts, fmt.Sprintf("Successful: %d (%.1f%%)", er.Summary.SuccessCount, er.Summary.GetSuccessRate()))
	parts = append(parts, fmt.Sprintf("Failed: %d (%.1f%%)", er.Summary.FailureCount, er.Summary.GetFailureRate()))
	parts = append(parts, fmt.Sprintf("Skipped: %d (%.1f%%)", er.Summary.SkipCount, er.Summary.GetSkipRate()))
	parts = append(parts, fmt.Sprintf("Elapsed Time: %v", er.Summary.ElapsedTime.Round(time.Second)))

	if er.Summary.RecoverableErrors > 0 {
		parts = append(parts, fmt.Sprintf("Recoverable Errors: %d", er.Summary.RecoverableErrors))
	}

	return strings.Join(parts, "\n")
}

// HasRecoverableErrors returns true if there are errors that can be retried
func (er *ErrorReport) HasRecoverableErrors() bool {
	return er.Summary.RecoverableErrors > 0
}

// GetTopRetryRecommendation returns the highest priority retry recommendation
func (er *ErrorReport) GetTopRetryRecommendation() *RetryRecommendation {
	if len(er.RetryRecommendations) == 0 {
		return nil
	}
	return &er.RetryRecommendations[0]
}
