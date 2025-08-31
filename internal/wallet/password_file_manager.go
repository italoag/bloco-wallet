package wallet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// PasswordFileErrorType represents different types of password file errors
type PasswordFileErrorType int

const (
	PasswordFileNotFound PasswordFileErrorType = iota
	PasswordFileUnreadable
	PasswordFileEmpty
	PasswordFileInvalid
	PasswordFileOversized
	PasswordFileCorrupted
)

// PasswordFileError represents a specific error that occurred during password file operations
type PasswordFileError struct {
	Type         PasswordFileErrorType
	File         string
	Message      string
	Cause        error
	Recoverable  bool
	RecoveryHint string
	Context      map[string]interface{}
}

// Error implements the error interface
func (e *PasswordFileError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap returns the underlying error for error unwrapping
func (e *PasswordFileError) Unwrap() error {
	return e.Cause
}

// GetLocalizationKey returns the localization key for this error type
func (e PasswordFileErrorType) GetLocalizationKey() string {
	switch e {
	case PasswordFileNotFound:
		return "password_file_not_found"
	case PasswordFileUnreadable:
		return "password_file_unreadable"
	case PasswordFileEmpty:
		return "password_file_empty"
	case PasswordFileInvalid:
		return "password_file_invalid"
	case PasswordFileOversized:
		return "password_file_oversized"
	case PasswordFileCorrupted:
		return "password_file_corrupted"
	default:
		return "password_file_unknown_error"
	}
}

// PasswordFileManager handles password file operations for keystore imports
type PasswordFileManager struct{}

// NewPasswordFileManager creates a new PasswordFileManager instance
func NewPasswordFileManager() *PasswordFileManager {
	return &PasswordFileManager{}
}

// FindPasswordFile detects if a password file exists for the given keystore path
// It looks for a .pwd file with the same base name as the keystore file
func (pfm *PasswordFileManager) FindPasswordFile(keystorePath string) (string, error) {
	if keystorePath == "" {
		return "", NewPasswordFileErrorWithRecovery(
			PasswordFileInvalid,
			"",
			"Keystore path cannot be empty",
			true,
			"provide_valid_keystore_path",
			nil,
		)
	}

	// Get the directory and base name of the keystore file
	dir := filepath.Dir(keystorePath)
	baseName := filepath.Base(keystorePath)

	// Remove the extension to get the base name
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)

	// Construct the password file path
	passwordFilePath := filepath.Join(dir, nameWithoutExt+".pwd")

	// Check if the password file exists
	if _, err := os.Stat(passwordFilePath); os.IsNotExist(err) {
		return "", NewPasswordFileErrorWithRecovery(
			PasswordFileNotFound,
			passwordFilePath,
			fmt.Sprintf("Password file not found: %s", passwordFilePath),
			true,
			"create_password_file_or_manual_input",
			err,
		)
	} else if err != nil {
		return "", NewPasswordFileErrorWithRecovery(
			PasswordFileUnreadable,
			passwordFilePath,
			fmt.Sprintf("Cannot access password file: %s", passwordFilePath),
			true,
			"fix_file_permissions",
			err,
		)
	}

	return passwordFilePath, nil
}

// ReadPasswordFile reads and validates a password from a .pwd file
func (pfm *PasswordFileManager) ReadPasswordFile(passwordPath string) (string, error) {
	if passwordPath == "" {
		return "", NewPasswordFileErrorWithRecovery(
			PasswordFileInvalid,
			"",
			"Password file path cannot be empty",
			true,
			"provide_valid_password_file_path",
			nil,
		)
	}

	// Validate the file before reading
	if err := pfm.ValidatePasswordFile(passwordPath); err != nil {
		return "", err
	}

	// Read the file content
	content, err := os.ReadFile(passwordPath)
	if err != nil {
		return "", NewPasswordFileErrorWithRecovery(
			PasswordFileUnreadable,
			passwordPath,
			fmt.Sprintf("Failed to read password file: %s", passwordPath),
			true,
			"fix_file_permissions_or_manual_input",
			err,
		)
	}

	// Convert to string and validate UTF-8 encoding
	password := string(content)
	if !utf8.ValidString(password) {
		return "", NewPasswordFileErrorWithRecovery(
			PasswordFileCorrupted,
			passwordPath,
			fmt.Sprintf("Password file contains invalid UTF-8 encoding: %s", passwordPath),
			false,
			"recreate_password_file",
			nil,
		)
	}

	// Trim whitespace (including newlines)
	password = strings.TrimSpace(password)

	// Check if password is empty after trimming
	if password == "" {
		return "", NewPasswordFileErrorWithRecovery(
			PasswordFileEmpty,
			passwordPath,
			fmt.Sprintf("Password file is empty: %s", passwordPath),
			true,
			"add_password_to_file_or_manual_input",
			nil,
		)
	}

	return password, nil
}

