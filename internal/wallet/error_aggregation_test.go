package wallet

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewErrorAggregator(t *testing.T) {
	aggregator := NewErrorAggregator(10)

	assert.NotNil(t, aggregator)
	assert.Equal(t, 10, aggregator.totalOperations)
	assert.Equal(t, 0, aggregator.successCount)
	assert.Equal(t, 0, aggregator.failureCount)
	assert.Equal(t, 0, aggregator.skipCount)
	assert.Empty(t, aggregator.errors)
}

func TestErrorAggregator_AddError(t *testing.T) {
	aggregator := NewErrorAggregator(5)

	// Test adding keystore import error
	keystoreErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	aggregator.AddError(keystoreErr, "test.json", UserActionRetry)

	assert.Equal(t, 1, len(aggregator.errors))
	assert.Equal(t, 1, aggregator.failureCount)

	aggregatedErr := aggregator.errors[0]
	assert.Equal(t, keystoreErr, aggregatedErr.OriginalError)
	assert.Equal(t, "test.json", aggregatedErr.File)
	assert.Equal(t, ErrorIncorrectPassword, aggregatedErr.ErrorType)
	assert.Equal(t, CategoryPassword, aggregatedErr.Category)
	assert.Equal(t, UserActionRetry, aggregatedErr.UserAction)
	assert.True(t, aggregatedErr.Recoverable)
}

func TestErrorAggregator_AddPasswordFileError(t *testing.T) {
	aggregator := NewErrorAggregator(5)

	// Test adding password file error
	passwordErr := NewPasswordFileError(PasswordFileNotFound, "test.pwd", "not found", nil)
	aggregator.AddError(passwordErr, "test.pwd", UserActionSkip)

	assert.Equal(t, 1, len(aggregator.errors))
	assert.Equal(t, 1, aggregator.skipCount)

	aggregatedErr := aggregator.errors[0]
	assert.Equal(t, passwordErr, aggregatedErr.OriginalError)
	assert.Equal(t, ErrorPasswordFileNotFound, aggregatedErr.ErrorType)
	assert.Equal(t, CategoryPassword, aggregatedErr.Category)
	assert.Equal(t, UserActionSkip, aggregatedErr.UserAction)
}

func TestErrorAggregator_AddPasswordInputError(t *testing.T) {
	aggregator := NewErrorAggregator(5)

	// Test adding password input error
	inputErr := &PasswordInputError{
		Type:    PasswordInputCancelled,
		Message: "user cancelled",
		File:    "test.json",
	}
	aggregator.AddError(inputErr, "test.json", UserActionCancel)

	assert.Equal(t, 1, len(aggregator.errors))
	assert.Equal(t, 1, aggregator.failureCount)

	aggregatedErr := aggregator.errors[0]
	assert.Equal(t, inputErr, aggregatedErr.OriginalError)
	assert.Equal(t, ErrorPasswordInputCancelled, aggregatedErr.ErrorType)
	assert.Equal(t, CategoryUserAction, aggregatedErr.Category)
	assert.Equal(t, UserActionCancel, aggregatedErr.UserAction)
}

func TestErrorAggregator_AddGenericError(t *testing.T) {
	aggregator := NewErrorAggregator(5)

	// Test adding generic error
	genericErr := errors.New("generic error")
	aggregator.AddError(genericErr, "test.json", UserActionNone)

	assert.Equal(t, 1, len(aggregator.errors))
	assert.Equal(t, 1, aggregator.failureCount)

	aggregatedErr := aggregator.errors[0]
	assert.Equal(t, genericErr, aggregatedErr.OriginalError)
	assert.Equal(t, ErrorBatchImportFailed, aggregatedErr.ErrorType)
	assert.Equal(t, CategorySystem, aggregatedErr.Category)
	assert.Equal(t, UserActionNone, aggregatedErr.UserAction)
	assert.False(t, aggregatedErr.Recoverable)
}

func TestErrorAggregator_AddSuccess(t *testing.T) {
	aggregator := NewErrorAggregator(5)

	aggregator.AddSuccess()
	aggregator.AddSuccess()

	assert.Equal(t, 2, aggregator.successCount)
	assert.Equal(t, 0, aggregator.failureCount)
	assert.Equal(t, 0, aggregator.skipCount)
}

func TestErrorAggregator_GetErrorsByCategory(t *testing.T) {
	aggregator := NewErrorAggregator(5)

	// Add errors of different categories
	keystoreErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	aggregator.AddError(keystoreErr, "test1.json", UserActionRetry)

	passwordErr := NewPasswordFileError(PasswordFileNotFound, "test2.pwd", "not found", nil)
	aggregator.AddError(passwordErr, "test2.pwd", UserActionSkip)

	validationErr := NewKeystoreImportError(ErrorInvalidJSON, "invalid json", nil)
	aggregator.AddError(validationErr, "test3.json", UserActionNone)

	categoryMap := aggregator.GetErrorsByCategory()

	assert.Equal(t, 2, len(categoryMap[CategoryPassword]))
	assert.Equal(t, 1, len(categoryMap[CategoryValidation]))
	assert.Equal(t, 0, len(categoryMap[CategoryFileSystem]))
}

