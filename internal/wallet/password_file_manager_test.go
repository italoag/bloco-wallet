package wallet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordFileManager_FindPasswordFile(t *testing.T) {
	pfm := NewPasswordFileManager()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "password_file_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test cases
	tests := []struct {
		name          string
		keystorePath  string
		createPwdFile bool
		pwdContent    string
		expectedError PasswordFileErrorType
		shouldSucceed bool
	}{
		{
			name:          "Valid password file exists",
			keystorePath:  filepath.Join(tempDir, "wallet1.json"),
			createPwdFile: true,
			pwdContent:    "testpassword",
			shouldSucceed: true,
		},
		{
			name:          "Password file does not exist",
			keystorePath:  filepath.Join(tempDir, "wallet2.json"),
			createPwdFile: false,
			expectedError: PasswordFileNotFound,
			shouldSucceed: false,
		},
		{
			name:          "Empty keystore path",
			keystorePath:  "",
			createPwdFile: false,
			expectedError: PasswordFileInvalid,
			shouldSucceed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create password file if needed
			if tt.createPwdFile {
				pwdPath := filepath.Join(tempDir, "wallet1.pwd")
				err := os.WriteFile(pwdPath, []byte(tt.pwdContent), 0644)
				require.NoError(t, err)
			}

			// Test FindPasswordFile
			passwordPath, err := pfm.FindPasswordFile(tt.keystorePath)

			if tt.shouldSucceed {
				assert.NoError(t, err)
				assert.NotEmpty(t, passwordPath)
				assert.Contains(t, passwordPath, ".pwd")
			} else {
				assert.Error(t, err)
				var pwdErr *PasswordFileError
				assert.ErrorAs(t, err, &pwdErr)
				assert.Equal(t, tt.expectedError, pwdErr.Type)
			}
		})
	}
}

func TestPasswordFileManager_ReadPasswordFile(t *testing.T) {
	pfm := NewPasswordFileManager()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "password_file_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name          string
		content       []byte
		expectedPwd   string
		expectedError PasswordFileErrorType
		shouldSucceed bool
	}{
		{
			name:          "Valid password file",
			content:       []byte("testpassword"),
			expectedPwd:   "testpassword",
			shouldSucceed: true,
		},
		{
			name:          "Password with whitespace",
			content:       []byte("  testpassword  \n"),
			expectedPwd:   "testpassword",
			shouldSucceed: true,
		},
		{
			name:          "Empty password file",
			content:       []byte(""),
			expectedError: PasswordFileEmpty,
			shouldSucceed: false,
		},
		{
			name:          "Only whitespace",
			content:       []byte("   \n\t  "),
			expectedError: PasswordFileEmpty,
			shouldSucceed: false,
		},
		{
			name:          "Complex password",
			content:       []byte("P@$w0rd!123#ComplexPassword"),
			expectedPwd:   "P@$w0rd!123#ComplexPassword",
			shouldSucceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test password file
			pwdPath := filepath.Join(tempDir, "test.pwd")
			err := os.WriteFile(pwdPath, tt.content, 0644)
			require.NoError(t, err)

			// Test ReadPasswordFile
			password, err := pfm.ReadPasswordFile(pwdPath)

			if tt.shouldSucceed {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedPwd, password)
			} else {
				assert.Error(t, err)
				var pwdErr *PasswordFileError
				assert.ErrorAs(t, err, &pwdErr)
				assert.Equal(t, tt.expectedError, pwdErr.Type)
			}

			// Clean up
			os.Remove(pwdPath)
		})
	}
}

func TestPasswordFileManager_ValidatePasswordFile(t *testing.T) {
	pfm := NewPasswordFileManager()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "password_file_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name          string
		setupFunc     func() string
		expectedError PasswordFileErrorType
		shouldSucceed bool
	}{
		{
			name: "Valid password file",
			setupFunc: func() string {
				path := filepath.Join(tempDir, "valid.pwd")
				os.WriteFile(path, []byte("testpassword"), 0644)
				return path
			},
			shouldSucceed: true,
		},
		{
			name: "Empty password file",
			setupFunc: func() string {
				path := filepath.Join(tempDir, "empty.pwd")
				os.WriteFile(path, []byte(""), 0644)
				return path
			},
			expectedError: PasswordFileEmpty,
			shouldSucceed: false,
		},
		{
			name: "Oversized password file",
			setupFunc: func() string {
				path := filepath.Join(tempDir, "oversized.pwd")
				// Create a file larger than 1024 bytes
				content := make([]byte, 1025)
				for i := range content {
					content[i] = 'a'
				}
				os.WriteFile(path, content, 0644)
				return path
			},
			expectedError: PasswordFileOversized,
			shouldSucceed: false,
		},
		{
			name: "Non-existent file",
			setupFunc: func() string {
				return filepath.Join(tempDir, "nonexistent.pwd")
			},
			expectedError: PasswordFileNotFound,
			shouldSucceed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setupFunc()

			err := pfm.ValidatePasswordFile(path)

			if tt.shouldSucceed {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				var pwdErr *PasswordFileError
				assert.ErrorAs(t, err, &pwdErr)
				assert.Equal(t, tt.expectedError, pwdErr.Type)
			}
		})
	}
}