// ValidatePasswordFile validates a password file format and constraints
func (pfm *PasswordFileManager) ValidatePasswordFile(passwordPath string) error {
	if passwordPath == "" {
		return NewPasswordFileError(
			PasswordFileInvalid,
			"",
			"Password file path cannot be empty",
			nil,
		)
	}

	// Check if file exists and is accessible
	fileInfo, err := os.Stat(passwordPath)
	if os.IsNotExist(err) {
		return NewPasswordFileError(
			PasswordFileNotFound,
			passwordPath,
			fmt.Sprintf("Password file not found: %s", passwordPath),
			err,
		)
	} else if err != nil {
		return NewPasswordFileError(
			PasswordFileUnreadable,
			passwordPath,
			fmt.Sprintf("Cannot access password file: %s", passwordPath),
			err,
		)
	}

	// Check if it's a regular file
	if !fileInfo.Mode().IsRegular() {
		return NewPasswordFileError(
			PasswordFileInvalid,
			passwordPath,
			fmt.Sprintf("Password file is not a regular file: %s", passwordPath),
			nil,
		)
	}

	// Check file size (max 256 characters, but we allow some buffer for encoding)
	// UTF-8 can use up to 4 bytes per character, so we set a reasonable limit
	maxFileSize := int64(1024) // 1KB should be more than enough for 256 characters
	if fileInfo.Size() > maxFileSize {
		return NewPasswordFileError(
			PasswordFileOversized,
			passwordPath,
			fmt.Sprintf("Password file is too large (max %d bytes): %s", maxFileSize, passwordPath),
			nil,
		)
	}

	// Check if file is empty
	if fileInfo.Size() == 0 {
		return NewPasswordFileError(
			PasswordFileEmpty,
			passwordPath,
			fmt.Sprintf("Password file is empty: %s", passwordPath),
			nil,
		)
	}

	return nil
}

// RequiresManualPassword checks if a keystore file requires manual password input
// Returns true if no password file exists or if the password file is invalid
func (pfm *PasswordFileManager) RequiresManualPassword(keystorePath string) bool {
	passwordPath, err := pfm.FindPasswordFile(keystorePath)
	if err != nil {
		return true // No password file found or inaccessible
	}

	// Try to read the password file
	_, err = pfm.ReadPasswordFile(passwordPath)
	return err != nil // If we can't read it, manual password is required
}

// GetPasswordForKeystore attempts to get the password for a keystore file
// Returns the password if a valid .pwd file exists, otherwise returns an error
func (pfm *PasswordFileManager) GetPasswordForKeystore(keystorePath string) (string, error) {
	// Find the password file
	passwordPath, err := pfm.FindPasswordFile(keystorePath)
	if err != nil {
		return "", err
	}

	// Read and return the password
	return pfm.ReadPasswordFile(passwordPath)
}

// ValidatePasswordLength validates that a password meets length requirements
func (pfm *PasswordFileManager) ValidatePasswordLength(password string) error {
	const maxPasswordLength = 256

	if len(password) > maxPasswordLength {
		return NewPasswordFileError(
			PasswordFileInvalid,
			"",
			fmt.Sprintf("Password is too long (max %d characters)", maxPasswordLength),
			nil,
		)
	}

	return nil
}

// NewPasswordFileError creates a new PasswordFileError
func NewPasswordFileError(errorType PasswordFileErrorType, file string, message string, cause error) *PasswordFileError {
	return &PasswordFileError{
		Type:        errorType,
		File:        file,
		Message:     message,
		Cause:       cause,
		Recoverable: isPasswordFileErrorRecoverable(errorType),
		Context:     make(map[string]interface{}),
	}
}

// NewPasswordFileErrorWithRecovery creates a new PasswordFileError with recovery information
func NewPasswordFileErrorWithRecovery(errorType PasswordFileErrorType, file string, message string, recoverable bool, recoveryHint string, cause error) *PasswordFileError {
	return &PasswordFileError{
		Type:         errorType,
		File:         file,
		Message:      message,
		Cause:        cause,
		Recoverable:  recoverable,
		RecoveryHint: recoveryHint,
		Context:      make(map[string]interface{}),
	}
}

// isPasswordFileErrorRecoverable determines if a password file error type is recoverable
func isPasswordFileErrorRecoverable(errorType PasswordFileErrorType) bool {
	switch errorType {
	case PasswordFileNotFound, PasswordFileUnreadable, PasswordFileEmpty:
		return true // Can be recovered by manual password input or fixing the file
	case PasswordFileInvalid, PasswordFileOversized, PasswordFileCorrupted:
		return false // File is fundamentally broken
	default:
		return false
	}
}

// GetErrorTypeString returns a string representation of the error type
func (e PasswordFileErrorType) String() string {
	switch e {
	case PasswordFileNotFound:
		return "PASSWORD_FILE_NOT_FOUND"
	case PasswordFileUnreadable:
		return "PASSWORD_FILE_UNREADABLE"
	case PasswordFileEmpty:
		return "PASSWORD_FILE_EMPTY"
	case PasswordFileInvalid:
		return "PASSWORD_FILE_INVALID"
	case PasswordFileOversized:
		return "PASSWORD_FILE_OVERSIZED"
	case PasswordFileCorrupted:
		return "PASSWORD_FILE_CORRUPTED"
	default:
		return "PASSWORD_FILE_UNKNOWN_ERROR"
	}
}

// IsRecoverable returns whether this error can be recovered from
func (e *PasswordFileError) IsRecoverable() bool {
	return e.Recoverable
}

// GetRecoveryHint returns a hint for how to recover from this error
func (e *PasswordFileError) GetRecoveryHint() string {
	if e.RecoveryHint != "" {
		return e.RecoveryHint
	}
	return e.Type.GetLocalizationKey() + "_recovery"
}

// GetUserFriendlyMessage returns a user-friendly error message without sensitive information
func (e *PasswordFileError) GetUserFriendlyMessage() string {
	return e.Type.GetLocalizationKey()
}

// GetContext returns additional context information
func (e *PasswordFileError) GetContext() map[string]interface{} {
	if e.Context == nil {
		return make(map[string]interface{})
	}
	return e.Context
}

// SetContext sets additional context information
func (e *PasswordFileError) SetContext(key string, value interface{}) {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
}
