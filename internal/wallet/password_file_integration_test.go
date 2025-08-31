package wallet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordFileIntegration(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "password_integration_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	pfm := NewPasswordFileManager()

	// Test data setup
	testCases := []struct {
		name          string
		keystoreFile  string
		passwordFile  string
		password      string
		shouldHavePwd bool
		shouldRequire bool
	}{
		{
			name:          "Keystore with password file",
			keystoreFile:  "wallet1.json",
			passwordFile:  "wallet1.pwd",
			password:      "testpassword123",
			shouldHavePwd: true,
			shouldRequire: false,
		},
		{
			name:          "Keystore without password file",
			keystoreFile:  "wallet2.json",
			passwordFile:  "",
			password:      "",
			shouldHavePwd: false,
			shouldRequire: true,
		},
		{
			name:          "Keystore with empty password file",
			keystoreFile:  "wallet3.json",
			passwordFile:  "wallet3.pwd",
			password:      "",
			shouldHavePwd: true,
			shouldRequire: true, // Empty password file should require manual input
		},
		{
			name:          "Keystore with complex password",
			keystoreFile:  "wallet4.json",
			passwordFile:  "wallet4.pwd",
			password:      "P@$w0rd!123#ComplexPassword",
			shouldHavePwd: true,
			shouldRequire: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup keystore path
			keystorePath := filepath.Join(tempDir, tc.keystoreFile)

			// Create password file if specified
			if tc.passwordFile != "" {
				passwordPath := filepath.Join(tempDir, tc.passwordFile)
				err := os.WriteFile(passwordPath, []byte(tc.password), 0644)
				require.NoError(t, err)
			}

			// Test RequiresManualPassword
			requiresManual := pfm.RequiresManualPassword(keystorePath)
			assert.Equal(t, tc.shouldRequire, requiresManual,
				"RequiresManualPassword result mismatch for %s", tc.name)

			// Test GetPasswordForKeystore
			if tc.shouldHavePwd && tc.password != "" {
				password, err := pfm.GetPasswordForKeystore(keystorePath)
				assert.NoError(t, err, "Should be able to get password for %s", tc.name)
				assert.Equal(t, tc.password, password, "Password mismatch for %s", tc.name)
			} else {
				password, err := pfm.GetPasswordForKeystore(keystorePath)
				assert.Error(t, err, "Should fail to get password for %s", tc.name)
				assert.Empty(t, password, "Password should be empty for %s", tc.name)
			}

			// Test FindPasswordFile
			passwordPath, err := pfm.FindPasswordFile(keystorePath)
			if tc.shouldHavePwd {
				assert.NoError(t, err, "Should find password file for %s", tc.name)
				assert.NotEmpty(t, passwordPath, "Password path should not be empty for %s", tc.name)
				assert.Contains(t, passwordPath, ".pwd", "Password path should contain .pwd for %s", tc.name)
			} else {
				assert.Error(t, err, "Should not find password file for %s", tc.name)
				var pwdErr *PasswordFileError
				assert.ErrorAs(t, err, &pwdErr)
				assert.Equal(t, PasswordFileNotFound, pwdErr.Type)
			}
		})
	}
}

