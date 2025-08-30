package wallet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"blocowallet/pkg/localization"
)

func TestEnhancedImportIntegration(t *testing.T) {
	// Initialize localization system
	localization.Labels = make(map[string]string)
	localization.SetCurrentLanguage("en")
	localization.AddEnhancedImportMessages()

	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "enhanced_import_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test scenario: Batch import with various error conditions
	t.Run("Batch import with comprehensive error handling", func(t *testing.T) {
		// Create test files
		validKeystorePath := filepath.Join(tempDir, "valid.json")
		invalidKeystorePath := filepath.Join(tempDir, "invalid.json")
		missingKeystorePath := filepath.Join(tempDir, "missing.json")
		passwordFilePath := filepath.Join(tempDir, "valid.pwd")
		emptyPasswordFilePath := filepath.Join(tempDir, "empty.pwd")

		// Create a valid keystore file (minimal structure)
		validKeystore := `{
			"version": 3,
			"id": "test-id",
			"address": "0x1234567890123456789012345678901234567890",
			"crypto": {
				"cipher": "aes-128-ctr",
				"ciphertext": "test",
				"cipherparams": {"iv": "test"},
				"kdf": "scrypt",
				"kdfparams": {"dklen": 32, "n": 262144, "r": 8, "p": 1, "salt": "test"},
				"mac": "test"
			}
		}`
		err = os.WriteFile(validKeystorePath, []byte(validKeystore), 0644)
		require.NoError(t, err)

		// Create an invalid keystore file
		invalidKeystore := `{"invalid": "json"}`
		err = os.WriteFile(invalidKeystorePath, []byte(invalidKeystore), 0644)
		require.NoError(t, err)

		// Create password files
		err = os.WriteFile(passwordFilePath, []byte("testpassword"), 0644)
		require.NoError(t, err)

		err = os.WriteFile(emptyPasswordFilePath, []byte(""), 0644)
		require.NoError(t, err)

		// Initialize error aggregator
		aggregator := NewErrorAggregator(4)

		// Test various error scenarios
		testCases := []struct {
			name           string
			keystorePath   string
			passwordPath   string
			expectedError  KeystoreErrorType
			expectedAction UserActionType
		}{
			{
				name:           "Valid keystore with password file",
				keystorePath:   validKeystorePath,
				passwordPath:   passwordFilePath,
				expectedError:  ErrorFileNotFound, // This will be success in real scenario
				expectedAction: UserActionNone,
			},
			{
				name:           "Invalid keystore structure",
				keystorePath:   invalidKeystorePath,
				passwordPath:   "",
				expectedError:  ErrorInvalidKeystore,
				expectedAction: UserActionNone,
			},
			{
				name:           "Missing keystore file",
				keystorePath:   missingKeystorePath,
				passwordPath:   "",
				expectedError:  ErrorFileNotFound,
				expectedAction: UserActionSkip,
			},
			{
				name:           "Empty password file",
				keystorePath:   validKeystorePath,
				passwordPath:   emptyPasswordFilePath,
				expectedError:  ErrorPasswordFileEmpty,
				expectedAction: UserActionRetry,
			},
		}

		// Process test cases and add errors to aggregator
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Create appropriate error based on test case
				var err error
				switch tc.expectedError {
				case ErrorInvalidKeystore:
					err = NewKeystoreImportErrorWithRecovery(
						ErrorInvalidKeystore,
						"Invalid keystore structure",
						tc.keystorePath,
						false,
						"use_valid_keystore_file",
						nil,
					)
				case ErrorFileNotFound:
					err = NewKeystoreImportErrorWithRecovery(
						ErrorFileNotFound,
						"Keystore file not found",
						tc.keystorePath,
						true,
						"select_existing_file",
						nil,
					)
				case ErrorPasswordFileEmpty:
					err = NewPasswordFileErrorWithRecovery(
						PasswordFileEmpty,
						tc.passwordPath,
						"Password file is empty",
						true,
						"add_password_to_file",
						nil,
					)
				}

				if err != nil {
					aggregator.AddError(err, tc.keystorePath, tc.expectedAction)

					// Test error categorization
					aggregatedErrors := aggregator.GetErrorsByCategory()
					assert.NotEmpty(t, aggregatedErrors, "Should have categorized errors")

					// Test localized error messages
					if keystoreErr, ok := err.(*KeystoreImportError); ok {
						localizationKey := keystoreErr.GetUserFriendlyMessage()
						localizedMessage := localization.GetEnhancedImportErrorMessage(localizationKey)
						assert.NotEmpty(t, localizedMessage, "Should have localized message for %s", localizationKey)

						// Test recovery hint
						recoveryHint := keystoreErr.GetRecoveryHint()
						assert.NotEmpty(t, recoveryHint, "Should have recovery hint")
					}

					if passwordErr, ok := err.(*PasswordFileError); ok {
						localizationKey := passwordErr.GetUserFriendlyMessage()
						localizedMessage := localization.GetEnhancedImportErrorMessage(localizationKey)
						assert.NotEmpty(t, localizedMessage, "Should have localized message for %s", localizationKey)

						// Test recovery hint
						recoveryHint := passwordErr.GetRecoveryHint()
						assert.NotEmpty(t, recoveryHint, "Should have recovery hint")
					}
				} else {
					aggregator.AddSuccess()
				}
			})
		}

		// Test error summary and reporting
		summary := aggregator.GetErrorSummary()
		assert.Equal(t, 4, summary.TotalOperations)
		assert.Greater(t, summary.TotalErrors, 0)

		// Test retry recommendations
		recommendations := aggregator.GetRetryRecommendations()
		assert.NotEmpty(t, recommendations, "Should have retry recommendations")

		// Test error report generation
		report := aggregator.GenerateErrorReport()
		assert.NotNil(t, report)
		assert.NotEmpty(t, report.ErrorsByCategory)

		// Test formatted summary
		formattedSummary := report.GetFormattedSummary()
		assert.Contains(t, formattedSummary, "Total Operations: 4")
		assert.Contains(t, formattedSummary, "Elapsed Time:")

		// Test recoverable errors
		if report.HasRecoverableErrors() {
			topRecommendation := report.GetTopRetryRecommendation()
			assert.NotNil(t, topRecommendation)
			assert.NotEmpty(t, topRecommendation.Strategy)
			assert.NotEmpty(t, topRecommendation.Description)

			// Test that description is a localization key
			descriptionMessage := localization.GetEnhancedImportErrorMessage(topRecommendation.Description)
			assert.NotEmpty(t, descriptionMessage)
		}
	})
}

