package wallet

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBatchImportService(t *testing.T) {
	// Create a mock wallet service
	mockWalletService := &WalletService{}

	service := NewBatchImportService(mockWalletService)

	assert.NotNil(t, service)
	assert.Equal(t, mockWalletService, service.walletService)
	assert.NotNil(t, service.passwordMgr)
}

func TestCreateImportJobsFromFiles(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "batch_import_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	// Create test keystore files
	keystore1 := filepath.Join(tempDir, "wallet1.json")
	keystore2 := filepath.Join(tempDir, "wallet2.json")
	password1 := filepath.Join(tempDir, "wallet1.pwd")

	// Create the files
	require.NoError(t, os.WriteFile(keystore1, []byte(`{"version":3,"crypto":{},"address":"0x123"}`), 0644))
	require.NoError(t, os.WriteFile(keystore2, []byte(`{"version":3,"crypto":{},"address":"0x456"}`), 0644))
	require.NoError(t, os.WriteFile(password1, []byte("password123"), 0644))

	service := NewBatchImportService(nil)

	t.Run("successful job creation", func(t *testing.T) {
		jobs, err := service.CreateImportJobsFromFiles([]string{keystore1, keystore2})

		require.NoError(t, err)
		assert.Len(t, jobs, 2)

		// Check first job (has password file)
		assert.Equal(t, keystore1, jobs[0].KeystorePath)
		assert.Equal(t, password1, jobs[0].PasswordPath)
		assert.Equal(t, "wallet1", jobs[0].WalletName)
		assert.False(t, jobs[0].RequiresInput)

		// Check second job (no password file)
		assert.Equal(t, keystore2, jobs[1].KeystorePath)
		assert.Equal(t, "", jobs[1].PasswordPath)
		assert.Equal(t, "wallet2", jobs[1].WalletName)
		assert.True(t, jobs[1].RequiresInput)
	})

	t.Run("empty file list", func(t *testing.T) {
		jobs, err := service.CreateImportJobsFromFiles([]string{})

		assert.Error(t, err)
		assert.Nil(t, jobs)
		assert.Contains(t, err.Error(), "no keystore files provided")
	})

	t.Run("non-existent file", func(t *testing.T) {
		nonExistentFile := filepath.Join(tempDir, "nonexistent.json")

		jobs, err := service.CreateImportJobsFromFiles([]string{nonExistentFile})

		assert.Error(t, err)
		assert.Nil(t, jobs)
		assert.Contains(t, err.Error(), "keystore file not found")
	})
}

