# Test Keystore Files

This directory contains sample keystore files for testing the keystore validation and import functionality.

## Valid Keystore Files

- `valid_keystore_v3.json`: A valid keystore v3 file with scrypt KDF
  - Password: `testpassword`
  - Address: `0x5d8c5d3a5e6f6d6c5b4a3a2b1c0d9e8f7a6b5c4d`

- `valid_keystore_v3_pbkdf2.json`: A valid keystore v3 file with PBKDF2 KDF
  - Password: `testpassword`
  - Address: `0x5d8c5d3a5e6f6d6c5b4a3a2b1c0d9e8f7a6b5c4d`

- `real_keystore_v3.json`: A real keystore v3 file generated with go-ethereum's keystore library (scrypt KDF)
  - Password: `testpassword`
  - Address: Generated from the private key

- `real_keystore_v3_pbkdf2.json`: A real keystore v3 file generated with go-ethereum's keystore library (PBKDF2 KDF)
  - Password: `testpassword`
  - Address: Generated from the private key

- `real_keystore_v3_standard.json`: A real keystore v3 file with standard scrypt parameters
  - Password: `testpassword`
  - Address: Generated from the private key

- `real_keystore_v3_light.json`: A real keystore v3 file with light scrypt parameters
  - Password: `testpassword`
  - Address: Generated from the private key

- `real_keystore_v3_complex_password.json`: A real keystore v3 file with a complex password
  - Password: `P@$$w0rd!123#ComplexPassword`
  - Address: Generated from the private key

- `real_keystore_v3_empty_password.json`: A real keystore v3 file with an empty password
  - Password: `` (empty string)
  - Address: Generated from the private key

## Invalid Keystore Files

- `invalid_version.json`: A keystore file with version 2 instead of 3
- `invalid_json.json`: A keystore file with invalid JSON syntax (will trigger "O arquivo não contém um JSON válido" error)
- `missing_address.json`: A keystore file missing the address field
- `invalid_address.json`: A keystore file with an invalid address format
- `missing_crypto.json`: A keystore file missing the crypto field
- `missing_cipher.json`: A keystore file missing the crypto.cipher field
- `missing_ciphertext.json`: A keystore file missing the crypto.ciphertext field
- `missing_iv.json`: A keystore file missing the crypto.cipherparams.iv field
- `missing_mac.json`: A keystore file missing the crypto.mac field
- `missing_scrypt_dklen.json`: A keystore file missing the crypto.kdfparams.dklen field
- `unsupported_kdf.json`: A keystore file with an unsupported KDF algorithm
- `corrupted_ciphertext.json`: A keystore file with corrupted ciphertext that is not valid hex
- `invalid_mac.json`: A keystore file with an invalid MAC value (will fail password validation)
- `invalid_kdf_params.json`: A keystore file with invalid KDF parameters (negative scrypt N value)
- `non_standard_cipher.json`: A keystore file with a non-standard cipher algorithm
- `malformed_address_with_prefix.json`: A keystore file with a malformed address (0x prefix but incorrect length)

## Generating Test Keystore Files

There are two generator scripts in this directory:

1. `generate_real_keystore.go`: Generates real keystore files with known private keys and passwords using the go-ethereum keystore library.
2. `generate_test_keystores.go`: Generates a comprehensive set of test keystore files for both valid and invalid scenarios.

To generate new keystore files:

```bash
cd internal/wallet/testdata
go run generate_test_keystores.go
```

This will create various test keystore files in the `keystores` directory and update the README.md file with details about each file.

## Usage in Tests

These files can be used for testing the keystore validation and import functionality. The valid keystore files can be used to test successful imports, while the invalid keystore files can be used to test error handling.

### Testing Files with Different Extensions

As part of the flexible keystore import feature, the system now supports importing keystore files with any extension or no extension. The validation now focuses on the file content rather than the extension, with updated error messages in Portuguese:

- For invalid JSON: "O arquivo não contém um JSON válido" (The file does not contain valid JSON)
- For invalid keystore structure: "O arquivo não contém um keystore v3 válido" (The file does not contain a valid keystore v3)

To test this functionality, you can create copies of the test files with different extensions:

```bash
# Create a copy with .key extension
cp keystores/valid_keystore_v3.json keystores/valid_keystore_v3.key

# Create a copy with no extension
cp keystores/valid_keystore_v3.json keystores/valid_keystore_v3

# Create a copy with a complex extension
cp keystores/valid_keystore_v3.json keystores/valid_keystore_v3.keystoremaster
```