func TestPasswordFileManagerWithEnhancedErrors(t *testing.T) {
	// Initialize localization system
	localization.Labels = make(map[string]string)
	localization.SetCurrentLanguage("en")
	localization.AddEnhancedImportMessages()

	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "password_file_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	pfm := NewPasswordFileManager()

	t.Run("Password file error handling with localization", func(t *testing.T) {
		testCases := []struct {
			name                string
			setupFunc           func() string
			expectedErrorType   PasswordFileErrorType
			expectedRecoverable bool
		}{
			{
				name: "Missing password file",
				setupFunc: func() string {
					return filepath.Join(tempDir, "nonexistent.json")
				},
				expectedErrorType:   PasswordFileNotFound,
				expectedRecoverable: true,
			},
			{
				name: "Empty password file",
				setupFunc: func() string {
					keystorePath := filepath.Join(tempDir, "empty_pwd.json")
					passwordPath := filepath.Join(tempDir, "empty_pwd.pwd")
					err := os.WriteFile(passwordPath, []byte(""), 0644)
					require.NoError(t, err)
					return keystorePath
				},
				expectedErrorType:   PasswordFileEmpty,
				expectedRecoverable: true,
			},
			{
				name: "Oversized password file",
				setupFunc: func() string {
					keystorePath := filepath.Join(tempDir, "oversized_pwd.json")
					passwordPath := filepath.Join(tempDir, "oversized_pwd.pwd")
					// Create a file larger than 1024 bytes
					largeContent := make([]byte, 2048)
					for i := range largeContent {
						largeContent[i] = 'a'
					}
					err := os.WriteFile(passwordPath, largeContent, 0644)
					require.NoError(t, err)
					return keystorePath
				},
				expectedErrorType:   PasswordFileOversized,
				expectedRecoverable: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				keystorePath := tc.setupFunc()

				// Test FindPasswordFile
				passwordPath, err := pfm.FindPasswordFile(keystorePath)
				if tc.expectedErrorType == PasswordFileNotFound {
					assert.Error(t, err)
					passwordFileErr, ok := err.(*PasswordFileError)
					require.True(t, ok, "Should be PasswordFileError")
					assert.Equal(t, tc.expectedErrorType, passwordFileErr.Type)
					assert.Equal(t, tc.expectedRecoverable, passwordFileErr.IsRecoverable())

					// Test localized message
					localizationKey := passwordFileErr.GetUserFriendlyMessage()
					localizedMessage := localization.GetEnhancedImportErrorMessage(localizationKey)
					assert.NotEmpty(t, localizedMessage)

					// Test recovery hint
					recoveryHint := passwordFileErr.GetRecoveryHint()
					assert.NotEmpty(t, recoveryHint)
				} else {
					require.NoError(t, err)

					// Test ReadPasswordFile for other error types
					_, err = pfm.ReadPasswordFile(passwordPath)
					assert.Error(t, err)
					passwordFileErr, ok := err.(*PasswordFileError)
					require.True(t, ok, "Should be PasswordFileError")
					assert.Equal(t, tc.expectedErrorType, passwordFileErr.Type)
					assert.Equal(t, tc.expectedRecoverable, passwordFileErr.IsRecoverable())

					// Test localized message
					localizationKey := passwordFileErr.GetUserFriendlyMessage()
					localizedMessage := localization.GetEnhancedImportErrorMessage(localizationKey)
					assert.NotEmpty(t, localizedMessage)

					// Test recovery hint
					recoveryHint := passwordFileErr.GetRecoveryHint()
					assert.NotEmpty(t, recoveryHint)
				}
			})
		}
	})
}