func TestErrorAggregator_GetRecoverableErrors(t *testing.T) {
	aggregator := NewErrorAggregator(5)

	// Add recoverable and non-recoverable errors
	recoverableErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	aggregator.AddError(recoverableErr, "test1.json", UserActionRetry)

	nonRecoverableErr := NewKeystoreImportError(ErrorInvalidJSON, "invalid json", nil)
	aggregator.AddError(nonRecoverableErr, "test2.json", UserActionNone)

	recoverableErrors := aggregator.GetRecoverableErrors()

	assert.Equal(t, 1, len(recoverableErrors))
	assert.Equal(t, ErrorIncorrectPassword, recoverableErrors[0].ErrorType)
}

func TestErrorAggregator_GetErrorSummary(t *testing.T) {
	aggregator := NewErrorAggregator(10)

	// Add various operations
	aggregator.AddSuccess()
	aggregator.AddSuccess()
	aggregator.AddSuccess()

	recoverableErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	aggregator.AddError(recoverableErr, "test1.json", UserActionRetry)

	nonRecoverableErr := NewKeystoreImportError(ErrorInvalidJSON, "invalid json", nil)
	aggregator.AddError(nonRecoverableErr, "test2.json", UserActionNone)

	passwordErr := NewPasswordFileError(PasswordFileNotFound, "test3.pwd", "not found", nil)
	aggregator.AddError(passwordErr, "test3.pwd", UserActionSkip)

	summary := aggregator.GetErrorSummary()

	assert.Equal(t, 10, summary.TotalOperations)
	assert.Equal(t, 3, summary.SuccessCount)
	assert.Equal(t, 2, summary.FailureCount)
	assert.Equal(t, 1, summary.SkipCount)
	assert.Equal(t, 3, summary.TotalErrors)
	assert.Equal(t, 2, summary.RecoverableErrors) // password and incorrect password errors

	// Test calculated rates
	assert.Equal(t, 30.0, summary.GetSuccessRate())
	assert.Equal(t, 20.0, summary.GetFailureRate())
	assert.Equal(t, 10.0, summary.GetSkipRate())

	// Test most common error category
	assert.Equal(t, CategoryPassword, summary.GetMostCommonErrorCategory())
}

func TestErrorAggregator_GetRetryRecommendations(t *testing.T) {
	aggregator := NewErrorAggregator(5)

	// Add errors that can be retried
	passwordErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	aggregator.AddError(passwordErr, "test1.json", UserActionRetry)
	aggregator.AddError(passwordErr, "test2.json", UserActionRetry)

	fileErr := NewKeystoreImportError(ErrorFileNotFound, "file not found", nil)
	aggregator.AddError(fileErr, "test3.json", UserActionNone)

	recommendations := aggregator.GetRetryRecommendations()

	assert.Equal(t, 2, len(recommendations))

	// Should be sorted by priority (highest first)
	// ErrorFileNotFound has priority 8, ErrorIncorrectPassword has priority 7
	assert.Equal(t, ErrorFileNotFound, recommendations[0].ErrorType)
	assert.Equal(t, 1, len(recommendations[0].AffectedFiles))
	assert.Equal(t, "reselect_files", recommendations[0].Strategy)

	assert.Equal(t, ErrorIncorrectPassword, recommendations[1].ErrorType)
	assert.Equal(t, 2, len(recommendations[1].AffectedFiles))
	assert.Equal(t, "correct_password", recommendations[1].Strategy)
}

func TestErrorAggregator_GenerateErrorReport(t *testing.T) {
	aggregator := NewErrorAggregator(5)

	// Add some errors and successes
	aggregator.AddSuccess()
	aggregator.AddSuccess()

	passwordErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	aggregator.AddError(passwordErr, "test1.json", UserActionRetry)

	report := aggregator.GenerateErrorReport()

	assert.NotNil(t, report)
	assert.Equal(t, 5, report.Summary.TotalOperations)
	assert.Equal(t, 2, report.Summary.SuccessCount)
	assert.Equal(t, 1, report.Summary.FailureCount)
	assert.Equal(t, 1, report.Summary.RecoverableErrors)

	assert.Equal(t, 1, len(report.ErrorsByCategory))
	assert.Equal(t, 1, len(report.ErrorsByCategory[CategoryPassword]))

	assert.Equal(t, 1, len(report.RetryRecommendations))
	assert.Equal(t, ErrorIncorrectPassword, report.RetryRecommendations[0].ErrorType)

	assert.True(t, report.HasRecoverableErrors())

	topRecommendation := report.GetTopRetryRecommendation()
	assert.NotNil(t, topRecommendation)
	assert.Equal(t, ErrorIncorrectPassword, topRecommendation.ErrorType)
}

