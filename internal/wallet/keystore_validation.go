package wallet

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// KeystoreErrorType represents different types of keystore import errors
type KeystoreErrorType int

const (
	ErrorFileNotFound KeystoreErrorType = iota
	ErrorInvalidJSON
	ErrorInvalidKeystore
	ErrorInvalidVersion
	ErrorIncorrectPassword
	ErrorCorruptedFile
	ErrorAddressMismatch
	ErrorMissingRequiredFields
	ErrorInvalidAddress
	// Password file related errors
	ErrorPasswordFileNotFound
	ErrorPasswordFileUnreadable
	ErrorPasswordFileEmpty
	ErrorPasswordFileInvalid
	ErrorPasswordFileOversized
	ErrorPasswordFileCorrupted
	// Batch import related errors
	ErrorBatchImportFailed
	ErrorImportJobValidation
	ErrorDirectoryScanFailed
	ErrorPasswordInputTimeout
	ErrorPasswordInputCancelled
	ErrorPasswordInputSkipped
	ErrorMaxAttemptsExceeded
	// Recovery and aggregation errors
	ErrorPartialImportFailure
	ErrorImportInterrupted
	ErrorCleanupFailed
)

// KeystoreImportError represents a specific error that occurred during keystore import
type KeystoreImportError struct {
	Type         KeystoreErrorType
	Message      string
	Cause        error
	Field        string                 // Optional field name that caused the error
	File         string                 // File path that caused the error
	Recoverable  bool                   // Whether this error can be recovered from
	RecoveryHint string                 // Hint for how to recover from this error
	Context      map[string]interface{} // Additional context information
}