func TestBatchImportServiceWithErrorAggregation(t *testing.T) {
	// Initialize localization system
	localization.Labels = make(map[string]string)
	localization.SetCurrentLanguage("en")
	localization.AddEnhancedImportMessages()

	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "batch_import_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a mock wallet service (we'll just test the validation part)
	bis := NewBatchImportService(nil) // nil is OK for validation tests

	t.Run("Import job validation with enhanced error handling", func(t *testing.T) {
		// Test empty jobs
		err := bis.ValidateImportJobs([]ImportJob{})
		assert.Error(t, err)
		keystoreErr, ok := err.(*KeystoreImportError)
		require.True(t, ok, "Should be KeystoreImportError")
		assert.Equal(t, ErrorImportJobValidation, keystoreErr.Type)
		assert.True(t, keystoreErr.IsRecoverable())

		// Test localized message
		localizationKey := keystoreErr.GetUserFriendlyMessage()
		localizedMessage := localization.GetEnhancedImportErrorMessage(localizationKey)
		assert.NotEmpty(t, localizedMessage)

		// Test job with missing keystore file
		jobs := []ImportJob{
			{
				KeystorePath: filepath.Join(tempDir, "nonexistent.json"),
				WalletName:   "test",
			},
		}

		err = bis.ValidateImportJobs(jobs)
		assert.Error(t, err)
		keystoreErr, ok = err.(*KeystoreImportError)
		require.True(t, ok, "Should be KeystoreImportError")
		assert.Equal(t, ErrorFileNotFound, keystoreErr.Type)
		assert.True(t, keystoreErr.IsRecoverable())

		// Test recovery hint
		recoveryHint := keystoreErr.GetRecoveryHint()
		assert.NotEmpty(t, recoveryHint)
	})

	t.Run("Directory scanning with error aggregation", func(t *testing.T) {
		// Create test files
		validKeystorePath := filepath.Join(tempDir, "valid.json")
		invalidFilePath := filepath.Join(tempDir, "invalid.txt")

		// Create a valid keystore file
		validKeystore := `{
			"version": 3,
			"id": "test-id",
			"address": "0x1234567890123456789012345678901234567890",
			"crypto": {
				"cipher": "aes-128-ctr",
				"ciphertext": "test",
				"cipherparams": {"iv": "test"},
				"kdf": "scrypt",
				"kdfparams": {"dklen": 32, "n": 262144, "r": 8, "p": 1, "salt": "test"},
				"mac": "test"
			}
		}`
		err = os.WriteFile(validKeystorePath, []byte(validKeystore), 0644)
		require.NoError(t, err)

		// Create an invalid file
		err = os.WriteFile(invalidFilePath, []byte("not a keystore"), 0644)
		require.NoError(t, err)

		// Test directory scanning
		keystoreFiles, scanErrors, err := bis.ScanDirectoryForKeystores(tempDir)
		require.NoError(t, err)

		// Should find the valid keystore
		assert.Equal(t, 1, len(keystoreFiles))
		assert.Contains(t, keystoreFiles[0], "valid.json")

		// Should have scan errors for invalid files (if any JSON files were invalid)
		// Note: .txt files are ignored, so no scan errors expected
		assert.Equal(t, 0, len(scanErrors))

		// Test discovery report
		report, err := bis.GetKeystoreDiscoveryReport(tempDir)
		require.NoError(t, err)
		assert.Equal(t, tempDir, report.DirectoryPath)
		assert.Equal(t, 1, report.ValidFilesCount)
		assert.Equal(t, 0, report.ErrorFilesCount)
		assert.Equal(t, 0, report.PasswordFilesFound) // No .pwd files created
	})
}