func TestCreateImportJobsFromDirectory(t *testing.T) {
	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "batch_import_dir_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	// Create subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	// Create test files
	keystore1 := filepath.Join(tempDir, "wallet1.json")
	keystore2 := filepath.Join(subDir, "wallet2.json")
	nonKeystore := filepath.Join(tempDir, "not_keystore.json")
	textFile := filepath.Join(tempDir, "readme.txt")

	// Create keystore files with valid keystore structure
	keystoreContent := `{
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
	require.NoError(t, os.WriteFile(keystore1, []byte(keystoreContent), 0644))
	require.NoError(t, os.WriteFile(keystore2, []byte(keystoreContent), 0644))

	// Create non-keystore JSON file
	require.NoError(t, os.WriteFile(nonKeystore, []byte(`{"not":"keystore"}`), 0644))
	require.NoError(t, os.WriteFile(textFile, []byte("This is not JSON"), 0644))

	service := NewBatchImportService(nil)

	t.Run("successful directory scan", func(t *testing.T) {
		jobs, err := service.CreateImportJobsFromDirectory(tempDir)

		require.NoError(t, err)
		assert.Len(t, jobs, 2) // Should find 2 keystore files

		// Verify the jobs contain the correct files
		foundFiles := make(map[string]bool)
		for _, job := range jobs {
			foundFiles[job.KeystorePath] = true
		}

		assert.True(t, foundFiles[keystore1])
		assert.True(t, foundFiles[keystore2])
	})

	t.Run("empty directory path", func(t *testing.T) {
		jobs, err := service.CreateImportJobsFromDirectory("")

		assert.Error(t, err)
		assert.Nil(t, jobs)
		assert.Contains(t, err.Error(), "directory path cannot be empty")
	})

	t.Run("non-existent directory", func(t *testing.T) {
		nonExistentDir := filepath.Join(tempDir, "nonexistent")

		jobs, err := service.CreateImportJobsFromDirectory(nonExistentDir)

		assert.Error(t, err)
		assert.Nil(t, jobs)
		assert.Contains(t, err.Error(), "directory not found")
	})

	t.Run("path is not a directory", func(t *testing.T) {
		jobs, err := service.CreateImportJobsFromDirectory(keystore1)

		assert.Error(t, err)
		assert.Nil(t, jobs)
		assert.Contains(t, err.Error(), "path is not a directory")
	})

	t.Run("directory with no keystore files", func(t *testing.T) {
		emptyDir := filepath.Join(tempDir, "empty")
		require.NoError(t, os.MkdirAll(emptyDir, 0755))

		jobs, err := service.CreateImportJobsFromDirectory(emptyDir)

		assert.Error(t, err)
		assert.Nil(t, jobs)
		assert.Contains(t, err.Error(), "no valid keystore files found")
	})
}

func TestIsValidKeystoreFile(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "keystore_detection_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	service := NewBatchImportService(nil)

	t.Run("valid keystore file", func(t *testing.T) {
		keystoreFile := filepath.Join(tempDir, "valid_keystore.json")
		content := `{
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
		require.NoError(t, os.WriteFile(keystoreFile, []byte(content), 0644))

		isKeystore := service.isValidKeystoreFile(keystoreFile)
		assert.True(t, isKeystore)
	})

	t.Run("keystore with Crypto field", func(t *testing.T) {
		keystoreFile := filepath.Join(tempDir, "crypto_uppercase.json")
		content := `{
  "version": 3,
  "id": "f8b74b19-9a13-4b11-92a7-4c2a4c4e2e2d",
  "address": "5d8c5d3a5e6f6d6c5b4a3a2b1c0d9e8f7a6b5c4d",
  "Crypto": {
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
		require.NoError(t, os.WriteFile(keystoreFile, []byte(content), 0644))

		isKeystore := service.isValidKeystoreFile(keystoreFile)
		assert.True(t, isKeystore)
	})

	t.Run("invalid JSON file", func(t *testing.T) {
		nonKeystoreFile := filepath.Join(tempDir, "not_keystore.json")
		content := `{"not":"keystore","missing":"required_fields"}`
		require.NoError(t, os.WriteFile(nonKeystoreFile, []byte(content), 0644))

		isKeystore := service.isValidKeystoreFile(nonKeystoreFile)
		assert.False(t, isKeystore)
	})

	t.Run("non-existent file", func(t *testing.T) {
		nonExistentFile := filepath.Join(tempDir, "nonexistent.json")

		isKeystore := service.isValidKeystoreFile(nonExistentFile)
		assert.False(t, isKeystore)
	})

	t.Run("empty file", func(t *testing.T) {
		emptyFile := filepath.Join(tempDir, "empty.json")
		require.NoError(t, os.WriteFile(emptyFile, []byte(""), 0644))

		isKeystore := service.isValidKeystoreFile(emptyFile)
		assert.False(t, isKeystore)
	})
}

func TestValidateImportJobs(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "validate_jobs_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	// Create test files
	validKeystore := filepath.Join(tempDir, "valid.json")
	validPassword := filepath.Join(tempDir, "valid.pwd")
	invalidPassword := filepath.Join(tempDir, "invalid.pwd")

	require.NoError(t, os.WriteFile(validKeystore, []byte(`{"version":3}`), 0644))
	require.NoError(t, os.WriteFile(validPassword, []byte("password123"), 0644))
	require.NoError(t, os.WriteFile(invalidPassword, []byte(""), 0644)) // Empty password file

	service := NewBatchImportService(nil)

	t.Run("valid jobs", func(t *testing.T) {
		jobs := []ImportJob{
			{
				KeystorePath:  validKeystore,
				PasswordPath:  validPassword,
				WalletName:    "wallet1",
				RequiresInput: false,
			},
		}

		err := service.ValidateImportJobs(jobs)
		assert.NoError(t, err)
	})

	t.Run("empty jobs list", func(t *testing.T) {
		jobs := []ImportJob{}

		err := service.ValidateImportJobs(jobs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no import jobs provided")
	})

	t.Run("empty keystore path", func(t *testing.T) {
		jobs := []ImportJob{
			{
				KeystorePath: "",
				WalletName:   "wallet1",
			},
		}

		err := service.ValidateImportJobs(jobs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "keystore path cannot be empty")
	})

	t.Run("non-existent keystore file", func(t *testing.T) {
		jobs := []ImportJob{
			{
				KeystorePath: filepath.Join(tempDir, "nonexistent.json"),
				WalletName:   "wallet1",
			},
		}

		err := service.ValidateImportJobs(jobs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "keystore file not found")
	})

	t.Run("empty wallet name", func(t *testing.T) {
		jobs := []ImportJob{
			{
				KeystorePath: validKeystore,
				WalletName:   "",
			},
		}

		err := service.ValidateImportJobs(jobs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "wallet name cannot be empty")
	})

	t.Run("invalid password file", func(t *testing.T) {
		jobs := []ImportJob{
			{
				KeystorePath:  validKeystore,
				PasswordPath:  invalidPassword,
				WalletName:    "wallet1",
				RequiresInput: false,
			},
		}

		err := service.ValidateImportJobs(jobs)
		assert.NoError(t, err) // Should not error, but should mark as requiring input

		// Check that the job was modified to require input
		assert.True(t, jobs[0].RequiresInput)
		assert.Equal(t, "", jobs[0].PasswordPath) // Should clear invalid path
	})
}

func TestGetImportSummary(t *testing.T) {
	service := NewBatchImportService(nil)

	results := []ImportResult{
		{
			Job:     ImportJob{KeystorePath: "file1.json"},
			Success: true,
			Wallet:  &WalletDetails{},
			Error:   nil,
			Skipped: false,
		},
		{
			Job:     ImportJob{KeystorePath: "file2.json"},
			Success: false,
			Wallet:  nil,
			Error:   fmt.Errorf("import failed"),
			Skipped: false,
		},
		{
			Job:     ImportJob{KeystorePath: "file3.json"},
			Success: false,
			Wallet:  nil,
			Error:   fmt.Errorf("user skipped"),
			Skipped: true,
		},
	}

	summary := service.GetImportSummary(results)

	assert.Equal(t, 3, summary.TotalFiles)
	assert.Equal(t, 1, summary.SuccessfulImports)
	assert.Equal(t, 1, summary.FailedImports)
	assert.Equal(t, 1, summary.SkippedImports)
	assert.Len(t, summary.Errors, 1) // Only failed imports, not skipped ones
	assert.Equal(t, "file2.json", summary.Errors[0].File)
	assert.False(t, summary.Errors[0].Skipped)
}

func TestImportBatchChannelCommunication(t *testing.T) {
	// This test focuses on channel communication without actual wallet import
	service := NewBatchImportService(nil)

	// Create a simple job that requires manual input
	jobs := []ImportJob{
		{
			KeystorePath:  "test.json",
			WalletName:    "test",
			RequiresInput: true,
		},
	}

	// Create channels
	progressChan := make(chan ImportProgress, 10)
	passwordRequestChan := make(chan PasswordRequest, 1)
	passwordResponseChan := make(chan PasswordResponse, 1)

	// Start import in goroutine
	go func() {
		service.ImportBatch(jobs, progressChan, passwordRequestChan, passwordResponseChan)
	}()

	// Check that we receive initial progress
	select {
	case progress := <-progressChan:
		assert.Equal(t, 1, progress.TotalFiles)
		assert.Equal(t, 0, progress.ProcessedFiles)
		assert.Equal(t, 0.0, progress.Percentage)
	case <-time.After(1 * time.Second):
		t.Fatal("Did not receive initial progress")
	}

	// Should receive a password request
	select {
	case request := <-passwordRequestChan:
		assert.Equal(t, "test.json", request.KeystoreFile)
		assert.Equal(t, 1, request.AttemptCount)
		assert.False(t, request.IsRetry)
		assert.Equal(t, "", request.ErrorMessage)
	case <-time.After(1 * time.Second):
		t.Fatal("Did not receive password request")
	}

	// Send skip response
	passwordResponseChan <- PasswordResponse{
		Password:  "",
		Cancelled: false,
		Skip:      true,
	}

	// Should receive final progress
	var finalProgress ImportProgress
	progressReceived := false

	outerLoop:
	for {
		select {
		case progress, ok := <-progressChan:
			if !ok {
				// Channel closed, we're done
				break outerLoop
			}
			finalProgress = progress
			if progress.ProcessedFiles == 1 {
				progressReceived = true
			}
		case <-time.After(2 * time.Second):
			break outerLoop
		}

		if !progressReceived {
			continue
		}
		break
	}

	assert.True(t, progressReceived, "Did not receive final progress")
	assert.Equal(t, 1, finalProgress.TotalFiles)
	assert.Equal(t, 1, finalProgress.ProcessedFiles)
	assert.Equal(t, 100.0, finalProgress.Percentage)
}

func TestPasswordRequest(t *testing.T) {
	request := PasswordRequest{
		KeystoreFile: "test.json",
		AttemptCount: 2,
	}

	assert.Equal(t, "test.json", request.KeystoreFile)
	assert.Equal(t, 2, request.AttemptCount)
}

func TestPasswordResponse(t *testing.T) {
	t.Run("password provided", func(t *testing.T) {
		response := PasswordResponse{
			Password:  "secret123",
			Cancelled: false,
			Skip:      false,
		}

		assert.Equal(t, "secret123", response.Password)
		assert.False(t, response.Cancelled)
		assert.False(t, response.Skip)
	})

	t.Run("cancelled", func(t *testing.T) {
		response := PasswordResponse{
			Password:  "",
			Cancelled: true,
			Skip:      false,
		}

		assert.Equal(t, "", response.Password)
		assert.True(t, response.Cancelled)
		assert.False(t, response.Skip)
	})

	t.Run("skipped", func(t *testing.T) {
		response := PasswordResponse{
			Password:  "",
			Cancelled: false,
			Skip:      true,
		}

		assert.Equal(t, "", response.Password)
		assert.False(t, response.Cancelled)
		assert.True(t, response.Skip)
	})
}

func TestImportProgress(t *testing.T) {
	startTime := time.Now()
	progress := ImportProgress{
		CurrentFile:     "wallet1.json",
		TotalFiles:      5,
		ProcessedFiles:  2,
		Percentage:      40.0,
		Errors:          []ImportError{},
		PendingPassword: true,
		PendingFile:     "wallet3.json",
		StartTime:       startTime,
		ElapsedTime:     time.Minute,
	}

	assert.Equal(t, "wallet1.json", progress.CurrentFile)
	assert.Equal(t, 5, progress.TotalFiles)
	assert.Equal(t, 2, progress.ProcessedFiles)
	assert.Equal(t, 40.0, progress.Percentage)
	assert.True(t, progress.PendingPassword)
	assert.Equal(t, "wallet3.json", progress.PendingFile)
	assert.Equal(t, startTime, progress.StartTime)
	assert.Equal(t, time.Minute, progress.ElapsedTime)
}

func TestImportError(t *testing.T) {
	err := ImportError{
		File:    "wallet1.json",
		Error:   fmt.Errorf("test error"),
		Skipped: false,
	}

	assert.Equal(t, "wallet1.json", err.File)
	assert.Equal(t, "test error", err.Error.Error())
	assert.False(t, err.Skipped)
}

func TestImportJob(t *testing.T) {
	job := ImportJob{
		KeystorePath:   "/path/to/keystore.json",
		PasswordPath:   "/path/to/keystore.pwd",
		ManualPassword: "manual123",
		WalletName:     "MyWallet",
		RequiresInput:  false,
	}

	assert.Equal(t, "/path/to/keystore.json", job.KeystorePath)
	assert.Equal(t, "/path/to/keystore.pwd", job.PasswordPath)
	assert.Equal(t, "manual123", job.ManualPassword)
	assert.Equal(t, "MyWallet", job.WalletName)
	assert.False(t, job.RequiresInput)
}

func TestImportResult(t *testing.T) {
	job := ImportJob{KeystorePath: "test.json"}
	wallet := &WalletDetails{}
	err := fmt.Errorf("test error")

	result := ImportResult{
		Job:     job,
		Success: false,
		Wallet:  wallet,
		Error:   err,
		Skipped: true,
	}

	assert.Equal(t, job, result.Job)
	assert.False(t, result.Success)
	assert.Equal(t, wallet, result.Wallet)
	assert.Equal(t, err, result.Error)
	assert.True(t, result.Skipped)
}

func TestPasswordInputError(t *testing.T) {
	t.Run("password input cancelled", func(t *testing.T) {
		err := &PasswordInputError{
			Type:    PasswordInputCancelled,
			Message: "cancelled by user",
			File:    "test.json",
		}

		assert.Equal(t, "cancelled by user", err.Error())
		assert.True(t, err.IsCancelled())
		assert.False(t, err.IsSkipped())
	})

	t.Run("password input skipped", func(t *testing.T) {
		err := &PasswordInputError{
			Type:    PasswordInputSkipped,
			Message: "skipped by user",
			File:    "test.json",
		}

		assert.Equal(t, "skipped by user", err.Error())
		assert.False(t, err.IsCancelled())
		assert.True(t, err.IsSkipped())
	})

	t.Run("password input timeout", func(t *testing.T) {
		err := &PasswordInputError{
			Type:    PasswordInputTimeout,
			Message: "timeout occurred",
			File:    "test.json",
		}

		assert.Equal(t, "timeout occurred", err.Error())
		assert.False(t, err.IsCancelled())
		assert.False(t, err.IsSkipped())
	})
}

func TestPasswordRetryMechanism(t *testing.T) {
	service := NewBatchImportService(nil)

	// Create a temporary keystore file for testing
	tempDir, err := os.MkdirTemp("", "password_retry_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	keystoreFile := filepath.Join(tempDir, "test.json")
	// Create a simple keystore structure that will fail password validation
	keystoreContent := `{"version":3,"crypto":{"cipher":"aes-128-ctr"},"address":"0x123"}`
	require.NoError(t, os.WriteFile(keystoreFile, []byte(keystoreContent), 0644))

	jobs := []ImportJob{
		{
			KeystorePath:  keystoreFile,
			WalletName:    "test",
			RequiresInput: true,
		},
	}

	progressChan := make(chan ImportProgress, 10)
	passwordRequestChan := make(chan PasswordRequest, 5)
	passwordResponseChan := make(chan PasswordResponse, 5)

	// Start import in goroutine
	go func() {
		service.ImportBatch(jobs, progressChan, passwordRequestChan, passwordResponseChan)
	}()

	// Consume initial progress
	<-progressChan

	// First password request
	request1 := <-passwordRequestChan
	assert.Equal(t, 1, request1.AttemptCount)
	assert.False(t, request1.IsRetry)
	assert.Equal(t, "", request1.ErrorMessage)

	// Send incorrect password
	passwordResponseChan <- PasswordResponse{
		Password:  "wrong_password",
		Cancelled: false,
		Skip:      false,
	}

	// Second password request (retry)
	request2 := <-passwordRequestChan
	assert.Equal(t, 2, request2.AttemptCount)
	assert.True(t, request2.IsRetry)
	assert.Equal(t, "Incorrect password. Please try again.", request2.ErrorMessage)

	// Send another incorrect password
	passwordResponseChan <- PasswordResponse{
		Password:  "still_wrong",
		Cancelled: false,
		Skip:      false,
	}

	// Third password request (final retry)
	request3 := <-passwordRequestChan
	assert.Equal(t, 3, request3.AttemptCount)
	assert.True(t, request3.IsRetry)
	assert.Equal(t, "Incorrect password. Please try again.", request3.ErrorMessage)

	// Send final incorrect password
	passwordResponseChan <- PasswordResponse{
		Password:  "final_wrong",
		Cancelled: false,
		Skip:      false,
	}

	// Consume remaining progress updates
	for {
		select {
		case _, ok := <-progressChan:
			if !ok {
				return // Channel closed, test complete
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Test timed out waiting for completion")
		}
	}
}

func TestPasswordPopupCancellation(t *testing.T) {
	service := NewBatchImportService(nil)

	jobs := []ImportJob{
		{
			KeystorePath:  "test.json",
			WalletName:    "test",
			RequiresInput: true,
		},
	}

	progressChan := make(chan ImportProgress, 10)
	passwordRequestChan := make(chan PasswordRequest, 1)
	passwordResponseChan := make(chan PasswordResponse, 1)

	// Start import in goroutine
	var results []ImportResult
	done := make(chan bool)
	go func() {
		results = service.ImportBatch(jobs, progressChan, passwordRequestChan, passwordResponseChan)
		done <- true
	}()

	// Consume initial progress
	<-progressChan

	// Receive password request
	<-passwordRequestChan

	// Send cancellation response
	passwordResponseChan <- PasswordResponse{
		Password:  "",
		Cancelled: true,
		Skip:      false,
	}

	// Wait for completion
	<-done

	// Verify result
	require.Len(t, results, 1)
	assert.False(t, results[0].Success)
	assert.True(t, results[0].Skipped)
	assert.IsType(t, &PasswordInputError{}, results[0].Error)

	passwordErr := results[0].Error.(*PasswordInputError)
	assert.True(t, passwordErr.IsCancelled())
}

func TestPasswordPopupSkip(t *testing.T) {
	service := NewBatchImportService(nil)

	jobs := []ImportJob{
		{
			KeystorePath:  "test.json",
			WalletName:    "test",
			RequiresInput: true,
		},
	}

	progressChan := make(chan ImportProgress, 10)
	passwordRequestChan := make(chan PasswordRequest, 1)
	passwordResponseChan := make(chan PasswordResponse, 1)

	// Start import in goroutine
	var results []ImportResult
	done := make(chan bool)
	go func() {
		results = service.ImportBatch(jobs, progressChan, passwordRequestChan, passwordResponseChan)
		done <- true
	}()

	// Consume initial progress
	<-progressChan

	// Receive password request
	<-passwordRequestChan

	// Send skip response
	passwordResponseChan <- PasswordResponse{
		Password:  "",
		Cancelled: false,
		Skip:      true,
	}

	// Wait for completion
	<-done

	// Verify result
	require.Len(t, results, 1)
	assert.False(t, results[0].Success)
	assert.True(t, results[0].Skipped)
	assert.IsType(t, &PasswordInputError{}, results[0].Error)

	passwordErr := results[0].Error.(*PasswordInputError)
	assert.True(t, passwordErr.IsSkipped())
}

func TestCreatePasswordRequest(t *testing.T) {
	service := NewBatchImportService(nil)

	t.Run("first attempt", func(t *testing.T) {
		request := service.createPasswordRequest("test.json", 1, nil)

		assert.Equal(t, "test.json", request.KeystoreFile)
		assert.Equal(t, 1, request.AttemptCount)
		assert.False(t, request.IsRetry)
		assert.Equal(t, "", request.ErrorMessage)
	})

	t.Run("retry with invalid password error", func(t *testing.T) {
		previousError := &PasswordInputError{
			Type:    PasswordInputInvalid,
			Message: "empty password",
			File:    "test.json",
		}

		request := service.createPasswordRequest("test.json", 2, previousError)

		assert.Equal(t, "test.json", request.KeystoreFile)
		assert.Equal(t, 2, request.AttemptCount)
		assert.True(t, request.IsRetry)
		assert.Equal(t, "Password cannot be empty. Please enter a valid password.", request.ErrorMessage)
	})

	t.Run("retry with incorrect password", func(t *testing.T) {
		previousError := &PasswordInputError{
			Type:    PasswordInputMaxAttemptsExceeded,
			Message: "incorrect password",
			File:    "test.json",
		}

		request := service.createPasswordRequest("test.json", 3, previousError)

		assert.Equal(t, "test.json", request.KeystoreFile)
		assert.Equal(t, 3, request.AttemptCount)
		assert.True(t, request.IsRetry)
		assert.Equal(t, "Incorrect password. Please try again.", request.ErrorMessage)
	})
}

func TestSendPasswordRequest(t *testing.T) {
	service := NewBatchImportService(nil)

	t.Run("successful send", func(t *testing.T) {
		passwordRequestChan := make(chan PasswordRequest, 1)
		request := PasswordRequest{KeystoreFile: "test.json"}

		err := service.sendPasswordRequest(request, passwordRequestChan)
		assert.NoError(t, err)

		// Verify request was sent
		select {
		case receivedRequest := <-passwordRequestChan:
			assert.Equal(t, request, receivedRequest)
		default:
			t.Fatal("Request was not sent")
		}
	})

	t.Run("channel unavailable", func(t *testing.T) {
		passwordRequestChan := make(chan PasswordRequest) // No buffer
		request := PasswordRequest{KeystoreFile: "test.json"}

		err := service.sendPasswordRequest(request, passwordRequestChan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "password request channel is unavailable")
	})
}

func TestSendProgressUpdate(t *testing.T) {
	service := NewBatchImportService(nil)

	t.Run("successful send", func(t *testing.T) {
		progressChan := make(chan ImportProgress, 1)
		progress := ImportProgress{TotalFiles: 5}

		// Should not panic or error
		service.sendProgressUpdate(progress, progressChan)

		// Verify progress was sent
		select {
		case receivedProgress := <-progressChan:
			assert.Equal(t, progress, receivedProgress)
		default:
			t.Fatal("Progress was not sent")
		}
	})

	t.Run("channel unavailable - should not error", func(t *testing.T) {
		progressChan := make(chan ImportProgress) // No buffer
		progress := ImportProgress{TotalFiles: 5}

		// Should not panic or error even if channel is unavailable
		service.sendProgressUpdate(progress, progressChan)
	})
}