These files can be used to verify that the system correctly validates the content regardless of the file extension.

Example:

```go
func TestImportWalletFromKeystoreV3(t *testing.T) {
    // Test with valid keystore file
    walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", "testdata/keystores/real_keystore_v3_standard.json", "testpassword")
    assert.NoError(t, err)
    assert.NotNil(t, walletDetails)

    // Test with invalid keystore file
    walletDetails, err = walletService.ImportWalletFromKeystoreV3("Test Wallet", "testdata/keystores/invalid_version.json", "testpassword")
    assert.Error(t, err)
    assert.Nil(t, walletDetails)
}
```

## Password Files (.pwd)

The enhanced keystore import feature supports automatic password detection using `.pwd` files. These files should:

- Have the same base name as the keystore file (e.g., `wallet1.json` → `wallet1.pwd`)
- Contain only the password as plain text
- Use UTF-8 encoding
- Be limited to 1024 bytes maximum
- Have proper file permissions for security

### Password File Examples

For testing the password file functionality, create corresponding `.pwd` files:

```bash
# Create password files for existing keystores
echo "testpassword" > keystores/real_keystore_v3_standard.pwd
echo "P@$w0rd!123#ComplexPassword" > keystores/real_keystore_v3_complex_password.pwd
echo "" > keystores/real_keystore_v3_empty_password.pwd
```

### Password File Error Scenarios

Test files for various error conditions:

- `empty.pwd`: Empty password file (should trigger PasswordFileEmpty error)
- `oversized.pwd`: Password file exceeding 1024 bytes (should trigger PasswordFileOversized error)
- `invalid_utf8.pwd`: File with invalid UTF-8 encoding (should trigger PasswordFileCorrupted error)
- `unreadable.pwd`: File with restricted permissions (should trigger PasswordFileUnreadable error)

### Password File Manager API

The `PasswordFileManager` provides the following functionality:

```go
// Create a new password file manager
pfm := NewPasswordFileManager()

// Check if a keystore requires manual password input
requiresManual := pfm.RequiresManualPassword("path/to/keystore.json")

// Get password for a keystore (if .pwd file exists)
password, err := pfm.GetPasswordForKeystore("path/to/keystore.json")

// Find password file for a keystore
passwordPath, err := pfm.FindPasswordFile("path/to/keystore.json")

// Read and validate a password file
password, err := pfm.ReadPasswordFile("path/to/keystore.pwd")
```

### Password Popup Component Testing

When password files are not available, the system uses the Password Popup Component for manual password input. This component can be tested using:

```go
// Create a password popup for testing
popup := NewPasswordPopupModel("test_keystore.json", 3) // 3 max retries

// Test password input scenarios
func TestPasswordPopupInteraction(t *testing.T) {
    // Test successful password entry
    popup := NewPasswordPopupModel("valid_keystore.json", 3)
    // Simulate user input and validation...
    
    // Test retry mechanism with incorrect passwords
    popup.SetError("Invalid password. Please try again.")
    assert.True(t, popup.retryCount > 0)
    
    // Test maximum retry limit
    for i := 0; i < 3; i++ {
        popup.SetError("Invalid password")
    }
    assert.True(t, popup.HasExceededMaxRetries())
    
    // Test skip functionality
    result := PasswordPopupResult{Cancelled: true, Skip: true}
    assert.True(t, result.Skip)
}
```

The Password Popup Component integrates with the batch import workflow to handle keystores that don't have corresponding `.pwd` files, providing a seamless user experience for mixed scenarios.

### Import Progress Component Testing

The Import Progress Component provides real-time feedback during batch keystore import operations. This component can be tested using:

```go
// Create an import progress component for testing
progress := NewImportProgressModel(5, styles) // 5 total files

// Test progress updates
func TestImportProgressTracking(t *testing.T) {
    // Test initial state
    assert.Equal(t, 0, progress.processedFiles)
    assert.Equal(t, 5, progress.totalFiles)
    assert.False(t, progress.IsCompleted())
    
    // Test progress updates
    progress.UpdateProgress("keystore1.json", 1)
    assert.Equal(t, "keystore1.json", progress.currentFile)
    assert.Equal(t, 1, progress.processedFiles)
    assert.Equal(t, 0.2, progress.GetPercentage())
    
    // Test error tracking
    err := errors.New("invalid password")
    progress.AddError("keystore2.json", err, false) // failed import
    progress.AddError("keystore3.json", err, true)  // skipped import
    
    assert.Equal(t, 2, len(progress.GetErrors()))
    assert.Equal(t, 1, len(progress.GetFailedErrors()))
    assert.Equal(t, 1, len(progress.GetSkippedErrors()))
    
    // Test pause/resume functionality
    progress.Pause("Waiting for password input")
    assert.True(t, progress.IsPaused())
    assert.Equal(t, "Waiting for password input", progress.pauseReason)
    
    progress.Resume()
    assert.False(t, progress.IsPaused())
    
    // Test completion
    progress.Complete()
    assert.True(t, progress.IsCompleted())
    assert.Contains(t, progress.GetSummaryText(), "Import completed")
}
```