func TestErrorReport_GetFormattedSummary(t *testing.T) {
	aggregator := NewErrorAggregator(10)

	// Add operations to create a meaningful summary
	for i := 0; i < 6; i++ {
		aggregator.AddSuccess()
	}

	passwordErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	aggregator.AddError(passwordErr, "test1.json", UserActionRetry)
	aggregator.AddError(passwordErr, "test2.json", UserActionSkip)

	// Use a non-recoverable error type
	fileErr := NewKeystoreImportError(ErrorCorruptedFile, "file corrupted", nil)
	aggregator.AddError(fileErr, "test3.json", UserActionNone)

	// Wait a bit to get meaningful elapsed time
	time.Sleep(10 * time.Millisecond)

	report := aggregator.GenerateErrorReport()
	summary := report.GetFormattedSummary()

	assert.Contains(t, summary, "Total Operations: 10")
	assert.Contains(t, summary, "Successful: 6 (60.0%)")
	assert.Contains(t, summary, "Failed: 2 (20.0%)")
	assert.Contains(t, summary, "Skipped: 1 (10.0%)")
	assert.Contains(t, summary, "Elapsed Time:")
	assert.Contains(t, summary, "Recoverable Errors: 2")
}

func TestErrorCategory_String(t *testing.T) {
	testCases := []struct {
		category ErrorCategory
		expected string
	}{
		{CategoryFileSystem, "FILE_SYSTEM"},
		{CategoryValidation, "VALIDATION"},
		{CategoryPassword, "PASSWORD"},
		{CategoryUserAction, "USER_ACTION"},
		{CategorySystem, "SYSTEM"},
		{CategoryNetwork, "NETWORK"},
		{CategoryUnknown, "UNKNOWN"},
	}

	for _, tc := range testCases {
		assert.Equal(t, tc.expected, tc.category.String())
	}
}

func TestUserActionType_String(t *testing.T) {
	testCases := []struct {
		action   UserActionType
		expected string
	}{
		{UserActionNone, "NONE"},
		{UserActionSkip, "SKIP"},
		{UserActionCancel, "CANCEL"},
		{UserActionRetry, "RETRY"},
		{UserActionIgnore, "IGNORE"},
		{UserActionAbort, "ABORT"},
	}

	for _, tc := range testCases {
		assert.Equal(t, tc.expected, tc.action.String())
	}
}

func TestCategorizeKeystoreError(t *testing.T) {
	testCases := []struct {
		errorType ErrorType
		expected  ErrorCategory
	}{
		{ErrorFileNotFound, CategoryFileSystem},
		{ErrorPasswordFileUnreadable, CategoryFileSystem},
		{ErrorInvalidJSON, CategoryValidation},
		{ErrorInvalidKeystore, CategoryValidation},
		{ErrorIncorrectPassword, CategoryPassword},
		{ErrorPasswordFileNotFound, CategoryPassword},
		{ErrorPasswordInputCancelled, CategoryUserAction},
		{ErrorPasswordInputSkipped, CategoryUserAction},
		{ErrorBatchImportFailed, CategorySystem},
	}

	for _, tc := range testCases {
		result := categorizeKeystoreError(tc.errorType)
		assert.Equal(t, tc.expected, result, "Error type %v should be categorized as %v", tc.errorType, tc.expected)
	}
}

func TestGetRetryPriority(t *testing.T) {
	testCases := []struct {
		errorType   ErrorType
		minPriority int
	}{
		{ErrorPasswordFileNotFound, 8},
		{ErrorPasswordInputTimeout, 8},
		{ErrorFileNotFound, 6},
		{ErrorIncorrectPassword, 5},
		{ErrorInvalidJSON, 1},
	}

	for _, tc := range testCases {
		priority := getRetryPriority(tc.errorType)
		assert.GreaterOrEqual(t, priority, tc.minPriority, "Error type %v should have priority >= %d", tc.errorType, tc.minPriority)
	}
}

func TestGetRetryStrategy(t *testing.T) {
	testCases := []struct {
		errorType ErrorType
		expected  string
	}{
		{ErrorPasswordFileNotFound, "manual_password_input"},
		{ErrorPasswordInputTimeout, "manual_password_input"},
		{ErrorFileNotFound, "reselect_files"},
		{ErrorDirectoryScanFailed, "reselect_directory"},
		{ErrorIncorrectPassword, "correct_password"},
		{ErrorPasswordFileUnreadable, "fix_file_permissions"},
	}

	for _, tc := range testCases {
		strategy := getRetryStrategy(tc.errorType)
		assert.Equal(t, tc.expected, strategy, "Error type %v should have strategy %s", tc.errorType, tc.expected)
	}
}

// Helper type alias for testing
type ErrorType = KeystoreErrorType