func TestPasswordFileManager_RequiresManualPassword(t *testing.T) {
	pfm := NewPasswordFileManager()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "password_file_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test case 1: Keystore with valid password file
	keystorePath1 := filepath.Join(tempDir, "wallet1.json")
	pwdPath1 := filepath.Join(tempDir, "wallet1.pwd")
	err = os.WriteFile(pwdPath1, []byte("testpassword"), 0644)
	require.NoError(t, err)

	requiresManual1 := pfm.RequiresManualPassword(keystorePath1)
	assert.False(t, requiresManual1, "Should not require manual password when valid .pwd file exists")

	// Test case 2: Keystore without password file
	keystorePath2 := filepath.Join(tempDir, "wallet2.json")
	requiresManual2 := pfm.RequiresManualPassword(keystorePath2)
	assert.True(t, requiresManual2, "Should require manual password when no .pwd file exists")

	// Test case 3: Keystore with empty password file
	keystorePath3 := filepath.Join(tempDir, "wallet3.json")
	pwdPath3 := filepath.Join(tempDir, "wallet3.pwd")
	err = os.WriteFile(pwdPath3, []byte(""), 0644)
	require.NoError(t, err)

	requiresManual3 := pfm.RequiresManualPassword(keystorePath3)
	assert.True(t, requiresManual3, "Should require manual password when .pwd file is empty")
}

func TestPasswordFileManager_GetPasswordForKeystore(t *testing.T) {
	pfm := NewPasswordFileManager()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "password_file_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test case 1: Valid keystore with password file
	keystorePath := filepath.Join(tempDir, "wallet1.json")
	pwdPath := filepath.Join(tempDir, "wallet1.pwd")
	expectedPassword := "testpassword123"
	err = os.WriteFile(pwdPath, []byte(expectedPassword), 0644)
	require.NoError(t, err)

	password, err := pfm.GetPasswordForKeystore(keystorePath)
	assert.NoError(t, err)
	assert.Equal(t, expectedPassword, password)

	// Test case 2: Keystore without password file
	keystorePath2 := filepath.Join(tempDir, "wallet2.json")
	password2, err2 := pfm.GetPasswordForKeystore(keystorePath2)
	assert.Error(t, err2)
	assert.Empty(t, password2)
	var pwdErr *PasswordFileError
	assert.ErrorAs(t, err2, &pwdErr)
	assert.Equal(t, PasswordFileNotFound, pwdErr.Type)
}

func TestPasswordFileManager_ValidatePasswordLength(t *testing.T) {
	pfm := NewPasswordFileManager()

	tests := []struct {
		name          string
		password      string
		shouldSucceed bool
	}{
		{
			name:          "Valid short password",
			password:      "test",
			shouldSucceed: true,
		},
		{
			name:          "Valid medium password",
			password:      "testpassword123",
			shouldSucceed: true,
		},
		{
			name:          "Valid max length password",
			password:      string(make([]byte, 256)),
			shouldSucceed: true,
		},
		{
			name:          "Too long password",
			password:      string(make([]byte, 257)),
			shouldSucceed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pfm.ValidatePasswordLength(tt.password)

			if tt.shouldSucceed {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				var pwdErr *PasswordFileError
				assert.ErrorAs(t, err, &pwdErr)
				assert.Equal(t, PasswordFileInvalid, pwdErr.Type)
			}
		})
	}
}

func TestPasswordFileError_Methods(t *testing.T) {
	// Test error creation and methods
	originalErr := os.ErrNotExist
	pwdErr := NewPasswordFileError(
		PasswordFileNotFound,
		"/path/to/file.pwd",
		"Test error message",
		originalErr,
	)

	// Test Error() method
	errorMsg := pwdErr.Error()
	assert.Contains(t, errorMsg, "Test error message")
	assert.Contains(t, errorMsg, originalErr.Error())

	// Test Unwrap() method
	unwrapped := pwdErr.Unwrap()
	assert.Equal(t, originalErr, unwrapped)

	// Test GetLocalizationKey() method
	locKey := PasswordFileNotFound.GetLocalizationKey()
	assert.Equal(t, "password_file_not_found", locKey)

	// Test String() method
	strRepr := PasswordFileNotFound.String()
	assert.Equal(t, "PASSWORD_FILE_NOT_FOUND", strRepr)
}
