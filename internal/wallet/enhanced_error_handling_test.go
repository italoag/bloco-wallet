package wallet

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeystoreImportError_EnhancedFields(t *testing.T) {
	err := NewKeystoreImportErrorWithRecovery(
		ErrorIncorrectPassword,
		"wrong password",
		"test.json",
		true,
		"try_correct_password",
		errors.New("underlying error"),
	)

	assert.Equal(t, ErrorIncorrectPassword, err.Type)
	assert.Equal(t, "wrong password", err.Message)
	assert.Equal(t, "test.json", err.File)
	assert.True(t, err.Recoverable)
	assert.Equal(t, "try_correct_password", err.RecoveryHint)
	assert.NotNil(t, err.Cause)
	assert.NotNil(t, err.Context)
}

func TestKeystoreImportError_IsRecoverable(t *testing.T) {
	recoverableErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	assert.True(t, recoverableErr.IsRecoverable())

	nonRecoverableErr := NewKeystoreImportError(ErrorInvalidJSON, "invalid json", nil)
	assert.False(t, nonRecoverableErr.IsRecoverable())
}

func TestKeystoreImportError_GetRecoveryHint(t *testing.T) {
	// Test with explicit recovery hint
	errWithHint := NewKeystoreImportErrorWithRecovery(
		ErrorIncorrectPassword,
		"wrong password",
		"test.json",
		true,
		"custom_recovery_hint",
		nil,
	)
	assert.Equal(t, "custom_recovery_hint", errWithHint.GetRecoveryHint())

	// Test with default recovery hint
	errWithoutHint := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	assert.Equal(t, "keystore_recovery_incorrect_password", errWithoutHint.GetRecoveryHint())
}

func TestKeystoreImportError_GetUserFriendlyMessage(t *testing.T) {
	err := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	userFriendlyMsg := err.GetUserFriendlyMessage()

	// Should return localization key, not the actual message
	assert.Equal(t, "keystore_incorrect_password", userFriendlyMsg)
}

func TestKeystoreImportError_Context(t *testing.T) {
	err := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)

	// Test setting and getting context
	err.SetContext("attempt_count", 3)
	err.SetContext("file_size", 1024)

	context := err.GetContext()
	assert.Equal(t, 3, context["attempt_count"])
	assert.Equal(t, 1024, context["file_size"])
}

func TestKeystoreErrorType_CategoryMethods(t *testing.T) {
	testCases := []struct {
		errorType           KeystoreErrorType
		isPasswordRelated   bool
		isFileSystemRelated bool
		isValidationRelated bool
		isUserActionRelated bool
	}{
		{ErrorIncorrectPassword, true, false, false, false},
		{ErrorPasswordFileNotFound, true, true, false, false},
		{ErrorFileNotFound, false, true, false, false},
		{ErrorInvalidJSON, false, false, true, false},
		{ErrorPasswordInputCancelled, true, false, false, true},
		{ErrorBatchImportFailed, false, false, false, false},
	}

	for _, tc := range testCases {
		assert.Equal(t, tc.isPasswordRelated, tc.errorType.IsPasswordRelated(),
			"ErrorType %v IsPasswordRelated should be %v", tc.errorType, tc.isPasswordRelated)
		assert.Equal(t, tc.isFileSystemRelated, tc.errorType.IsFileSystemRelated(),
			"ErrorType %v IsFileSystemRelated should be %v", tc.errorType, tc.isFileSystemRelated)
		assert.Equal(t, tc.isValidationRelated, tc.errorType.IsValidationRelated(),
			"ErrorType %v IsValidationRelated should be %v", tc.errorType, tc.isValidationRelated)
		assert.Equal(t, tc.isUserActionRelated, tc.errorType.IsUserActionRelated(),
			"ErrorType %v IsUserActionRelated should be %v", tc.errorType, tc.isUserActionRelated)
	}
}

