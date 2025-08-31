package wallet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanDirectoryForKeystores(t *testing.T) {
	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "directory_scan_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Create subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	service := NewBatchImportService(nil)

	// Valid keystore content
	validKeystoreContent := `{
  "version": 3,
  "id": "f8b74b19-9a13-4b11-92a7-4c2a4c4e2e2d",
  "address": "5d8c5d3a5e6f6d6c5b4a3a2b1c0d9e8f7a6b5c4d",
  "crypto": {
    "ciphertext": "7a0f7e6d5c4b3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f",
    "cipherparams": {
      "iv": "6a5b4c3d2e1f0a9b8c7d6e5f4a3b2c1d"
    },
    "cipher": "aes-128-ctr",
    "kdf": "scrypt",
    "kdfparams": {
      "dklen": 32,
      "salt": "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f",
      "n": 8192,
      "r": 8,
      "p": 1
    },
    "mac": "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f"
  }
}`

	t.Run("successful directory scan with valid keystores", func(t *testing.T) {
		// Create test files
		keystore1 := filepath.Join(tempDir, "wallet1.json")
		keystore2 := filepath.Join(subDir, "wallet2.json")
		nonKeystore := filepath.Join(tempDir, "not_keystore.json")
		textFile := filepath.Join(tempDir, "readme.txt")

		// Create valid keystore files
		require.NoError(t, os.WriteFile(keystore1, []byte(validKeystoreContent), 0644))
		require.NoError(t, os.WriteFile(keystore2, []byte(validKeystoreContent), 0644))

		// Create invalid files
		require.NoError(t, os.WriteFile(nonKeystore, []byte(`{"not":"keystore"}`), 0644))
		require.NoError(t, os.WriteFile(textFile, []byte("This is not JSON"), 0644))

		keystoreFiles, scanErrors, err := service.ScanDirectoryForKeystores(tempDir)

		require.NoError(t, err)
		assert.Len(t, keystoreFiles, 2) // Should find 2 valid keystore files
		assert.Len(t, scanErrors, 1)    // Should have 1 invalid keystore error

		// Verify the found files
		foundFiles := make(map[string]bool)
		for _, file := range keystoreFiles {
			foundFiles[file] = true
		}

		assert.True(t, foundFiles[keystore1])
		assert.True(t, foundFiles[keystore2])

		// Verify scan errors
		assert.Equal(t, ScanErrorInvalidKeystore, scanErrors[0].Type)
		assert.Equal(t, nonKeystore, scanErrors[0].Path)
	})

	t.Run("empty directory", func(t *testing.T) {
		emptyDir := filepath.Join(tempDir, "empty")
		require.NoError(t, os.MkdirAll(emptyDir, 0755))

		keystoreFiles, scanErrors, err := service.ScanDirectoryForKeystores(emptyDir)

		require.NoError(t, err)
		assert.Len(t, keystoreFiles, 0)
		assert.Len(t, scanErrors, 0)
	})

	t.Run("directory with only invalid files", func(t *testing.T) {
		invalidDir := filepath.Join(tempDir, "invalid")
		require.NoError(t, os.MkdirAll(invalidDir, 0755))

		// Create only invalid files
		invalidFile1 := filepath.Join(invalidDir, "invalid1.json")
		invalidFile2 := filepath.Join(invalidDir, "invalid2.json")

		require.NoError(t, os.WriteFile(invalidFile1, []byte(`{"not":"keystore"}`), 0644))
		require.NoError(t, os.WriteFile(invalidFile2, []byte(`{"also":"not keystore"}`), 0644))

		keystoreFiles, scanErrors, err := service.ScanDirectoryForKeystores(invalidDir)

		require.NoError(t, err)
		assert.Len(t, keystoreFiles, 0)
		assert.Len(t, scanErrors, 2) // Should have 2 invalid keystore errors

		// Verify all errors are invalid keystore type
		for _, scanError := range scanErrors {
			assert.Equal(t, ScanErrorInvalidKeystore, scanError.Type)
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		nonExistentDir := filepath.Join(tempDir, "nonexistent")

		keystoreFiles, scanErrors, err := service.ScanDirectoryForKeystores(nonExistentDir)

		// The function should handle non-existent directories gracefully
		// It may return an error or empty results with scan errors
		if err != nil {
			assert.Contains(t, err.Error(), "no such file or directory")
		} else {
			// If no error, should have empty results
			assert.Len(t, keystoreFiles, 0)
			// May have scan errors for the directory access issue
			_ = scanErrors // Use the variable to avoid compiler error
		}
	})
}

func TestGetKeystoreDiscoveryReport(t *testing.T) {
	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "discovery_report_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	service := NewBatchImportService(nil)

	// Valid keystore content
	validKeystoreContent := `{
  "version": 3,
  "id": "f8b74b19-9a13-4b11-92a7-4c2a4c4e2e2d",
  "address": "5d8c5d3a5e6f6d6c5b4a3a2b1c0d9e8f7a6b5c4d",
  "crypto": {
    "ciphertext": "7a0f7e6d5c4b3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f",
    "cipherparams": {
      "iv": "6a5b4c3d2e1f0a9b8c7d6e5f4a3b2c1d"
    },
    "cipher": "aes-128-ctr",
    "kdf": "scrypt",
    "kdfparams": {
      "dklen": 32,
      "salt": "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f",
      "n": 8192,
      "r": 8,
      "p": 1
    },
    "mac": "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f"
  }
}`

	t.Run("comprehensive discovery report", func(t *testing.T) {
		// Create test files
		keystore1 := filepath.Join(tempDir, "wallet1.json")
		keystore2 := filepath.Join(tempDir, "wallet2.json")
		password1 := filepath.Join(tempDir, "wallet1.pwd")
		invalidKeystore := filepath.Join(tempDir, "invalid.json")

		// Create valid keystore files
		require.NoError(t, os.WriteFile(keystore1, []byte(validKeystoreContent), 0644))
		require.NoError(t, os.WriteFile(keystore2, []byte(validKeystoreContent), 0644))

		// Create password file for first keystore
		require.NoError(t, os.WriteFile(password1, []byte("password123"), 0644))

		// Create invalid keystore
		require.NoError(t, os.WriteFile(invalidKeystore, []byte(`{"not":"keystore"}`), 0644))

		report, err := service.GetKeystoreDiscoveryReport(tempDir)

		require.NoError(t, err)
		assert.NotNil(t, report)

		// Verify report contents
		assert.Equal(t, tempDir, report.DirectoryPath)
		assert.Len(t, report.ValidKeystores, 2)
		assert.Len(t, report.ScanErrors, 1)
		assert.Equal(t, 3, report.TotalFilesFound) // 2 valid + 1 invalid
		assert.Equal(t, 2, report.ValidFilesCount)
		assert.Equal(t, 1, report.ErrorFilesCount)
		assert.Equal(t, 1, report.PasswordFilesFound) // Only wallet1 has password file

		// Verify valid keystores are included
		foundFiles := make(map[string]bool)
		for _, file := range report.ValidKeystores {
			foundFiles[file] = true
		}
		assert.True(t, foundFiles[keystore1])
		assert.True(t, foundFiles[keystore2])

		// Verify scan error
		assert.Equal(t, ScanErrorInvalidKeystore, report.ScanErrors[0].Type)
		assert.Equal(t, invalidKeystore, report.ScanErrors[0].Path)
	})

	t.Run("empty directory report", func(t *testing.T) {
		emptyDir := filepath.Join(tempDir, "empty")
		require.NoError(t, os.MkdirAll(emptyDir, 0755))

		report, err := service.GetKeystoreDiscoveryReport(emptyDir)

		require.NoError(t, err)
		assert.NotNil(t, report)

		assert.Equal(t, emptyDir, report.DirectoryPath)
		assert.Len(t, report.ValidKeystores, 0)
		assert.Len(t, report.ScanErrors, 0)
		assert.Equal(t, 0, report.TotalFilesFound)
		assert.Equal(t, 0, report.ValidFilesCount)
		assert.Equal(t, 0, report.ErrorFilesCount)
		assert.Equal(t, 0, report.PasswordFilesFound)
	})

	t.Run("non-existent directory report", func(t *testing.T) {
		nonExistentDir := filepath.Join(tempDir, "nonexistent")

		report, err := service.GetKeystoreDiscoveryReport(nonExistentDir)

		// Should handle non-existent directories gracefully
		if err != nil {
			assert.Nil(t, report)
		} else {
			// If no error, should have empty report
			assert.NotNil(t, report)
			assert.Equal(t, 0, report.ValidFilesCount)
		}
	})
}

func TestDirectoryScanErrorTypes(t *testing.T) {
	t.Run("scan error type string representation", func(t *testing.T) {
		assert.Equal(t, "ACCESS_ERROR", ScanErrorAccess.String())
		assert.Equal(t, "INVALID_KEYSTORE", ScanErrorInvalidKeystore.String())
		assert.Equal(t, "READ_FAILURE", ScanErrorReadFailure.String())
	})

	t.Run("unknown scan error type", func(t *testing.T) {
		unknownType := ScanErrorType(999)
		assert.Equal(t, "UNKNOWN_ERROR", unknownType.String())
	})
}