// Error implements the error interface
func (e *KeystoreImportError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap returns the underlying error for error unwrapping
func (e *KeystoreImportError) Unwrap() error {
	return e.Cause
}

// KeystoreV3 represents the structure of a keystore v3 file
type KeystoreV3 struct {
	Version int              `json:"version"`
	ID      string           `json:"id"`
	Address string           `json:"address"`
	Crypto  KeystoreV3Crypto `json:"crypto"`
}

// KeystoreV3Crypto represents the crypto section of a keystore v3 file
type KeystoreV3Crypto struct {
	Cipher       string                 `json:"cipher"`
	CipherText   string                 `json:"ciphertext"`
	CipherParams KeystoreV3CipherParams `json:"cipherparams"`
	KDF          string                 `json:"kdf"`
	KDFParams    any                    `json:"kdfparams"`
	MAC          string                 `json:"mac"`
}

// KeystoreV3CipherParams represents the cipher parameters in a keystore v3 file
type KeystoreV3CipherParams struct {
	IV string `json:"iv"`
}

// KeystoreV3ScryptParams represents scrypt KDF parameters
type KeystoreV3ScryptParams struct {
	DKLen int    `json:"dklen"`
	N     int    `json:"n"`
	P     int    `json:"p"`
	R     int    `json:"r"`
	Salt  string `json:"salt"`
}

// KeystoreV3PBKDF2Params represents PBKDF2 KDF parameters
type KeystoreV3PBKDF2Params struct {
	DKLen int    `json:"dklen"`
	C     int    `json:"c"`
	PRF   string `json:"prf"`
	Salt  string `json:"salt"`
}

// KeystoreValidator provides methods to validate keystore files
type KeystoreValidator struct{}

// ValidateKeystoreV3 parses JSON data and validates the keystore structure
func (kv *KeystoreValidator) ValidateKeystoreV3(data []byte) (*KeystoreV3, error) {
	var keystore KeystoreV3

	// Parse JSON
	if err := json.Unmarshal(data, &keystore); err != nil {
		return nil, NewKeystoreImportError(ErrorInvalidJSON, "O arquivo não contém um JSON válido", err)
	}

	// Validate structure
	if err := kv.ValidateStructure(&keystore); err != nil {
		return nil, err
	}

	return &keystore, nil
}

// ValidateStructure checks if the keystore has all required fields
func (kv *KeystoreValidator) ValidateStructure(keystore *KeystoreV3) error {
	// Check version
	if err := kv.ValidateVersion(keystore.Version); err != nil {
		return err
	}

	// Check address
	if err := kv.ValidateAddress(keystore.Address); err != nil {
		return err
	}

	// Check crypto section
	if err := kv.ValidateCrypto(&keystore.Crypto); err != nil {
		return err
	}

	return nil
}

// ValidateVersion ensures the keystore version is exactly 3
func (kv *KeystoreValidator) ValidateVersion(version int) error {
	if version != 3 {
		return NewKeystoreImportErrorWithField(
			ErrorInvalidVersion,
			fmt.Sprintf("Invalid keystore version: %d, expected version 3", version),
			"version",
			nil,
		)
	}
	return nil
}

// ValidateAddress checks if the address is a valid Ethereum address
func (kv *KeystoreValidator) ValidateAddress(address string) error {
	if address == "" {
		return NewKeystoreImportErrorWithField(
			ErrorMissingRequiredFields,
			"Missing required field: address",
			"address",
			nil,
		)
	}

	// Remove 0x prefix if present for validation
	cleanAddress := address
	if strings.HasPrefix(strings.ToLower(address), "0x") {
		cleanAddress = address[2:]
	}

	// Ethereum addresses are 40 hex characters
	matched, err := regexp.MatchString("^[0-9a-fA-F]{40}$", cleanAddress)
	if err != nil {
		return NewKeystoreImportErrorWithField(
			ErrorInvalidAddress,
			"Error validating address format",
			"address",
			err,
		)
	}

	if !matched {
		return NewKeystoreImportErrorWithField(
			ErrorInvalidAddress,
			fmt.Sprintf("Invalid Ethereum address format: %s", address),
			"address",
			nil,
		)
	}

	return nil
}

// ValidateCrypto checks if the crypto section has all required fields
func (kv *KeystoreValidator) ValidateCrypto(crypto *KeystoreV3Crypto) error {
	if crypto.Cipher == "" {
		return NewKeystoreImportErrorWithField(
			ErrorMissingRequiredFields,
			"Missing required field: crypto.cipher",
			"crypto.cipher",
			nil,
		)
	}

	if crypto.CipherText == "" {
		return NewKeystoreImportErrorWithField(
			ErrorMissingRequiredFields,
			"Missing required field: crypto.ciphertext",
			"crypto.ciphertext",
			nil,
		)
	}

	if crypto.CipherParams.IV == "" {
		return NewKeystoreImportErrorWithField(
			ErrorMissingRequiredFields,
			"Missing required field: crypto.cipherparams.iv",
			"crypto.cipherparams.iv",
			nil,
		)
	}

	if crypto.KDF == "" {
		return NewKeystoreImportErrorWithField(
			ErrorMissingRequiredFields,
			"Missing required field: crypto.kdf",
			"crypto.kdf",
			nil,
		)
	}

	if crypto.KDFParams == nil {
		return NewKeystoreImportErrorWithField(
			ErrorMissingRequiredFields,
			"Missing required field: crypto.kdfparams",
			"crypto.kdfparams",
			nil,
		)
	}

	if crypto.MAC == "" {
		return NewKeystoreImportErrorWithField(
			ErrorMissingRequiredFields,
			"Missing required field: crypto.mac",
			"crypto.mac",
			nil,
		)
	}

	// Validate KDF parameters based on KDF type
	switch strings.ToLower(crypto.KDF) {
	case "scrypt":
		return kv.validateScryptParams(crypto.KDFParams)
	case "pbkdf2":
		return kv.validatePBKDF2Params(crypto.KDFParams)
	default:
		return NewKeystoreImportErrorWithField(
			ErrorInvalidKeystore,
			fmt.Sprintf("Unsupported KDF algorithm: %s", crypto.KDF),
			"crypto.kdf",
			nil,
		)
	}
}

// validateScryptParams validates scrypt KDF parameters
func (kv *KeystoreValidator) validateScryptParams(params any) error {
	// Convert interface{} to map for validation
	paramsMap, ok := params.(map[string]any)
	if !ok {
		return NewKeystoreImportErrorWithField(
			ErrorInvalidKeystore,
			"Invalid scrypt parameters format",
			"crypto.kdfparams",
			nil,
		)
	}

	// Check required fields
	requiredFields := []string{"dklen", "n", "r", "p", "salt"}
	for _, field := range requiredFields {
		if _, exists := paramsMap[field]; !exists {
			return NewKeystoreImportErrorWithField(
				ErrorMissingRequiredFields,
				fmt.Sprintf("Missing required field: crypto.kdfparams.%s", field),
				fmt.Sprintf("crypto.kdfparams.%s", field),
				nil,
			)
		}
	}

	return nil
}

// validatePBKDF2Params validates PBKDF2 KDF parameters
func (kv *KeystoreValidator) validatePBKDF2Params(params any) error {
	// Convert interface{} to map for validation
	paramsMap, ok := params.(map[string]any)
	if !ok {
		return NewKeystoreImportErrorWithField(
			ErrorInvalidKeystore,
			"Invalid PBKDF2 parameters format",
			"crypto.kdfparams",
			nil,
		)
	}

	// Check required fields
	requiredFields := []string{"dklen", "c", "prf", "salt"}
	for _, field := range requiredFields {
		if _, exists := paramsMap[field]; !exists {
			return NewKeystoreImportErrorWithField(
				ErrorMissingRequiredFields,
				fmt.Sprintf("Missing required field: crypto.kdfparams.%s", field),
				fmt.Sprintf("crypto.kdfparams.%s", field),
				nil,
			)
		}
	}

	return nil
}

// NewKeystoreImportError creates a new KeystoreImportError
func NewKeystoreImportError(errorType KeystoreErrorType, message string, cause error) *KeystoreImportError {
	return &KeystoreImportError{
		Type:        errorType,
		Message:     message,
		Cause:       cause,
		Recoverable: isRecoverableErrorType(errorType),
		Context:     make(map[string]interface{}),
	}
}

// NewKeystoreImportErrorWithField creates a new KeystoreImportError with a specific field
func NewKeystoreImportErrorWithField(errorType KeystoreErrorType, message string, field string, cause error) *KeystoreImportError {
	return &KeystoreImportError{
		Type:        errorType,
		Message:     message,
		Field:       field,
		Cause:       cause,
		Recoverable: isRecoverableErrorType(errorType),
		Context:     make(map[string]interface{}),
	}
}

// NewKeystoreImportErrorWithFile creates a new KeystoreImportError with file information
func NewKeystoreImportErrorWithFile(errorType KeystoreErrorType, message string, file string, cause error) *KeystoreImportError {
	return &KeystoreImportError{
		Type:        errorType,
		Message:     message,
		File:        file,
		Cause:       cause,
		Recoverable: isRecoverableErrorType(errorType),
		Context:     make(map[string]interface{}),
	}
}

// NewKeystoreImportErrorWithRecovery creates a new KeystoreImportError with recovery information
func NewKeystoreImportErrorWithRecovery(errorType KeystoreErrorType, message string, file string, recoverable bool, recoveryHint string, cause error) *KeystoreImportError {
	return &KeystoreImportError{
		Type:         errorType,
		Message:      message,
		File:         file,
		Cause:        cause,
		Recoverable:  recoverable,
		RecoveryHint: recoveryHint,
		Context:      make(map[string]interface{}),
	}
}

// isRecoverableErrorType determines if an error type is generally recoverable
func isRecoverableErrorType(errorType KeystoreErrorType) bool {
	switch errorType {
	case ErrorIncorrectPassword, ErrorPasswordFileNotFound, ErrorPasswordFileUnreadable,
		ErrorPasswordFileEmpty, ErrorPasswordInputTimeout, ErrorPasswordInputCancelled,
		ErrorPasswordInputSkipped:
		return true // These can be recovered by providing correct password or retrying
	case ErrorFileNotFound, ErrorDirectoryScanFailed:
		return true // These can be recovered by selecting different files/directories
	case ErrorImportJobValidation, ErrorBatchImportFailed:
		return true // These can be recovered by fixing the input
	case ErrorInvalidJSON, ErrorInvalidKeystore, ErrorInvalidVersion,
		ErrorCorruptedFile, ErrorAddressMismatch, ErrorMissingRequiredFields,
		ErrorInvalidAddress, ErrorPasswordFileCorrupted, ErrorPasswordFileInvalid:
		return false // These indicate fundamental issues with the files
	default:
		return false // Conservative approach for unknown errors
	}
}

// GetErrorTypeString returns a string representation of the error type
func (e KeystoreErrorType) String() string {
	switch e {
	case ErrorFileNotFound:
		return "FILE_NOT_FOUND"
	case ErrorInvalidJSON:
		return "INVALID_JSON"
	case ErrorInvalidKeystore:
		return "INVALID_KEYSTORE"
	case ErrorInvalidVersion:
		return "INVALID_VERSION"
	case ErrorIncorrectPassword:
		return "INCORRECT_PASSWORD"
	case ErrorCorruptedFile:
		return "CORRUPTED_FILE"
	case ErrorAddressMismatch:
		return "ADDRESS_MISMATCH"
	case ErrorMissingRequiredFields:
		return "MISSING_REQUIRED_FIELDS"
	case ErrorInvalidAddress:
		return "INVALID_ADDRESS"
	// Password file errors
	case ErrorPasswordFileNotFound:
		return "PASSWORD_FILE_NOT_FOUND"
	case ErrorPasswordFileUnreadable:
		return "PASSWORD_FILE_UNREADABLE"
	case ErrorPasswordFileEmpty:
		return "PASSWORD_FILE_EMPTY"
	case ErrorPasswordFileInvalid:
		return "PASSWORD_FILE_INVALID"
	case ErrorPasswordFileOversized:
		return "PASSWORD_FILE_OVERSIZED"
	case ErrorPasswordFileCorrupted:
		return "PASSWORD_FILE_CORRUPTED"
	// Batch import errors
	case ErrorBatchImportFailed:
		return "BATCH_IMPORT_FAILED"
	case ErrorImportJobValidation:
		return "IMPORT_JOB_VALIDATION_FAILED"
	case ErrorDirectoryScanFailed:
		return "DIRECTORY_SCAN_FAILED"
	case ErrorPasswordInputTimeout:
		return "PASSWORD_INPUT_TIMEOUT"
	case ErrorPasswordInputCancelled:
		return "PASSWORD_INPUT_CANCELLED"
	case ErrorPasswordInputSkipped:
		return "PASSWORD_INPUT_SKIPPED"
	case ErrorMaxAttemptsExceeded:
		return "MAX_PASSWORD_ATTEMPTS_EXCEEDED"
	// Recovery and aggregation errors
	case ErrorPartialImportFailure:
		return "PARTIAL_IMPORT_FAILURE"
	case ErrorImportInterrupted:
		return "IMPORT_INTERRUPTED"
	case ErrorCleanupFailed:
		return "CLEANUP_FAILED"
	default:
		return "UNKNOWN_ERROR"
	}
}

// GetLocalizationKey returns the localization key for this error type
func (e KeystoreErrorType) GetLocalizationKey() string {
	switch e {
	case ErrorFileNotFound:
		return "keystore_file_not_found"
	case ErrorInvalidJSON:
		return "keystore_invalid_json"
	case ErrorInvalidKeystore:
		return "keystore_invalid_structure"
	case ErrorInvalidVersion:
		return "keystore_invalid_version"
	case ErrorIncorrectPassword:
		return "keystore_incorrect_password"
	case ErrorCorruptedFile:
		return "keystore_corrupted_file"
	case ErrorAddressMismatch:
		return "keystore_address_mismatch"
	case ErrorMissingRequiredFields:
		return "keystore_missing_fields"
	case ErrorInvalidAddress:
		return "keystore_invalid_address"
	// Password file errors
	case ErrorPasswordFileNotFound:
		return "password_file_not_found"
	case ErrorPasswordFileUnreadable:
		return "password_file_unreadable"
	case ErrorPasswordFileEmpty:
		return "password_file_empty"
	case ErrorPasswordFileInvalid:
		return "password_file_invalid"
	case ErrorPasswordFileOversized:
		return "password_file_oversized"
	case ErrorPasswordFileCorrupted:
		return "password_file_corrupted"
	// Batch import errors
	case ErrorBatchImportFailed:
		return "batch_import_failed"
	case ErrorImportJobValidation:
		return "import_job_validation_failed"
	case ErrorDirectoryScanFailed:
		return "directory_scan_failed"
	case ErrorPasswordInputTimeout:
		return "password_input_timeout"
	case ErrorPasswordInputCancelled:
		return "password_input_cancelled"
	case ErrorPasswordInputSkipped:
		return "password_input_skipped"
	case ErrorMaxAttemptsExceeded:
		return "max_password_attempts_exceeded"
	// Recovery and aggregation errors
	case ErrorPartialImportFailure:
		return "partial_import_failure"
	case ErrorImportInterrupted:
		return "import_interrupted"
	case ErrorCleanupFailed:
		return "cleanup_failed"
	default:
		return "unknown_error"
	}
}

// GetLocalizedMessage returns a localized error message for this error
func (e *KeystoreImportError) GetLocalizedMessage() string {
	// This will be used by UI code to get localized messages
	key := e.Type.GetLocalizationKey()

	// The actual localization will be done by the UI layer
	// to avoid import cycles between packages
	return key
}

// GetLocalizedMessageWithField returns a localized error message with field information
func (e *KeystoreImportError) GetLocalizedMessageWithField() string {
	key := e.Type.GetLocalizationKey()

	// The actual localization will be done by the UI layer
	// but we return the field information here
	if e.Field != "" {
		return key + ":" + e.Field
	}
	return key
}

// IsRecoverable returns whether this error can be recovered from
func (e *KeystoreImportError) IsRecoverable() bool {
	return e.Recoverable
}

// GetRecoveryHint returns a hint for how to recover from this error
func (e *KeystoreImportError) GetRecoveryHint() string {
	if e.RecoveryHint != "" {
		return e.RecoveryHint
	}

	// Provide default recovery hints based on error type
	return e.Type.GetDefaultRecoveryHint()
}

// GetUserFriendlyMessage returns a user-friendly error message without sensitive information
func (e *KeystoreImportError) GetUserFriendlyMessage() string {
	// Return localization key for UI layer to handle
	// This ensures no sensitive information is exposed
	return e.Type.GetLocalizationKey()
}

// GetContext returns additional context information
func (e *KeystoreImportError) GetContext() map[string]interface{} {
	if e.Context == nil {
		return make(map[string]interface{})
	}
	return e.Context
}

// SetContext sets additional context information
func (e *KeystoreImportError) SetContext(key string, value interface{}) {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
}

// GetDefaultRecoveryHint returns a default recovery hint for the error type
func (e KeystoreErrorType) GetDefaultRecoveryHint() string {
	switch e {
	case ErrorFileNotFound:
		return "keystore_recovery_file_not_found"
	case ErrorInvalidJSON:
		return "keystore_recovery_invalid_json"
	case ErrorInvalidKeystore:
		return "keystore_recovery_invalid_structure"
	case ErrorIncorrectPassword:
		return "keystore_recovery_incorrect_password"
	case ErrorPasswordFileNotFound:
		return "password_file_recovery_not_found"
	case ErrorPasswordFileUnreadable:
		return "password_file_recovery_unreadable"
	case ErrorPasswordFileEmpty:
		return "password_file_recovery_empty"
	case ErrorPasswordFileInvalid:
		return "password_file_recovery_invalid"
	case ErrorBatchImportFailed:
		return "batch_import_recovery_failed"
	case ErrorDirectoryScanFailed:
		return "directory_scan_recovery_failed"
	case ErrorPasswordInputTimeout:
		return "password_input_recovery_timeout"
	case ErrorMaxAttemptsExceeded:
		return "password_attempts_recovery_exceeded"
	default:
		return "keystore_recovery_general"
	}
}

// IsPasswordRelated returns whether this error is related to password operations
func (e KeystoreErrorType) IsPasswordRelated() bool {
	switch e {
	case ErrorIncorrectPassword, ErrorPasswordFileNotFound, ErrorPasswordFileUnreadable,
		ErrorPasswordFileEmpty, ErrorPasswordFileInvalid, ErrorPasswordFileOversized,
		ErrorPasswordFileCorrupted, ErrorPasswordInputTimeout, ErrorPasswordInputCancelled,
		ErrorPasswordInputSkipped, ErrorMaxAttemptsExceeded:
		return true
	default:
		return false
	}
}

// IsFileSystemRelated returns whether this error is related to file system operations
func (e KeystoreErrorType) IsFileSystemRelated() bool {
	switch e {
	case ErrorFileNotFound, ErrorCorruptedFile, ErrorPasswordFileNotFound,
		ErrorPasswordFileUnreadable, ErrorPasswordFileOversized, ErrorDirectoryScanFailed:
		return true
	default:
		return false
	}
}

// IsValidationRelated returns whether this error is related to validation
func (e KeystoreErrorType) IsValidationRelated() bool {
	switch e {
	case ErrorInvalidJSON, ErrorInvalidKeystore, ErrorInvalidVersion,
		ErrorAddressMismatch, ErrorMissingRequiredFields, ErrorInvalidAddress,
		ErrorPasswordFileInvalid, ErrorImportJobValidation:
		return true
	default:
		return false
	}
}

// IsUserActionRelated returns whether this error is related to user actions
func (e KeystoreErrorType) IsUserActionRelated() bool {
	switch e {
	case ErrorPasswordInputCancelled, ErrorPasswordInputSkipped, ErrorImportInterrupted:
		return true
	default:
		return false
	}
}