func TestPasswordFileWithRealKeystores(t *testing.T) {
	pfm := NewPasswordFileManager()

	// Test with actual keystore files from testdata
	testCases := []struct {
		name         string
		keystorePath string
		expectedPwd  string
	}{
		{
			name:         "Standard keystore with password file",
			keystorePath: "testdata/keystores/real_keystore_v3_standard.json",
			expectedPwd:  "testpassword",
		},
		{
			name:         "Complex password keystore",
			keystorePath: "testdata/keystores/real_keystore_v3_complex_password.json",
			expectedPwd:  "P@$w0rd!123#ComplexPassword",
		},
		{
			name:         "Valid keystore with password file",
			keystorePath: "testdata/keystores/valid_keystore_v3.json",
			expectedPwd:  "testpassword",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Check if keystore file exists
			if _, err := os.Stat(tc.keystorePath); os.IsNotExist(err) {
				t.Skipf("Keystore file %s does not exist, skipping test", tc.keystorePath)
				return
			}

			// Test that it doesn't require manual password (should have .pwd file)
			requiresManual := pfm.RequiresManualPassword(tc.keystorePath)
			assert.False(t, requiresManual, "Should not require manual password for %s", tc.name)

			// Test getting the password
			password, err := pfm.GetPasswordForKeystore(tc.keystorePath)
			assert.NoError(t, err, "Should be able to get password for %s", tc.name)
			assert.Equal(t, tc.expectedPwd, password, "Password mismatch for %s", tc.name)

			// Test finding the password file
			passwordPath, err := pfm.FindPasswordFile(tc.keystorePath)
			assert.NoError(t, err, "Should find password file for %s", tc.name)
			assert.Contains(t, passwordPath, ".pwd", "Password path should contain .pwd for %s", tc.name)

			// Verify the password file exists
			_, err = os.Stat(passwordPath)
			assert.NoError(t, err, "Password file should exist at %s", passwordPath)
		})
	}
}

func TestPasswordFileErrorScenarios(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "password_error_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	pfm := NewPasswordFileManager()

	// Test oversized password file
	t.Run("Oversized password file", func(t *testing.T) {
		keystorePath := filepath.Join(tempDir, "oversized.json")
		passwordPath := filepath.Join(tempDir, "oversized.pwd")

		// Create a password file larger than 1024 bytes
		oversizedContent := make([]byte, 1025)
		for i := range oversizedContent {
			oversizedContent[i] = 'a'
		}
		err := os.WriteFile(passwordPath, oversizedContent, 0644)
		require.NoError(t, err)

		// Should require manual password due to oversized file
		requiresManual := pfm.RequiresManualPassword(keystorePath)
		assert.True(t, requiresManual, "Should require manual password for oversized file")

		// Should fail to get password
		password, err := pfm.GetPasswordForKeystore(keystorePath)
		assert.Error(t, err)
		assert.Empty(t, password)
		var pwdErr *PasswordFileError
		assert.ErrorAs(t, err, &pwdErr)
		assert.Equal(t, PasswordFileOversized, pwdErr.Type)
	})

	// Test corrupted UTF-8 password file
	t.Run("Corrupted UTF-8 password file", func(t *testing.T) {
		keystorePath := filepath.Join(tempDir, "corrupted.json")
		passwordPath := filepath.Join(tempDir, "corrupted.pwd")

		// Create a file with invalid UTF-8 bytes
		invalidUTF8 := []byte{0xff, 0xfe, 0xfd}
		err := os.WriteFile(passwordPath, invalidUTF8, 0644)
		require.NoError(t, err)

		// Should require manual password due to corrupted file
		requiresManual := pfm.RequiresManualPassword(keystorePath)
		assert.True(t, requiresManual, "Should require manual password for corrupted file")

		// Should fail to get password
		password, err := pfm.GetPasswordForKeystore(keystorePath)
		assert.Error(t, err)
		assert.Empty(t, password)
		var pwdErr *PasswordFileError
		assert.ErrorAs(t, err, &pwdErr)
		assert.Equal(t, PasswordFileCorrupted, pwdErr.Type)
	})

	// Test password file with only whitespace
	t.Run("Whitespace-only password file", func(t *testing.T) {
		keystorePath := filepath.Join(tempDir, "whitespace.json")
		passwordPath := filepath.Join(tempDir, "whitespace.pwd")

		// Create a file with only whitespace
		err := os.WriteFile(passwordPath, []byte("   \n\t  "), 0644)
		require.NoError(t, err)

		// Should require manual password due to empty content after trimming
		requiresManual := pfm.RequiresManualPassword(keystorePath)
		assert.True(t, requiresManual, "Should require manual password for whitespace-only file")

		// Should fail to get password
		password, err := pfm.GetPasswordForKeystore(keystorePath)
		assert.Error(t, err)
		assert.Empty(t, password)
		var pwdErr *PasswordFileError
		assert.ErrorAs(t, err, &pwdErr)
		assert.Equal(t, PasswordFileEmpty, pwdErr.Type)
	})
}