func TestKeystoreErrorType_GetDefaultRecoveryHint(t *testing.T) {
	testCases := []struct {
		errorType    KeystoreErrorType
		expectedHint string
	}{
		{ErrorFileNotFound, "keystore_recovery_file_not_found"},
		{ErrorInvalidJSON, "keystore_recovery_invalid_json"},
		{ErrorIncorrectPassword, "keystore_recovery_incorrect_password"},
		{ErrorPasswordFileNotFound, "password_file_recovery_not_found"},
		{ErrorBatchImportFailed, "batch_import_recovery_failed"},
		{ErrorDirectoryScanFailed, "directory_scan_recovery_failed"},
		{ErrorPasswordInputTimeout, "password_input_recovery_timeout"},
		{ErrorMaxAttemptsExceeded, "password_attempts_recovery_exceeded"},
	}

	for _, tc := range testCases {
		hint := tc.errorType.GetDefaultRecoveryHint()
		assert.Equal(t, tc.expectedHint, hint,
			"ErrorType %v should have recovery hint %s", tc.errorType, tc.expectedHint)
	}
}

func TestKeystoreErrorType_GetLocalizationKey(t *testing.T) {
	testCases := []struct {
		errorType   KeystoreErrorType
		expectedKey string
	}{
		{ErrorFileNotFound, "keystore_file_not_found"},
		{ErrorInvalidJSON, "keystore_invalid_json"},
		{ErrorIncorrectPassword, "keystore_incorrect_password"},
		{ErrorPasswordFileNotFound, "password_file_not_found"},
		{ErrorPasswordFileUnreadable, "password_file_unreadable"},
		{ErrorBatchImportFailed, "batch_import_failed"},
		{ErrorImportJobValidation, "import_job_validation_failed"},
		{ErrorDirectoryScanFailed, "directory_scan_failed"},
		{ErrorPasswordInputTimeout, "password_input_timeout"},
		{ErrorPasswordInputCancelled, "password_input_cancelled"},
		{ErrorPasswordInputSkipped, "password_input_skipped"},
		{ErrorMaxAttemptsExceeded, "max_password_attempts_exceeded"},
		{ErrorPartialImportFailure, "partial_import_failure"},
		{ErrorImportInterrupted, "import_interrupted"},
		{ErrorCleanupFailed, "cleanup_failed"},
	}

	for _, tc := range testCases {
		key := tc.errorType.GetLocalizationKey()
		assert.Equal(t, tc.expectedKey, key,
			"ErrorType %v should have localization key %s", tc.errorType, tc.expectedKey)
	}
}

func TestKeystoreErrorType_String(t *testing.T) {
	testCases := []struct {
		errorType      KeystoreErrorType
		expectedString string
	}{
		{ErrorFileNotFound, "FILE_NOT_FOUND"},
		{ErrorInvalidJSON, "INVALID_JSON"},
		{ErrorPasswordFileNotFound, "PASSWORD_FILE_NOT_FOUND"},
		{ErrorBatchImportFailed, "BATCH_IMPORT_FAILED"},
		{ErrorPasswordInputTimeout, "PASSWORD_INPUT_TIMEOUT"},
		{ErrorMaxAttemptsExceeded, "MAX_PASSWORD_ATTEMPTS_EXCEEDED"},
		{ErrorPartialImportFailure, "PARTIAL_IMPORT_FAILURE"},
		{ErrorImportInterrupted, "IMPORT_INTERRUPTED"},
		{ErrorCleanupFailed, "CLEANUP_FAILED"},
	}

	for _, tc := range testCases {
		str := tc.errorType.String()
		assert.Equal(t, tc.expectedString, str,
			"ErrorType %v should have string representation %s", tc.errorType, tc.expectedString)
	}
}

func TestIsRecoverableErrorType(t *testing.T) {
	testCases := []struct {
		errorType   KeystoreErrorType
		recoverable bool
	}{
		{ErrorIncorrectPassword, true},
		{ErrorPasswordFileNotFound, true},
		{ErrorPasswordFileUnreadable, true},
		{ErrorPasswordFileEmpty, true},
		{ErrorPasswordInputTimeout, true},
		{ErrorPasswordInputCancelled, true},
		{ErrorPasswordInputSkipped, true},
		{ErrorFileNotFound, true},
		{ErrorDirectoryScanFailed, true},
		{ErrorImportJobValidation, true},
		{ErrorBatchImportFailed, true},
		{ErrorInvalidJSON, false},
		{ErrorInvalidKeystore, false},
		{ErrorInvalidVersion, false},
		{ErrorCorruptedFile, false},
		{ErrorAddressMismatch, false},
		{ErrorMissingRequiredFields, false},
		{ErrorInvalidAddress, false},
		{ErrorPasswordFileCorrupted, false},
		{ErrorPasswordFileInvalid, false},
	}

	for _, tc := range testCases {
		recoverable := isRecoverableErrorType(tc.errorType)
		assert.Equal(t, tc.recoverable, recoverable,
			"ErrorType %v recoverability should be %v", tc.errorType, tc.recoverable)
	}
}