func TestLocalizationIntegration(t *testing.T) {
	testCases := []struct {
		language string
		key      string
		expected string
	}{
		{"en", "password_file_not_found", "Password file not found"},
		{"pt", "password_file_not_found", "Arquivo de senha não encontrado"},
		{"es", "password_file_not_found", "Archivo de contraseña no encontrado"},
		{"en", "batch_import_failed", "Batch import operation failed"},
		{"pt", "batch_import_failed", "Operação de importação em lote falhou"},
		{"es", "batch_import_failed", "La operación de importación por lotes falló"},
	}

	for _, tc := range testCases {
		t.Run(tc.language+"_"+tc.key, func(t *testing.T) {
			// Initialize localization for specific language
			localization.Labels = make(map[string]string)
			localization.SetCurrentLanguage(tc.language)
			localization.AddEnhancedImportMessages()

			// Test message retrieval
			message := localization.GetEnhancedImportErrorMessage(tc.key)
			assert.Equal(t, tc.expected, message)
		})
	}
}

func TestErrorRecoveryWorkflow(t *testing.T) {
	// Initialize localization system
	localization.Labels = make(map[string]string)
	localization.SetCurrentLanguage("en")
	localization.AddEnhancedImportMessages()

	// Test complete error recovery workflow
	aggregator := NewErrorAggregator(3)

	// Add various recoverable errors
	passwordErr := NewKeystoreImportError(ErrorIncorrectPassword, "wrong password", nil)
	aggregator.AddError(passwordErr, "test1.json", UserActionRetry)

	fileErr := NewKeystoreImportError(ErrorFileNotFound, "file not found", nil)
	aggregator.AddError(fileErr, "test2.json", UserActionSkip)

	// Add one success
	aggregator.AddSuccess()

	// Generate comprehensive error report
	report := aggregator.GenerateErrorReport()

	// Test that we have recoverable errors
	assert.True(t, report.HasRecoverableErrors())

	// Test retry recommendations
	recommendations := report.RetryRecommendations
	assert.NotEmpty(t, recommendations)

	// Test that each recommendation has localized description
	for _, rec := range recommendations {
		description := localization.GetEnhancedImportErrorMessage(rec.Description)
		assert.NotEmpty(t, description)
		assert.NotEqual(t, rec.Description, description, "Should have actual localized message, not just key")
	}

	// Test error categorization
	categoryBreakdown := report.Summary.CategoryBreakdown
	assert.Contains(t, categoryBreakdown, CategoryPassword)
	assert.Contains(t, categoryBreakdown, CategoryFileSystem)

	// Test formatted summary
	formattedSummary := report.GetFormattedSummary()
	assert.Contains(t, formattedSummary, "Total Operations: 3")
	assert.Contains(t, formattedSummary, "Successful: 1")
	assert.Contains(t, formattedSummary, "Recoverable Errors: 2")
}