### Batch Import Integration Testing

Test the complete workflow with multiple keystores:

```go
func TestBatchImportWithProgress(t *testing.T) {
    keystores := []string{
        "testdata/keystores/real_keystore_v3_standard.json",
        "testdata/keystores/real_keystore_v3_pbkdf2.json",
        "testdata/keystores/real_keystore_v3_light.json",
        "testdata/keystores/invalid_version.json", // This will fail
    }
    
    progress := NewImportProgressModel(len(keystores), styles)
    
    for i, keystore := range keystores {
        progress.UpdateProgress(filepath.Base(keystore), i)
        
        // Simulate import attempt
        if strings.Contains(keystore, "invalid") {
            err := errors.New("invalid keystore version")
            progress.AddError(keystore, err, false)
        }
        
        progress.UpdateProgress(filepath.Base(keystore), i+1)
    }
    
    progress.Complete()
    
    // Verify final state
    assert.True(t, progress.IsCompleted())
    assert.Equal(t, 4, progress.processedFiles)
    assert.Equal(t, 1, len(progress.GetFailedErrors()))
    assert.Equal(t, 0, len(progress.GetSkippedErrors()))
}
```

### Progress Message Testing

Test the BubbleTea message handling:

```go
func TestImportProgressMessages(t *testing.T) {
    progress := NewImportProgressModel(3, styles)
    
    // Test progress message
    msg := ImportProgressMsg{
        CurrentFile:    "test.json",
        ProcessedFiles: 1,
        TotalFiles:     3,
        Completed:      false,
        Paused:         false,
    }
    
    updatedProgress, cmd := progress.Update(msg)
    assert.Equal(t, "test.json", updatedProgress.currentFile)
    assert.Equal(t, 1, updatedProgress.processedFiles)
    assert.NotNil(t, cmd) // Should return progress update command
    
    // Test error message
    importErr := &ImportError{
        File:    "error.json",
        Error:   errors.New("test error"),
        Skipped: false,
    }
    
    errorMsg := ImportProgressMsg{
        Error: importErr,
    }
    
    updatedProgress, _ = updatedProgress.Update(errorMsg)
    assert.Equal(t, 1, len(updatedProgress.GetErrors()))
}
```

### Visual Rendering Testing

Test the progress component's visual output:

```go
func TestImportProgressView(t *testing.T) {
    progress := NewImportProgressModel(2, styles)
    
    // Test initial view
    view := progress.View()
    assert.Contains(t, view, "Import Progress")
    assert.Contains(t, view, "0/2 files (0.0%)")
    
    // Test active import view
    progress.UpdateProgress("importing.json", 1)
    view = progress.View()
    assert.Contains(t, view, "Processing: importing.json")
    assert.Contains(t, view, "1/2 files (50.0%)")
    assert.Contains(t, view, "⏳ Importing...")
    
    // Test paused view
    progress.Pause("Password required")
    view = progress.View()
    assert.Contains(t, view, "⏸ Import paused")
    assert.Contains(t, view, "Password required")
    
    // Test completed view
    progress.Resume()
    progress.UpdateProgress("", 2)
    progress.Complete()
    view = progress.View()
    assert.Contains(t, view, "✓ Import completed")
    assert.Contains(t, view, "Success: 2, Failed: 0, Skipped: 0")
}
```

The Import Progress Component provides comprehensive feedback during batch import operations, including real-time progress tracking, error categorization, pause/resume functionality, and detailed completion summaries.

## Security Note

For security reasons, the private keys used in these test files should never be used in a production environment. They are included here only for testing purposes.

**Password File Security**: Password files (.pwd) should be handled with care in production environments. Consider using proper file permissions (600) and secure storage locations to protect password files from unauthorized access.