func TestPasswordFileError_EnhancedFields(t *testing.T) {
	err := NewPasswordFileErrorWithRecovery(
		PasswordFileNotFound,
		"test.pwd",
		"file not found",
		true,
		"create_password_file",
		errors.New("underlying error"),
	)

	assert.Equal(t, PasswordFileNotFound, err.Type)
	assert.Equal(t, "test.pwd", err.File)
	assert.Equal(t, "file not found", err.Message)
	assert.True(t, err.Recoverable)
	assert.Equal(t, "create_password_file", err.RecoveryHint)
	assert.NotNil(t, err.Cause)
	assert.NotNil(t, err.Context)
}

func TestPasswordFileError_IsRecoverable(t *testing.T) {
	recoverableErr := NewPasswordFileError(PasswordFileNotFound, "test.pwd", "not found", nil)
	assert.True(t, recoverableErr.IsRecoverable())

	nonRecoverableErr := NewPasswordFileError(PasswordFileCorrupted, "test.pwd", "corrupted", nil)
	assert.False(t, nonRecoverableErr.IsRecoverable())
}

func TestPasswordFileError_GetRecoveryHint(t *testing.T) {
	// Test with explicit recovery hint
	errWithHint := NewPasswordFileErrorWithRecovery(
		PasswordFileNotFound,
		"test.pwd",
		"not found",
		true,
		"custom_recovery_hint",
		nil,
	)
	assert.Equal(t, "custom_recovery_hint", errWithHint.GetRecoveryHint())

	// Test with default recovery hint
	errWithoutHint := NewPasswordFileError(PasswordFileNotFound, "test.pwd", "not found", nil)
	assert.Equal(t, "password_file_not_found_recovery", errWithoutHint.GetRecoveryHint())
}

func TestPasswordFileError_GetUserFriendlyMessage(t *testing.T) {
	err := NewPasswordFileError(PasswordFileNotFound, "test.pwd", "not found", nil)
	userFriendlyMsg := err.GetUserFriendlyMessage()

	// Should return localization key, not the actual message
	assert.Equal(t, "password_file_not_found", userFriendlyMsg)
}

func TestPasswordFileError_Context(t *testing.T) {
	err := NewPasswordFileError(PasswordFileNotFound, "test.pwd", "not found", nil)

	// Test setting and getting context
	err.SetContext("file_size", 0)
	err.SetContext("permissions", "644")

	context := err.GetContext()
	assert.Equal(t, 0, context["file_size"])
	assert.Equal(t, "644", context["permissions"])
}

func TestIsPasswordFileErrorRecoverable(t *testing.T) {
	testCases := []struct {
		errorType   PasswordFileErrorType
		recoverable bool
	}{
		{PasswordFileNotFound, true},
		{PasswordFileUnreadable, true},
		{PasswordFileEmpty, true},
		{PasswordFileInvalid, false},
		{PasswordFileOversized, false},
		{PasswordFileCorrupted, false},
	}

	for _, tc := range testCases {
		recoverable := isPasswordFileErrorRecoverable(tc.errorType)
		assert.Equal(t, tc.recoverable, recoverable,
			"PasswordFileErrorType %v recoverability should be %v", tc.errorType, tc.recoverable)
	}
}

func TestPasswordFileErrorType_GetLocalizationKey(t *testing.T) {
	testCases := []struct {
		errorType   PasswordFileErrorType
		expectedKey string
	}{
		{PasswordFileNotFound, "password_file_not_found"},
		{PasswordFileUnreadable, "password_file_unreadable"},
		{PasswordFileEmpty, "password_file_empty"},
		{PasswordFileInvalid, "password_file_invalid"},
		{PasswordFileOversized, "password_file_oversized"},
		{PasswordFileCorrupted, "password_file_corrupted"},
	}

	for _, tc := range testCases {
		key := tc.errorType.GetLocalizationKey()
		assert.Equal(t, tc.expectedKey, key,
			"PasswordFileErrorType %v should have localization key %s", tc.errorType, tc.expectedKey)
	}
}

func TestPasswordFileErrorType_String(t *testing.T) {
	testCases := []struct {
		errorType      PasswordFileErrorType
		expectedString string
	}{
		{PasswordFileNotFound, "PASSWORD_FILE_NOT_FOUND"},
		{PasswordFileUnreadable, "PASSWORD_FILE_UNREADABLE"},
		{PasswordFileEmpty, "PASSWORD_FILE_EMPTY"},
		{PasswordFileInvalid, "PASSWORD_FILE_INVALID"},
		{PasswordFileOversized, "PASSWORD_FILE_OVERSIZED"},
		{PasswordFileCorrupted, "PASSWORD_FILE_CORRUPTED"},
	}

	for _, tc := range testCases {
		str := tc.errorType.String()
		assert.Equal(t, tc.expectedString, str,
			"PasswordFileErrorType %v should have string representation %s", tc.errorType, tc.expectedString)
	}
}

func TestPasswordInputError_Methods(t *testing.T) {
	// Test IsSkipped
	skippedErr := &PasswordInputError{Type: PasswordInputSkipped}
	assert.True(t, skippedErr.IsSkipped())
	assert.False(t, skippedErr.IsCancelled())

	// Test IsCancelled
	cancelledErr := &PasswordInputError{Type: PasswordInputCancelled}
	assert.True(t, cancelledErr.IsCancelled())
	assert.False(t, cancelledErr.IsSkipped())

	// Test neither
	timeoutErr := &PasswordInputError{Type: PasswordInputTimeout}
	assert.False(t, timeoutErr.IsSkipped())
	assert.False(t, timeoutErr.IsCancelled())
}

func TestNewKeystoreImportErrorConstructors(t *testing.T) {
	// Test basic constructor
	basicErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	assert.Equal(t, ErrorIncorrectPassword, basicErr.Type)
	assert.Equal(t, "wrong password", basicErr.Message)
	assert.True(t, basicErr.Recoverable) // Should be set based on error type
	assert.NotNil(t, basicErr.Context)

	// Test constructor with field
	fieldErr := NewKeystoreImportErrorWithField(ErrorMissingRequiredFields, "missing field", "address", nil)
	assert.Equal(t, ErrorMissingRequiredFields, fieldErr.Type)
	assert.Equal(t, "missing field", fieldErr.Message)
	assert.Equal(t, "address", fieldErr.Field)
	assert.False(t, fieldErr.Recoverable) // Should be set based on error type

	// Test constructor with file
	fileErr := NewKeystoreImportErrorWithFile(ErrorFileNotFound, "file not found", "test.json", nil)
	assert.Equal(t, ErrorFileNotFound, fileErr.Type)
	assert.Equal(t, "file not found", fileErr.Message)
	assert.Equal(t, "test.json", fileErr.File)
	assert.True(t, fileErr.Recoverable) // Should be set based on error type

	// Test constructor with recovery
	recoveryErr := NewKeystoreImportErrorWithRecovery(
		ErrorIncorrectPassword,
		"wrong password",
		"test.json",
		false, // Override default recoverability
		"custom_hint",
		nil,
	)
	assert.Equal(t, ErrorIncorrectPassword, recoveryErr.Type)
	assert.Equal(t, "wrong password", recoveryErr.Message)
	assert.Equal(t, "test.json", recoveryErr.File)
	assert.False(t, recoveryErr.Recoverable) // Should use provided value
	assert.Equal(t, "custom_hint", recoveryErr.RecoveryHint)
}

func TestNewPasswordFileErrorConstructors(t *testing.T) {
	// Test basic constructor
	basicErr := NewPasswordFileError(PasswordFileNotFound, "test.pwd", "not found", nil)
	assert.Equal(t, PasswordFileNotFound, basicErr.Type)
	assert.Equal(t, "test.pwd", basicErr.File)
	assert.Equal(t, "not found", basicErr.Message)
	assert.True(t, basicErr.Recoverable) // Should be set based on error type
	assert.NotNil(t, basicErr.Context)

	// Test constructor with recovery
	recoveryErr := NewPasswordFileErrorWithRecovery(
		PasswordFileCorrupted,
		"test.pwd",
		"corrupted",
		true, // Override default recoverability
		"custom_hint",
		nil,
	)
	assert.Equal(t, PasswordFileCorrupted, recoveryErr.Type)
	assert.Equal(t, "test.pwd", recoveryErr.File)
	assert.Equal(t, "corrupted", recoveryErr.Message)
	assert.True(t, recoveryErr.Recoverable) // Should use provided value
	assert.Equal(t, "custom_hint", recoveryErr.RecoveryHint)
}
